package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// BrowserKey is where a step's browser job lives inside input_payload (no schema change).
const BrowserKey = "browser"

const (
	defaultBrowserSteps = 25
	maxBrowserSteps     = 40
	maxBrowserGoal      = 2000
)

// BrowserAction is a job for the owner's own logged-in browser: "open Gmail and write the follow-up to Ada",
// "find the application form and fill it in". It is the general way to use any website the owner is signed in
// to, with no per-site code. The agent works toward the goal one step at a time, and it stops BEFORE any step
// that sends or submits: that final step goes to the owner for approval together with everything that led to it.
type BrowserAction struct {
	Goal     string `json:"goal"`
	StartURL string `json:"start_url,omitempty"`
	MaxSteps int    `json:"max_steps,omitempty"`
}

// BrowserStepKind is one thing the browser can be asked to do.
type BrowserStepKind string

const (
	StepGoto   BrowserStepKind = "goto"
	StepClick  BrowserStepKind = "click"
	StepType   BrowserStepKind = "type"
	StepPress  BrowserStepKind = "press"
	StepScroll BrowserStepKind = "scroll"
	StepWait   BrowserStepKind = "wait"
	// StepOpen opens an app on the phone (Text is its name or package). StepBack presses Back.
	StepOpen BrowserStepKind = "open"
	StepBack BrowserStepKind = "back"
)

// BrowserStep is one recorded action. Ref names an element in the page snapshot the agent was shown.
type BrowserStep struct {
	Kind BrowserStepKind `json:"kind"`
	Ref  string          `json:"ref,omitempty"`
	// Role and Name describe the element the step acts on, as it was when the agent chose it. On replay the
	// browser refuses to act if the element at Ref no longer looks like this, so a changed page cannot turn an
	// approved "click Send" into a click on something else.
	Role string `json:"role,omitempty"`
	Name string `json:"name,omitempty"`
	Text string `json:"text,omitempty"`
	URL  string `json:"url,omitempty"`
	// Note is the agent's plain-words reason, shown to the owner.
	Note string `json:"note,omitempty"`
	// Final marks the step that sends or submits. Only the last step of a script may be final.
	Final bool `json:"final,omitempty"`
}

// ParseBrowserAction reads and validates the browser job in a payload, or returns (nil, nil) when it has none.
func ParseBrowserAction(payload json.RawMessage) (*BrowserAction, error) {
	var v map[string]json.RawMessage
	if len(payload) == 0 || json.Unmarshal(payload, &v) != nil {
		return nil, nil
	}
	raw, ok := v[BrowserKey]
	if !ok || string(raw) == "null" {
		return nil, nil
	}
	var b BrowserAction
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, fmt.Errorf("%w: input_payload.browser is not a valid job", ErrValidation)
	}
	if err := b.Validate(); err != nil {
		return nil, err
	}
	return &b, nil
}

// Validate normalizes and checks the job.
func (b *BrowserAction) Validate() error {
	b.Goal = strings.TrimSpace(b.Goal)
	if b.Goal == "" || len([]rune(b.Goal)) > maxBrowserGoal {
		return fmt.Errorf("%w: a browser job needs a goal of 1 to %d characters", ErrValidation, maxBrowserGoal)
	}
	if b.StartURL != "" {
		if err := ValidateBrowserURL(b.StartURL); err != nil {
			return err
		}
	}
	if b.MaxSteps == 0 {
		b.MaxSteps = defaultBrowserSteps
	}
	if b.MaxSteps < 1 || b.MaxSteps > maxBrowserSteps {
		return fmt.Errorf("%w: max_steps must be 1 to %d", ErrValidation, maxBrowserSteps)
	}
	return nil
}

// ValidateBrowserURL allows only web addresses. file:, javascript:, data: and browser-internal pages are refused:
// an agent steered by a hostile page must not be able to read local files or run script of its choosing.
func ValidateBrowserURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%w: only http and https addresses can be opened", ErrValidation)
	}
	return nil
}

// ValidateScript checks a recorded script before it is run again: web addresses only, one final step at most
// and only at the end, and the typed text and key names within sane sizes.
func ValidateScript(steps []BrowserStep) error {
	if len(steps) == 0 || len(steps) > maxBrowserSteps+1 {
		return fmt.Errorf("%w: a script has 1 to %d steps", ErrValidation, maxBrowserSteps+1)
	}
	for i, s := range steps {
		if s.Final && i != len(steps)-1 {
			return fmt.Errorf("%w: only the last step may send or submit", ErrValidation)
		}
		switch s.Kind {
		case StepGoto:
			if err := ValidateBrowserURL(s.URL); err != nil {
				return err
			}
		case StepClick:
			if s.Ref == "" {
				return fmt.Errorf("%w: click needs an element", ErrValidation)
			}
		case StepType:
			if s.Ref == "" || len(s.Text) > 8000 {
				return fmt.Errorf("%w: type needs an element and at most 8000 characters", ErrValidation)
			}
		case StepPress:
			if s.Text == "" || len(s.Text) > 32 {
				return fmt.Errorf("%w: press needs a key name", ErrValidation)
			}
		case StepOpen:
			if s.Text == "" || len(s.Text) > 100 {
				return fmt.Errorf("%w: open needs an app name", ErrValidation)
			}
		case StepScroll, StepWait, StepBack:
		default:
			return fmt.Errorf("%w: unknown step %q", ErrValidation, s.Kind)
		}
	}
	return nil
}

// ScriptDigest identifies the exact job and everything the agent will do, including every typed word. An
// approval is bound to it, so the owner approves this and nothing else.
func ScriptDigest(goal string, steps []BrowserStep) string {
	h := sha256.New()
	h.Write([]byte(goal))
	for _, s := range steps {
		h.Write([]byte{0x1f})
		h.Write([]byte(strings.Join([]string{string(s.Kind), s.Ref, s.Role, s.Name, s.Text, s.URL, fmt.Sprint(s.Final)}, "\x1e")))
	}
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// BrowserApprovalType is the approval action type that grants exactly this script.
func BrowserApprovalType(digest string) string { return HostActionApprovalPrefix + "browser:" + digest }

// Summary is the one-line description shown to the owner.
func (b *BrowserAction) Summary() string { return "Use the browser: " + truncateRunes(b.Goal, 200) }

// ValidateBrowser checks a task payload at creation: a browser job must be valid and the step must require the
// browser capability, otherwise a host without a signed-in browser could be handed it.
func ValidateBrowser(payload json.RawMessage) error {
	b, err := ParseBrowserAction(payload)
	if err != nil || b == nil {
		return err
	}
	caps, err := parseRequires(payload)
	if err != nil {
		return err
	}
	for _, c := range caps {
		if c == HostCapBrowser {
			return nil
		}
	}
	return fmt.Errorf("%w: a step with a browser job must require the %q capability", ErrValidation, HostCapBrowser)
}

// PhoneKey is where a step's phone job lives inside input_payload. A phone job has the same shape as a browser
// job (a goal, worked one step at a time, stopping before anything is sent) but runs on the phone's screen.
const PhoneKey = "phone"

// ParsePhoneAction reads and validates the phone job in a payload, or returns (nil, nil) when it has none.
func ParsePhoneAction(payload json.RawMessage) (*BrowserAction, error) {
	var v map[string]json.RawMessage
	if len(payload) == 0 || json.Unmarshal(payload, &v) != nil {
		return nil, nil
	}
	raw, ok := v[PhoneKey]
	if !ok || string(raw) == "null" {
		return nil, nil
	}
	var b BrowserAction
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, fmt.Errorf("%w: input_payload.phone is not a valid job", ErrValidation)
	}
	b.StartURL = ""
	if err := b.Validate(); err != nil {
		return nil, err
	}
	return &b, nil
}

// ValidatePhone checks a task payload at creation: a phone job must be valid and require the phone_ui capability.
func ValidatePhone(payload json.RawMessage) error {
	b, err := ParsePhoneAction(payload)
	if err != nil || b == nil {
		return err
	}
	caps, err := parseRequires(payload)
	if err != nil {
		return err
	}
	for _, c := range caps {
		if c == HostCapPhoneUI {
			return nil
		}
	}
	return fmt.Errorf("%w: a step with a phone job must require the %q capability", ErrValidation, HostCapPhoneUI)
}

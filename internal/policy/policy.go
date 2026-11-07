// Package policy decides what a device may do, and it is the only place that decision is made (PRD 7.16).
//
// It is pure: no I/O, no clock except the one passed in. The cloud stores the owner's Policy and serves
// it; the host's tool gateway asks Decide before every outward action. Enforcement lives below the agent,
// so these rules hold even when the agent's own permission prompts are switched off.
//
// Principles: fail closed (anything unrecognized is Off); the owner cannot grant by accident what the
// floors forbid; an action outside what the plan step declared never runs unasked.
package policy

import (
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/griffin/core/internal/domain"
)

// Mode is what happens when a device wants to do something.
type Mode string

const (
	Off  Mode = "off"  // refused, never asked
	Ask  Mode = "ask"  // stops and waits for the owner
	Auto Mode = "auto" // goes ahead
)

// Class is the kind of action, from harmless to consequential.
type Class string

const (
	Read   Class = "read"
	Draft  Class = "draft"
	Send   Class = "send"
	Submit Class = "submit"
	Buy    Class = "buy"
	Delete Class = "delete"
	Call   Class = "call"
)

// AllClasses lists the recognized classes in order of increasing consequence.
var AllClasses = []Class{Read, Draft, Send, Submit, Buy, Delete, Call}

func validClass(c Class) bool {
	for _, k := range AllClasses {
		if c == k {
			return true
		}
	}
	return false
}

func validMode(m Mode) bool { return m == Off || m == Ask || m == Auto }

// strictness orders modes from least to most restrictive.
func strictness(m Mode) int {
	switch m {
	case Auto:
		return 0
	case Ask:
		return 1
	default:
		return 2
	}
}

// Limits.
const (
	MaxRules     = 200
	MaxFullHours = 8
	maxName      = 100
)

// Rule applies to an action when every non-empty dimension matches. The most specific matching rule
// wins; between equally specific rules the more restrictive wins.
type Rule struct {
	Device     string `json:"device,omitempty"`     // "laptop" or "phone"
	Capability string `json:"capability,omitempty"` // a host capability
	App        string `json:"app,omitempty"`        // lowercase app name
	Folder     string `json:"folder,omitempty"`     // absolute path; matches that folder and everything under it
	Contact    string `json:"contact,omitempty"`    // a contact the owner named
	Class      Class  `json:"class,omitempty"`
	Mode       Mode   `json:"mode"`
}

func (r Rule) specificity() int {
	n := 0
	for _, s := range []string{r.Device, r.Capability, r.App, r.Folder, r.Contact, string(r.Class)} {
		if s != "" {
			n++
		}
	}
	return n
}

// Policy is the owner's whole permission setup.
type Policy struct {
	// Default applies when no rule matches. Ask unless set.
	Default Mode   `json:"default"`
	Rules   []Rule `json:"rules"`
	// FullUntil, while in the future, lifts the sandbox's folder limits on the laptop. It never relaxes
	// the floors or the intent check. It must be at most MaxFullHours ahead when set.
	FullUntil *time.Time `json:"full_until,omitempty"`
}

// Default returns the policy a new owner starts with: ask for everything that is not plainly harmless.
func DefaultPolicy() Policy {
	return Policy{
		Default: Ask,
		Rules: []Rule{
			{Class: Read, Mode: Auto},
			{Class: Draft, Mode: Auto},
		},
	}
}

// FullActive reports whether Full mode is currently on.
func (p Policy) FullActive(now time.Time) bool {
	return p.FullUntil != nil && p.FullUntil.After(now)
}

// Validate checks a policy the owner submitted. now is used for the Full time-box.
func (p *Policy) Validate(now time.Time) error {
	if p.Default == "" {
		p.Default = Ask
	}
	if !validMode(p.Default) {
		return fmt.Errorf("%w: default must be off, ask or auto", domain.ErrValidation)
	}
	if len(p.Rules) > MaxRules {
		return fmt.Errorf("%w: at most %d rules", domain.ErrValidation, MaxRules)
	}
	for i := range p.Rules {
		if err := p.Rules[i].validate(); err != nil {
			return fmt.Errorf("%w: rule %d: %v", domain.ErrValidation, i+1, err)
		}
	}
	if p.FullUntil != nil {
		if p.FullUntil.After(now.Add(MaxFullHours * time.Hour)) {
			return fmt.Errorf("%w: full mode lasts at most %d hours", domain.ErrValidation, MaxFullHours)
		}
	}
	return nil
}

func (r *Rule) validate() error {
	if !validMode(r.Mode) {
		return errors.New("mode must be off, ask or auto")
	}
	r.App, r.Contact = strings.ToLower(strings.TrimSpace(r.App)), strings.ToLower(strings.TrimSpace(r.Contact))
	if len(r.App) > maxName || len(r.Contact) > maxName {
		return errors.New("name too long")
	}
	if r.specificity() == 0 {
		return errors.New("a rule must narrow something; use the default for everything")
	}
	if r.Device != "" && r.Device != string(domain.HostKindLaptop) && r.Device != string(domain.HostKindPhone) {
		return errors.New("device must be laptop or phone")
	}
	if r.Capability != "" && !domain.AllHostCapabilities[r.Capability] {
		return fmt.Errorf("unknown capability %q", r.Capability)
	}
	if r.Class != "" && !validClass(r.Class) {
		return fmt.Errorf("unknown class %q", r.Class)
	}
	if r.Folder != "" {
		if !path.IsAbs(r.Folder) || containsDotDot(r.Folder) {
			return errors.New("folder must be an absolute path without ..")
		}
		r.Folder = path.Clean(r.Folder)
		if r.Folder == "/" {
			return errors.New("a rule may not cover the whole filesystem")
		}
	}
	return nil
}

func containsDotDot(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

// Action is something a device is about to do.
type Action struct {
	Device     string
	Capability string
	App        string
	Path       string // the resolved real path (symlinks followed by the caller), for file actions
	Contact    string
	Class      Class
}

// Intent is what the plan step declared it would do. Anything the agent attempts outside it is not
// covered, however the policy reads.
type Intent struct {
	Capability string
	Class      Class
	App        string
	Contact    string
	Folder     string
}

// Covers reports whether the action is within the declared intent.
func (i Intent) Covers(a Action) bool {
	if i.Capability == "" || i.Capability != a.Capability {
		return false
	}
	// Harmless steps toward the declared goal are always in scope; anything else must match exactly.
	if a.Class != i.Class && a.Class != Read && a.Class != Draft {
		return false
	}
	if i.App != "" && !strings.EqualFold(i.App, a.App) {
		return false
	}
	if i.App == "" && a.App != "" {
		return false
	}
	if a.Contact != "" && !strings.EqualFold(i.Contact, a.Contact) {
		return false
	}
	if a.Path != "" {
		if i.Folder == "" || !under(a.Path, i.Folder) {
			return false
		}
	}
	return true
}

// Decision is the outcome, with the reasons in plain words for the audit log and the approval prompt.
type Decision struct {
	Mode    Mode     `json:"mode"`
	Reasons []string `json:"reasons"`
}

// protectedSegments are path segments that are never reachable by an agent, whatever the policy says.
// The host's own token directory is on the list: the agent must not be able to read its own credentials.
var protectedSegments = []string{
	".ssh", ".aws", ".gnupg", ".kube", ".azure", ".griffin-host", ".password-store",
	"keychains", "1password", "bitwarden",
}

// protectedPaths are absolute paths (and everything under them) that are never reachable.
var protectedPaths = []string{"/etc/shadow", "/etc/sudoers", "/etc/ssh", "/root", "/proc", "/sys"}

func isProtected(p string) bool {
	p = path.Clean(strings.ReplaceAll(p, "\\", "/"))
	for _, pp := range protectedPaths {
		if p == pp || strings.HasPrefix(p, pp+"/") {
			return true
		}
	}
	for _, seg := range strings.Split(strings.ToLower(p), "/") {
		for _, bad := range protectedSegments {
			if seg == bad {
				return true
			}
		}
	}
	return false
}

// under reports whether p is dir or inside it, on a path-segment boundary (so /a/docs-secret is not
// under /a/docs) and without being fooled by "..".
func under(p, dir string) bool {
	if !path.IsAbs(p) || containsDotDot(p) {
		return false
	}
	p, dir = path.Clean(p), path.Clean(dir)
	return p == dir || strings.HasPrefix(p, dir+"/")
}

func (r Rule) matches(a Action) bool {
	if r.Device != "" && r.Device != a.Device {
		return false
	}
	if r.Capability != "" && r.Capability != a.Capability {
		return false
	}
	if r.App != "" && r.App != strings.ToLower(a.App) {
		return false
	}
	if r.Contact != "" && r.Contact != strings.ToLower(a.Contact) {
		return false
	}
	if r.Class != "" && r.Class != a.Class {
		return false
	}
	if r.Folder != "" && (a.Path == "" || !under(a.Path, r.Folder)) {
		return false
	}
	return true
}

// Decide is the single decision point. It never returns Auto for something it could not fully check.
func Decide(p Policy, a Action, in Intent) Decision {
	// Fail closed on anything malformed.
	if !validClass(a.Class) || a.Capability == "" || !domain.AllHostCapabilities[a.Capability] {
		return Decision{Off, []string{"unrecognized action"}}
	}
	if a.Path != "" && (!path.IsAbs(a.Path) || containsDotDot(a.Path)) {
		return Decision{Off, []string{"the path is not a resolved absolute path"}}
	}
	// Floor 1: protected locations are unreachable.
	if a.Path != "" && isProtected(a.Path) {
		return Decision{Off, []string{"that location holds credentials and is never available to an agent"}}
	}

	mode := p.Default
	if !validMode(mode) {
		mode = Ask
	}
	reasons := []string{"default: " + string(mode)}

	// Most specific rule wins; ties go to the more restrictive.
	best := -1
	for _, r := range p.Rules {
		if !r.matches(a) {
			continue
		}
		s := r.specificity()
		switch {
		case s > best:
			best, mode, reasons = s, r.Mode, []string{"rule: " + describe(r)}
		case s == best && strictness(r.Mode) > strictness(mode):
			mode, reasons = r.Mode, []string{"rule: " + describe(r)}
		}
	}

	// Floor 2: buying and calling are never automatic.
	if (a.Class == Buy || a.Class == Call) && mode == Auto {
		mode = Ask
		reasons = append(reasons, string(a.Class)+" always needs your approval")
	}

	// The intent check: never run unasked what the plan step did not declare.
	if mode == Auto && !in.Covers(a) {
		mode = Ask
		reasons = append(reasons, "this goes beyond what the step said it would do")
	}
	return Decision{Mode: mode, Reasons: reasons}
}

func describe(r Rule) string {
	var parts []string
	add := func(k, v string) {
		if v != "" {
			parts = append(parts, k+"="+v)
		}
	}
	add("device", r.Device)
	add("capability", r.Capability)
	add("app", r.App)
	add("folder", r.Folder)
	add("contact", r.Contact)
	add("class", string(r.Class))
	sort.Strings(parts)
	return strings.Join(parts, " ") + " -> " + string(r.Mode)
}

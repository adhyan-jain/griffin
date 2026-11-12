// Package browser lets an agent use any website in the owner's own, already signed-in browser, with no code
// per site. It works toward a goal one step at a time and, by construction, STOPS BEFORE any step that sends or
// submits: that step, and every step before it, goes to the owner as a script to approve. On approval the same
// script is replayed exactly, with no model in the loop, so what runs is what was approved.
package browser

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/griffin/core/internal/domain"
)

// Element is something on the page the agent may act on. Ref is stable only within one snapshot.
type Element struct {
	Ref  string `json:"ref"`
	Role string `json:"role"` // link, button, textbox, checkbox, ...
	Name string `json:"name"`
	// Value is what a textbox already holds (never a password).
	Value string `json:"value,omitempty"`
}

// Page is what the agent sees: the address, the visible text and the things it can act on.
type Page struct {
	URL      string    `json:"url"`
	Title    string    `json:"title"`
	Text     string    `json:"text"`
	Elements []Element `json:"elements"`
}

func (p *Page) find(ref string) *Element {
	for i := range p.Elements {
		if p.Elements[i].Ref == ref {
			return &p.Elements[i]
		}
	}
	return nil
}

// Driver is a real or fake browser.
type Driver interface {
	Do(ctx context.Context, step domain.BrowserStep) error
	Snapshot(ctx context.Context) (*Page, error)
}

// Decision is the agent's choice for the next move.
type Decision struct {
	Step domain.BrowserStep
	// Done means the goal is met with nothing left to send; Result is the answer for the owner.
	Done   bool
	Result string
}

// Decider chooses the next move from the goal, what is on screen and what has been done. A model is the real one.
type Decider interface {
	Next(ctx context.Context, goal string, page *Page, done []domain.BrowserStep) (*Decision, error)
}

// Outcome is how a run ended. Exactly one of Result and Staged is set.
type Outcome struct {
	// Result is the answer, when the goal needed nothing sent.
	Result string
	// Staged is a script ready for the owner's approval; its last step is the one that sends or submits.
	Staged *Staged
	Steps  []domain.BrowserStep
}

// Staged is a finished preparation waiting for the owner.
type Staged struct {
	Steps []domain.BrowserStep
	// Preview is what the owner is shown: the page as it will be when the last step is taken.
	Preview string
	Digest  string
}

// ErrStepBudget means the agent used all its steps without finishing.
var ErrStepBudget = errors.New("the browser job ran out of steps before it finished")

// Run works toward act.Goal. Every step the decider returns is executed except one marked Final, which is
// never executed here: the run stops and returns it, with the steps before it, as a Staged script.
func Run(ctx context.Context, drv Driver, dec Decider, act *domain.BrowserAction) (*Outcome, error) {
	var steps []domain.BrowserStep
	if act.StartURL != "" {
		first := domain.BrowserStep{Kind: domain.StepGoto, URL: act.StartURL}
		if err := drv.Do(ctx, first); err != nil {
			return nil, fmt.Errorf("opening %s: %w", act.StartURL, err)
		}
		steps = append(steps, first)
	}
	for i := 0; i < act.MaxSteps; i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page, err := drv.Snapshot(ctx)
		if err != nil {
			return nil, fmt.Errorf("reading the page: %w", err)
		}
		d, err := dec.Next(ctx, act.Goal, page, steps)
		if err != nil {
			return nil, fmt.Errorf("choosing the next step: %w", err)
		}
		if d.Done {
			return &Outcome{Result: d.Result, Steps: steps}, nil
		}
		if d.Step.Kind == domain.StepClick || d.Step.Kind == domain.StepType {
			el := page.find(d.Step.Ref)
			if el == nil {
				return nil, fmt.Errorf("the agent chose element %q, which is not on the page", d.Step.Ref)
			}
			d.Step.Role, d.Step.Name = el.Role, el.Name
		}
		// The model says which step sends. A page can talk a model into calling a send harmless, so the code also
		// decides: anything that looks like sending, paying, publishing or deleting is final, and so is pressing Enter.
		if irreversible(d.Step) {
			d.Step.Final = true
		}
		steps = append(steps, d.Step)
		if d.Step.Final {
			if err := domain.ValidateScript(steps); err != nil {
				return nil, err
			}
			return &Outcome{Steps: steps, Staged: &Staged{
				Steps: steps, Preview: Preview(page, d.Step), Digest: domain.ScriptDigest(act.Goal, steps),
			}}, nil
		}
		if err := drv.Do(ctx, d.Step); err != nil {
			return nil, fmt.Errorf("step %d (%s): %w", i+1, d.Step.Kind, err)
		}
	}
	return nil, ErrStepBudget
}

// Replay runs an approved script exactly as recorded, including its final step, with no model involved.
func Replay(ctx context.Context, drv Driver, steps []domain.BrowserStep) error {
	if err := domain.ValidateScript(steps); err != nil {
		return err
	}
	for i, s := range steps {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := drv.Do(ctx, s); err != nil {
			return fmt.Errorf("step %d (%s): %w", i+1, s.Kind, err)
		}
	}
	return nil
}

// Preview describes what the owner is about to approve: the page, then the step.
func Preview(page *Page, final domain.BrowserStep) string {
	var b strings.Builder
	fmt.Fprintf(&b, "On %s (%s)\n", page.Title, page.URL)
	text := strings.TrimSpace(page.Text)
	if r := []rune(text); len(r) > 1500 {
		text = string(r[:1500]) + "…"
	}
	if text != "" {
		fmt.Fprintf(&b, "\n%s\n", text)
	}
	label := final.Ref
	for _, e := range page.Elements {
		if e.Ref == final.Ref {
			label = fmt.Sprintf("%s “%s”", e.Role, e.Name)
		}
	}
	fmt.Fprintf(&b, "\nNext, and last: %s %s. %s", final.Kind, label, final.Note)
	return strings.TrimSpace(b.String())
}

var irreversibleWords = regexp.MustCompile(`(?i)\b(send|submit|post|reply|apply|confirm|pay|buy|purchase|order|checkout|delete|remove|publish|connect|invite|accept|place|sign ?(in|up)?|subscribe|share|tweet|follow|donate|transfer|withdraw)\b`)

// irreversible reports whether a step could send, spend or destroy something. It errs toward asking.
func irreversible(s domain.BrowserStep) bool {
	switch s.Kind {
	case domain.StepClick:
		return irreversibleWords.MatchString(s.Name)
	case domain.StepPress:
		return strings.EqualFold(s.Text, "enter")
	}
	return false
}

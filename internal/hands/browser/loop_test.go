package browser

import (
	"context"
	"errors"
	"testing"

	"github.com/griffin/core/internal/domain"
)

type fakeDriver struct{ did []domain.BrowserStep }

func (f *fakeDriver) Do(_ context.Context, s domain.BrowserStep) error {
	f.did = append(f.did, s)
	return nil
}
func (f *fakeDriver) Snapshot(context.Context) (*Page, error) {
	return &Page{URL: "https://mail.example/compose", Title: "Compose", Text: "To: ada@x.com\nHello Ada", Elements: []Element{{Ref: "3", Role: "textbox", Name: "Message"}, {Ref: "9", Role: "button", Name: "Send"}}}, nil
}

type scripted struct {
	moves []Decision
	i     int
}

func (s *scripted) Next(context.Context, string, *Page, []domain.BrowserStep) (*Decision, error) {
	d := s.moves[s.i]
	s.i++
	return &d, nil
}

func job() *domain.BrowserAction {
	return &domain.BrowserAction{Goal: "reply to Ada", StartURL: "https://mail.example", MaxSteps: 5}
}

func TestRunStopsBeforeTheFinalStepAndStagesIt(t *testing.T) {
	drv := &fakeDriver{}
	dec := &scripted{moves: []Decision{
		{Step: domain.BrowserStep{Kind: domain.StepType, Ref: "3", Text: "Hello Ada"}},
		{Step: domain.BrowserStep{Kind: domain.StepClick, Ref: "9", Final: true, Note: "Sends the reply."}},
	}}
	out, err := Run(context.Background(), drv, dec, job())
	if err != nil {
		t.Fatal(err)
	}
	if out.Staged == nil || out.Result != "" {
		t.Fatalf("expected a staged script: %+v", out)
	}
	for _, s := range drv.did {
		if s.Final {
			t.Fatal("the final step must never run before the owner approves")
		}
	}
	if len(out.Staged.Steps) != 3 || !out.Staged.Steps[2].Final {
		t.Fatalf("goto, type, final expected: %+v", out.Staged.Steps)
	}
	if out.Staged.Digest != domain.ScriptDigest("reply to Ada", out.Staged.Steps) {
		t.Fatal("digest must cover the whole script")
	}
	if want := "button “Send”"; !contains(out.Staged.Preview, want) {
		t.Fatalf("preview should name what will be pressed (%s): %s", want, out.Staged.Preview)
	}
}

func TestRunReturnsAnAnswerWhenNothingIsSent(t *testing.T) {
	out, err := Run(context.Background(), &fakeDriver{}, &scripted{moves: []Decision{{Done: true, Result: "3 new emails"}}}, job())
	if err != nil || out.Result != "3 new emails" || out.Staged != nil {
		t.Fatalf("%+v %v", out, err)
	}
}

func TestRunStopsAtTheStepBudget(t *testing.T) {
	waits := make([]Decision, 5)
	for i := range waits {
		waits[i] = Decision{Step: domain.BrowserStep{Kind: domain.StepWait}}
	}
	if _, err := Run(context.Background(), &fakeDriver{}, &scripted{moves: waits}, job()); !errors.Is(err, ErrStepBudget) {
		t.Fatalf("want the step budget error, got %v", err)
	}
}

func TestReplayRunsExactlyTheApprovedScript(t *testing.T) {
	drv := &fakeDriver{}
	steps := []domain.BrowserStep{{Kind: domain.StepGoto, URL: "https://mail.example"}, {Kind: domain.StepClick, Ref: "9", Final: true}}
	if err := Replay(context.Background(), drv, steps); err != nil || len(drv.did) != 2 || !drv.did[1].Final {
		t.Fatalf("replay: %+v %v", drv.did, err)
	}
	bad := []domain.BrowserStep{{Kind: domain.StepGoto, URL: "file:///etc/passwd"}}
	if Replay(context.Background(), &fakeDriver{}, bad) == nil {
		t.Fatal("a tampered script must not run")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestTheCodeForcesFinalOnSendLikeStepsWhateverTheModelSays(t *testing.T) {
	drv := &fakeDriver{}
	// The model claims clicking "Send" is harmless (as a hostile page might persuade it to).
	dec := &scripted{moves: []Decision{{Step: domain.BrowserStep{Kind: domain.StepClick, Ref: "9", Final: false}}}}
	out, err := Run(context.Background(), drv, dec, job())
	if err != nil || out.Staged == nil {
		t.Fatalf("a Send click must be staged, not run: %+v %v", out, err)
	}
	for _, s := range drv.did {
		if s.Kind == domain.StepClick {
			t.Fatal("the Send click ran")
		}
	}
	if !irreversible(domain.BrowserStep{Kind: domain.StepPress, Text: "Enter"}) {
		t.Fatal("Enter can send a chat message")
	}
	if irreversible(domain.BrowserStep{Kind: domain.StepClick, Name: "Next page"}) {
		t.Fatal("ordinary navigation should not need approval")
	}
}

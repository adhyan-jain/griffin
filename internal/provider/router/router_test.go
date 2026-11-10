package router

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/worker"
)

func TestClassify_ByWhatTheStepNeeds(t *testing.T) {
	cases := []struct {
		title, goal string
		want        domain.ProviderHint
	}{
		{"Scrape job listings", "collect every internship posting", domain.ProviderAgy},
		{"Log in to LinkedIn and apply", "submit the application form", domain.ProviderAgy},
		{"Search the web and summarise", "find reviews then summarise", domain.ProviderAgy}, // touches the world: stays on agy
		{"Draft a cover letter", "tailor it to the role", domain.ProviderClaude},
		{"Tailor resume and draft application materials", "Format customized resume and cover letter", domain.ProviderClaude},
		{"Tailor resume for Google to match keywords", "Apply keywords to CV", domain.ProviderClaude},
		{"Scout role requirements and company details", "Research role across target companies", domain.ProviderAgy},
		{"Draft a greeting on whatsapp and send to bhai on happy diwali", "Send greeting on whatsapp", domain.ProviderAgy},
		{"Summarise the findings", "one-page brief", domain.ProviderClaude},
		{"Compare the three offers", "pros and cons", domain.ProviderClaude},
		{"Do the thing", "", domain.ProviderAgy}, // unclear defaults to the always-available provider
	}
	for _, c := range cases {
		if got := domain.ClassifyProvider(c.title, c.goal); got != c.want {
			t.Errorf("domain.ClassifyProvider(%q, %q) = %s, want %s", c.title, c.goal, got, c.want)
		}
	}
}

func TestWithHint(t *testing.T) {
	var m map[string]any
	out := domain.WithProviderHint(nil, "Draft a reply", "")
	if err := json.Unmarshal(out, &m); err != nil || m[domain.ProviderHintKey] != "claude" {
		t.Fatalf("empty payload: %s %v", out, err)
	}
	out = domain.WithProviderHint(json.RawMessage(`{"keep":1}`), "Scrape pages", "")
	_ = json.Unmarshal(out, &m)
	if m[domain.ProviderHintKey] != "agy" || m["keep"] != float64(1) {
		t.Fatalf("merge lost data: %s", out)
	}
	pinned := json.RawMessage(`{"provider_hint":"agy"}`)
	if string(domain.WithProviderHint(pinned, "Draft a reply", "")) != string(pinned) {
		t.Fatal("an existing hint must be respected")
	}
	arr := json.RawMessage(`[1,2]`)
	if string(domain.WithProviderHint(arr, "Draft", "")) != string(arr) {
		t.Fatal("a non-object payload must be returned unchanged")
	}

	explicit := domain.WithExplicitProviderHint(pinned, domain.ProviderClaude)
	_ = json.Unmarshal(explicit, &m)
	if m[domain.ProviderHintKey] != "claude" {
		t.Fatalf("expected explicit hint to override, got: %s", explicit)
	}
}

func TestHintFor_ChatAlwaysAgy(t *testing.T) {
	task := &domain.Task{Title: "Draft", InputPayload: json.RawMessage(`{"kind":"chat","provider_hint":"claude"}`)}
	if domain.ProviderHintFor(task) != domain.ProviderAgy {
		t.Fatal("chat must stay on agy")
	}
}

type fakeExec struct {
	name    string
	healthy bool
	err     error
	calls   int
}

func (f *fakeExec) Name() string                            { return f.name }
func (f *fakeExec) IsHealthy(context.Context) (bool, error) { return f.healthy, nil }
func (f *fakeExec) Execute(context.Context, *domain.Task) (*worker.ExecutionResult, error) {
	f.calls++
	if f.err != nil {
		return &worker.ExecutionResult{ProviderName: f.name, Status: "FAILED", Error: f.err}, f.err
	}
	return &worker.ExecutionResult{ProviderName: f.name, Status: "SUCCESS"}, nil
}

func claudeTask() *domain.Task {
	return &domain.Task{Title: "Draft a summary", InputPayload: json.RawMessage(`{"provider_hint":"claude"}`)}
}

func TestRouter_ClaudeOffRunsOnAgy(t *testing.T) {
	agy := &fakeExec{name: "antigravity", healthy: true}
	res, err := New(agy, nil, nil).Execute(context.Background(), claudeTask())
	if err != nil || res.ProviderName != "antigravity" || agy.calls != 1 {
		t.Fatalf("claude off must run on agy: %+v %v", res, err)
	}
}

func TestRouter_UsesClaudeWhenHealthyAndFallsBackWhenNotSignedIn(t *testing.T) {
	agy := &fakeExec{name: "antigravity", healthy: true}
	cl := &fakeExec{name: "claude", healthy: true}
	r := New(agy, cl, nil)

	if res, _ := r.Execute(context.Background(), claudeTask()); res.ProviderName != "claude" || agy.calls != 0 {
		t.Fatalf("healthy claude should run the step: %+v", res)
	}

	cl.err = domain.ErrAuthenticationRequired
	res, err := r.Execute(context.Background(), claudeTask())
	if err != nil || res.ProviderName != "antigravity" {
		t.Fatalf("not signed in must fall back to agy: %+v %v", res, err)
	}

	cl.healthy, cl.err = false, nil
	agy.calls = 0
	if res, _ := r.Execute(context.Background(), claudeTask()); res.ProviderName != "antigravity" || agy.calls != 1 {
		t.Fatalf("unhealthy claude must run on agy: %+v", res)
	}
}

func TestRouter_NonAuthClaudeFailureIsNotHidden(t *testing.T) {
	agy := &fakeExec{name: "antigravity", healthy: true}
	cl := &fakeExec{name: "claude", healthy: true, err: context.DeadlineExceeded}
	res, err := New(agy, cl, nil).Execute(context.Background(), claudeTask())
	if err == nil || res.ProviderName != "claude" || agy.calls != 0 {
		t.Fatalf("a real failure must surface, not silently rerun: %+v %v", res, err)
	}
}

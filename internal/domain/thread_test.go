package domain

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestThreadOf(t *testing.T) {
	root, other := uuid.New(), uuid.New()
	plain := &Task{ID: other, InputPayload: json.RawMessage(`{"kind":"chat"}`)}
	if r, f := ThreadOf(plain); r != other || f != nil || IsFollowUp(plain) {
		t.Errorf("a task that continues nothing is its own thread root, got %v %v", r, f)
	}
	fu := &Task{ID: other, InputPayload: json.RawMessage(`{"thread_id":"` + root.String() + `","follow_up_of":"` + root.String() + `"}`)}
	if r, f := ThreadOf(fu); r != root || f == nil || *f != root || !IsFollowUp(fu) {
		t.Errorf("a follow-up belongs to the root's thread, got %v %v", r, f)
	}
	for _, bad := range []string{``, `not json`, `{"thread_id":"nope","follow_up_of":"nope"}`, `{"thread_id":7}`} {
		tk := &Task{ID: other, InputPayload: json.RawMessage(bad)}
		if r, f := ThreadOf(tk); r != other || f != nil {
			t.Errorf("payload %q must not invent a thread, got %v %v", bad, r, f)
		}
	}
}

func TestResultText(t *testing.T) {
	cases := map[string]string{
		`{"response":"  hello  "}`:          "hello",
		`{"text":"from text"}`:              "from text",
		`"bare string"`:                     "bare string",
		`{"response":"","summary":"later"}`: "later",
		`{"response":"","other":1}`:         "",
		`{"n":1}`:                           "",
		`not json`:                          "",
		``:                                  "",
		`[1,2]`:                             "",
	}
	for in, want := range cases {
		if got := ResultText(json.RawMessage(in)); got != want {
			t.Errorf("ResultText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseClaudeOptions(t *testing.T) {
	got := ParseClaudeOptions(json.RawMessage(`{"claude":{"lead":true,"model":" Sonnet ","effort":"HIGH","budget_usd":0.8}}`))
	if !got.Lead || got.Model != "sonnet" || got.Effort != "high" || got.Budget != 0.8 {
		t.Errorf("valid options should be normalised: %+v", got)
	}
	bad := ParseClaudeOptions(json.RawMessage(`{"claude":{"lead":true,"model":"gpt","effort":"ultra","budget_usd":-1}}`))
	if bad.Model != "" || bad.Effort != "" || bad.Budget != 0 || !bad.Lead {
		t.Errorf("unknown values must become defaults: %+v", bad)
	}
	for _, p := range []string{``, `nope`, `{}`, `{"claude":"x"}`, `{"claude":{"budget_usd":1000}}`} {
		if g := ParseClaudeOptions(json.RawMessage(p)); g.Lead || g.Budget != 0 {
			t.Errorf("payload %q must give empty options, got %+v", p, g)
		}
	}
	out := WithClaudeLead(json.RawMessage(`{"kind":"x"}`), ClaudeStepOptions{Model: "opus"})
	if o := ParseClaudeOptions(out); !o.Lead || o.Model != "opus" || ProviderHintFor(&Task{InputPayload: out}) != ProviderClaude {
		t.Errorf("WithClaudeLead should mark the step and route it to Claude: %s", out)
	}
	if string(WithClaudeLead(json.RawMessage(`[1]`), ClaudeStepOptions{})) != `[1]` {
		t.Error("a non-object payload is returned unchanged")
	}
}

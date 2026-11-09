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

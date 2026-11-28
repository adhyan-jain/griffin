package domain

import (
	"encoding/json"
	"testing"
)

func TestParseDesktopActionScreenshot(t *testing.T) {
	raw := json.RawMessage(`{"desktop":{"op":"screenshot","target":"screen"}}`)
	act, err := ParseDesktopAction(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if act == nil || act.Op != DesktopOpScreenshot || act.Target != "screen" {
		t.Fatalf("unexpected act: %+v", act)
	}

	// Default target when omitted
	rawDefault := json.RawMessage(`{"desktop":{"op":"screenshot"}}`)
	actDef, err := ParseDesktopAction(rawDefault)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if actDef.Target != "screen" {
		t.Fatalf("expected default target 'screen', got %q", actDef.Target)
	}
}

func TestParseDesktopActionOpenURL(t *testing.T) {
	act, err := ParseDesktopAction(json.RawMessage(`{"desktop":{"op":"open_url","url":"https://www.youtube.com/results?search_query=lofi"}}`))
	if err != nil || act == nil || act.Op != DesktopOpOpenURL {
		t.Fatalf("want an open_url action, got %+v, %v", act, err)
	}
	for _, bad := range []string{`{"desktop":{"op":"open_url"}}`, `{"desktop":{"op":"open_url","url":"file:///etc/passwd"}}`, `{"desktop":{"op":"open_url","url":"javascript:alert(1)"}}`} {
		if _, err := ParseDesktopAction(json.RawMessage(bad)); err == nil {
			t.Fatalf("expected %s to be rejected", bad)
		}
	}
}

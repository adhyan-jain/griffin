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

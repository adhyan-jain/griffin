package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWithEditedTextChangesTheDigestAndKeepsTheRest(t *testing.T) {
	in := json.RawMessage(`{"requires":["messaging"],"messaging":{"channel":"whatsapp","op":"send","contact":"Bro","text":"hi"}}`)
	before, _ := ParseMessagingAction(in)
	out, act, err := WithEditedText(in, "hey, are you free tonight?")
	if err != nil {
		t.Fatal(err)
	}
	if act.Text != "hey, are you free tonight?" || act.Digest() == before.Digest() {
		t.Fatalf("text/digest not updated: %+v", act)
	}
	if !strings.Contains(string(out), `"requires"`) {
		t.Fatalf("other payload keys must survive: %s", out)
	}
	again, _ := ParseMessagingAction(out)
	if again.Digest() != act.Digest() {
		t.Fatal("stored payload must produce the same digest the approval will be bound to")
	}
}

func TestWithEditedTextRefusesWhatCannotBeEdited(t *testing.T) {
	for name, p := range map[string]string{
		"read":    `{"messaging":{"channel":"whatsapp","op":"read_recent","contact":"Bro"}}`,
		"none":    `{"foo":1}`,
		"toolong": `{"messaging":{"channel":"whatsapp","op":"send","contact":"Bro","text":"x"}}`,
	} {
		text := "new"
		if name == "toolong" {
			text = strings.Repeat("x", 5000)
		}
		if _, _, err := WithEditedText(json.RawMessage(p), text); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

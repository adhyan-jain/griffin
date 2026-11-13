package domain

import (
	"encoding/json"
	"testing"
)

func TestBrowserActionValidation(t *testing.T) {
	ok := json.RawMessage(`{"browser":{"goal":"open gmail and draft a reply","start_url":"https://mail.google.com"}}`)
	b, err := ParseBrowserAction(ok)
	if err != nil || b == nil || b.MaxSteps != 25 {
		t.Fatalf("valid job: %+v %v", b, err)
	}
	for _, bad := range []string{
		`{"browser":{"goal":""}}`,
		`{"browser":{"goal":"x","start_url":"file:///etc/passwd"}}`,
		`{"browser":{"goal":"x","start_url":"javascript:alert(1)"}}`,
		`{"browser":{"goal":"x","max_steps":999}}`,
	} {
		if _, err := ParseBrowserAction(json.RawMessage(bad)); err == nil {
			t.Errorf("should refuse %s", bad)
		}
	}
	if b, err := ParseBrowserAction(json.RawMessage(`{"messaging":{}}`)); b != nil || err != nil {
		t.Fatal("a payload without a browser job is not ours")
	}
}

func TestScriptDigestCoversEveryTypedWord(t *testing.T) {
	a := []BrowserStep{{Kind: StepType, Ref: "3", Text: "hello"}, {Kind: StepClick, Ref: "9", Final: true}}
	b := []BrowserStep{{Kind: StepType, Ref: "3", Text: "hello!"}, {Kind: StepClick, Ref: "9", Final: true}}
	if ScriptDigest("g", a) == ScriptDigest("g", b) {
		t.Fatal("a changed word must change the digest")
	}
	if ScriptDigest("g", a) == ScriptDigest("other goal", a) {
		t.Fatal("a changed goal must change the digest")
	}
	if ScriptDigest("g", a) != ScriptDigest("g", append([]BrowserStep(nil), a...)) {
		t.Fatal("digest must be stable")
	}
}

func TestValidateScript(t *testing.T) {
	good := []BrowserStep{{Kind: StepGoto, URL: "https://x.com"}, {Kind: StepClick, Ref: "1", Final: true}}
	if err := ValidateScript(good); err != nil {
		t.Fatal(err)
	}
	for name, s := range map[string][]BrowserStep{
		"empty":           nil,
		"final in middle": {{Kind: StepClick, Ref: "1", Final: true}, {Kind: StepWait}},
		"file url":        {{Kind: StepGoto, URL: "file:///etc/passwd"}},
		"unknown kind":    {{Kind: "eval"}},
		"click no ref":    {{Kind: StepClick}},
	} {
		if ValidateScript(s) == nil {
			t.Errorf("%s: should be refused", name)
		}
	}
}

func TestValidateBrowserRequiresTheCapability(t *testing.T) {
	good := json.RawMessage(`{"requires":["browser"],"browser":{"goal":"check my gmail"}}`)
	if err := ValidateBrowser(good); err != nil {
		t.Fatal(err)
	}
	if ValidateBrowser(json.RawMessage(`{"browser":{"goal":"check my gmail"}}`)) == nil {
		t.Fatal("a browser job without the capability must be refused")
	}
	if ValidateBrowser(json.RawMessage(`{"foo":1}`)) != nil {
		t.Fatal("other payloads are not our concern")
	}
}

func TestPhoneJobNeedsItsCapabilityAndRefusesAURL(t *testing.T) {
	if err := ValidatePhone(json.RawMessage(`{"requires":["phone_ui"],"phone":{"goal":"add milk to Notes"}}`)); err != nil {
		t.Fatal(err)
	}
	if ValidatePhone(json.RawMessage(`{"phone":{"goal":"x"}}`)) == nil {
		t.Fatal("capability required")
	}
	b, _ := ParsePhoneAction(json.RawMessage(`{"requires":["phone_ui"],"phone":{"goal":"x","start_url":"file:///etc"}}`))
	if b == nil || b.StartURL != "" {
		t.Fatal("a phone job has no start URL")
	}
	if err := ValidateScript([]BrowserStep{{Kind: StepOpen, Text: "Notes"}, {Kind: StepBack}}); err != nil {
		t.Fatal(err)
	}
}

package domain

import (
	"encoding/json"
	"testing"
)

// A call task carries who to call, not a goal; creating it must not fail as a screen job.
func TestValidatePhoneAcceptsCallJobs(t *testing.T) {
	ok := `{"requires":["calls"],"phone":{"action":"direct_call","number":"Bhai","message":""}}`
	if err := ValidatePhone(json.RawMessage(ok)); err != nil {
		t.Fatalf("call job refused: %v", err)
	}
	for name, bad := range map[string]string{
		"no one to call":      `{"requires":["calls"],"phone":{"action":"automated_call","number":" "}}`,
		"no calls grant":      `{"requires":["phone_ui"],"phone":{"action":"whatsapp_call","number":"Bhai"}}`,
		"screen job, no goal": `{"requires":["phone_ui"],"phone":{"action":"tap_around"}}`,
	} {
		if ValidatePhone(json.RawMessage(bad)) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

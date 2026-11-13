package service

import "testing"

func TestWantsBrowserIsNarrow(t *testing.T) {
	yes := []string{"check my gmail for replies from recruiters", "send a cold mail to 20 founders", "message Ada on LinkedIn about the role", "apply to this internship on LinkedIn", "open my inbox and summarise"}
	no := []string{"write me a cover letter", "draft an email to my professor", "build a gmail clone", "what is linkedin", "search the web for internships"}
	for _, s := range yes {
		if !wantsBrowser(s) {
			t.Errorf("should use the browser: %q", s)
		}
	}
	for _, s := range no {
		if wantsBrowser(s) {
			t.Errorf("should not use the browser: %q", s)
		}
	}
}

func TestWantsPhoneIsNarrow(t *testing.T) {
	for _, s := range []string{"on my phone, open Notes and add milk", "using my phone set a reminder in the Clock app"} {
		if !wantsPhone(s) {
			t.Errorf("should use the phone: %q", s)
		}
	}
	for _, s := range []string{"what phone should I buy", "build a phone app", "write my phone number on the form"} {
		if wantsPhone(s) {
			t.Errorf("should not use the phone: %q", s)
		}
	}
}

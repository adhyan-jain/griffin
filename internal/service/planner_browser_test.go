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

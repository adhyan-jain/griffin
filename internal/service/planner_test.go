package service

import "testing"

// classifyIntent must match whole words. Substring matching misrouted "report" to engineering
// because it contains "repo", and "latest" because it contains "test".
func TestClassifyIntent(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		// Regressions: previously routed to the wrong manager through substring hits.
		{"plan a weekly status report", "research"},
		{"summarise the latest numbers", "research"},
		{"write up the contest results", "research"},
		{"draft a reporting template", "research"},
		{"what is the reputation of this vendor", "research"},

		// True positives that must keep working (including simple inflections).
		{"fix the login bug in the repo", "engineering"},
		{"refactor the payments codebase", "engineering"},
		{"debug the failing tests", "engineering"},
		{"open a pull request on github", "engineering"},
		{"tailor my resume for this job", "career"},
		{"prepare for the interview next week", "career"},
		{"find a hackathon with a bounty", "hackathon"},
		{"write a freelance proposal for the gig", "freelance"},
		{"set up outreach and update the crm", "networking"},
		{"summarise the ai release news", "tech_intelligence"},
		{"benchmark the new models", "tech_intelligence"},
		{"build a study curriculum", "learning"},
		{"plan my travel and calendar", "personal_ops"},
		{"review my aws bill and subscriptions", "finance"},
		{"write a prd for the new feature", "product_ideas"},

		// Default and case handling.
		{"", "research"},
		{"something with no keywords at all", "research"},
	}
	for _, tc := range cases {
		if got := classifyIntent(tc.text); got != tc.want {
			t.Errorf("classifyIntent(%q) = %q, want %q", tc.text, got, tc.want)
		}
	}
}

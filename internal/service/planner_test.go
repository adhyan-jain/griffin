package service

import (
	"context"
	"testing"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/manager"
)

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

// Plain asks are answered in one step; only an explicit research ask keeps the gather-then-synthesize split.
func TestRuleBasedPlanner_FastPathForPlainAsks(t *testing.T) {
	reg := manager.NewDefaultRegistry()
	p := NewRuleBasedPlanner()
	steps := func(text string) (int, string) {
		t.Helper()
		plan, err := p.GeneratePlan(context.Background(), &domain.UserRequest{RequestText: text, Priority: domain.TaskPriorityNormalP2}, reg)
		if err != nil {
			t.Fatalf("plan %q: %v", text, err)
		}
		return len(plan.ChildTasks), plan.ChildTasks[0].Goal
	}

	for _, plain := range []string{"Plan a weekly status report", "Give me a concise summary of the AI agent ecosystem", "what should I cook tonight"} {
		n, goal := steps(plain)
		if n != 1 {
			t.Errorf("%q: want one step, got %d", plain, n)
		}
		if goal != plain {
			t.Errorf("%q: the single step must carry the whole request, got %q", plain, goal)
		}
	}
	for _, research := range []string{"Research best practices for Go worker pools", "Find sources on cgroup v2 limits", "Deep dive into vector databases with citations"} {
		if n, _ := steps(research); n != 2 {
			t.Errorf("%q: explicit research keeps two steps, got %d", research, n)
		}
	}
}

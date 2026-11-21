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
		{"find software engineering internships", "career"},
		{"apply for backend internship at stripe", "career"},
		{"make a list of summer intern roles", "career"},
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

func TestRuleBasedPlanner_CareerInternship(t *testing.T) {
	reg := manager.NewDefaultRegistry()
	p := NewRuleBasedPlanner()

	// Finding/making internships without applying: 2 steps (research + tailor), no approval required.
	findReq := &domain.UserRequest{
		RequestText: "Find software engineering internships for summer 2025",
		Priority:    domain.TaskPriorityNormalP2,
	}
	findPlan, err := p.GeneratePlan(context.Background(), findReq, reg)
	if err != nil {
		t.Fatalf("GeneratePlan find: %v", err)
	}
	if len(findPlan.ChildTasks) != 2 {
		t.Fatalf("expected 2 child tasks, got %d", len(findPlan.ChildTasks))
	}
	if len(findPlan.ApprovalRequirements) != 0 {
		t.Fatalf("expected 0 approvals for finding internships, got %d", len(findPlan.ApprovalRequirements))
	}

	// Applying for internship: 2 steps, approval required on tailor step with ActionTypeJobApply and ApprovalRiskHigh.
	applyReq := &domain.UserRequest{
		RequestText: "Apply for backend internship at Stripe",
		Priority:    domain.TaskPriorityNormalP2,
	}
	applyPlan, err := p.GeneratePlan(context.Background(), applyReq, reg)
	if err != nil {
		t.Fatalf("GeneratePlan apply: %v", err)
	}
	if len(applyPlan.ApprovalRequirements) != 1 {
		t.Fatalf("expected 1 approval for applying to internship, got %d", len(applyPlan.ApprovalRequirements))
	}
	app := applyPlan.ApprovalRequirements[0]
	if app.ActionType != domain.ActionTypeJobApply {
		t.Errorf("expected ActionType %q, got %q", domain.ActionTypeJobApply, app.ActionType)
	}
	if app.RiskLevel != domain.ApprovalRiskHigh {
		t.Errorf("expected RiskLevel %q, got %q", domain.ApprovalRiskHigh, app.RiskLevel)
	}
}

func TestRuleBasedPlanner_CareerApplyDoesNotRouteToWhatsApp(t *testing.T) {
	reg := manager.NewDefaultRegistry()
	p := NewRuleBasedPlanner()

	userPrompt := "cool, apply for all these companies asap, show me the edited resumes for it, also when applying tell them what i can do for them in their company not like i'll do what you want but more like i can offer these particular set of skills, for this you already knwo what ik based on all my projects like echotales, computehive, griffin, etc. and make sure you know what they actually want based on the requirements and draft message based on that. Some things for the resume, make sure it covers on full page, keep information under the project which shows what the project is actually about, not like i did 1000 tests and all since those are just written by claude and are very minor checks and all"

	req := &domain.UserRequest{
		RequestText: userPrompt,
		Priority:    domain.TaskPriorityNormalP2,
	}

	plan, err := p.GeneratePlan(context.Background(), req, reg)
	if err != nil {
		t.Fatalf("GeneratePlan: %v", err)
	}

	if plan.ManagerID != "career" {
		t.Fatalf("expected ManagerID 'career', got %q", plan.ManagerID)
	}

	if len(plan.ChildTasks) != 2 {
		t.Fatalf("expected 2 child tasks, got %d", len(plan.ChildTasks))
	}

	if plan.ChildTasks[0].Key != "career_research" {
		t.Errorf("expected first child task 'career_research', got %q", plan.ChildTasks[0].Key)
	}
	if plan.ChildTasks[1].Key != "career_tailor" {
		t.Errorf("expected second child task 'career_tailor', got %q", plan.ChildTasks[1].Key)
	}

	for _, ct := range plan.ChildTasks {
		if ct.ManagerDomain == "personal_ops" {
			t.Errorf("child task %s must not belong to personal_ops", ct.Key)
		}
		if ct.Key == "wa_compose" || ct.Key == "wa_do" || ct.Key == "call_plan" || ct.Key == "call_do" {
			t.Errorf("child task %s must not be a whatsapp or call step", ct.Key)
		}
	}

	if len(plan.ApprovalRequirements) != 1 {
		t.Fatalf("expected 1 approval requirement for job application, got %d", len(plan.ApprovalRequirements))
	}
	if plan.ApprovalRequirements[0].ActionType != domain.ActionTypeJobApply {
		t.Errorf("expected ActionType %q, got %q", domain.ActionTypeJobApply, plan.ApprovalRequirements[0].ActionType)
	}
}

// A request to prepare applications for the owner's own list is one career task that is later split per listing,
// not a model-invented research/resume/cover-letter pipeline that never reads the list.
func TestBatchApplyRequestPlansOneCareerTask(t *testing.T) {
	reg := manager.NewDefaultRegistry()
	req := &domain.UserRequest{RequestText: "prepare the application drafts for all the A tier internships on my list and get them ready for my approval", Priority: "NORMAL"}

	for name, planner := range map[string]interface {
		GeneratePlan(context.Context, *domain.UserRequest, *manager.Registry) (*domain.PlanPayload, error)
	}{"rules": NewRuleBasedPlanner(), "ai": NewAIPlanner(nil)} {
		plan, err := planner.GeneratePlan(context.Background(), req, reg)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(plan.ChildTasks) != 1 || plan.ChildTasks[0].ManagerDomain != "career" {
			t.Fatalf("%s: want one career task, got %+v", name, plan.ChildTasks)
		}
		c := plan.ChildTasks[0]
		if c.ApprovalRequired || len(plan.ApprovalRequirements) != 0 {
			t.Fatalf("%s: the batch task itself must not wait for approval", name)
		}
		if errs := domain.ValidatePlan(plan, nil); len(errs) > 0 {
			t.Fatalf("%s: the plan must pass validation: %v", name, errs)
		}
		if dynamicEligible(plan) {
			t.Fatalf("%s: the dynamic planner must leave a batch plan alone", name)
		}
		if _, ok := domain.ApplyBatchOf(c.Title, c.Goal, c.InputPayload); !ok {
			t.Fatalf("%s: the task must still be recognised as a batch so it is split per listing", name)
		}
	}

	other := &domain.UserRequest{RequestText: "tailor my resume for this backend job", Priority: "NORMAL"}
	if plan := batchApplyPlan(other, reg); plan != nil {
		t.Fatal("an ordinary resume request must not be routed as a batch")
	}
}

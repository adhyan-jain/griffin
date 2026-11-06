package domain

import (
	"encoding/json"
	"testing"
)

func TestValidatePlan_ValidLinearDAG(t *testing.T) {
	plan := &PlanPayload{
		Objective:          "Research and draft article",
		InterpretedRequest: "User wants a research paper summarized",
		ManagerID:          "research",
		TaskType:           "RESEARCH_SYNTHESIS",
		Priority:           TaskPriorityNormalP2,
		ChildTasks: []ChildTaskPlan{
			{
				Key:           "t1",
				Title:         "Gather sources",
				Goal:          "Search for academic sources",
				ManagerDomain: "research",
				InputPayload:  json.RawMessage(`{}`),
				Priority:      TaskPriorityNormalP2,
			},
			{
				Key:           "t2",
				Title:         "Synthesize notes",
				Goal:          "Summarize findings",
				ManagerDomain: "research",
				InputPayload:  json.RawMessage(`{}`),
				Priority:      TaskPriorityNormalP2,
			},
		},
		Dependencies: []PlanDependency{
			{TaskKey: "t2", DependsOnKey: "t1"},
		},
	}

	allowed := map[string]bool{"research": true}
	errs := ValidatePlan(plan, allowed)
	if len(errs) > 0 {
		t.Fatalf("expected 0 validation errors, got: %v", errs)
	}
}

func TestValidatePlan_DetectsCycles(t *testing.T) {
	plan := &PlanPayload{
		Objective:          "Cyclic plan",
		InterpretedRequest: "Bad request",
		ManagerID:          "research",
		ChildTasks: []ChildTaskPlan{
			{Key: "a", Title: "Task A", Goal: "Do A"},
			{Key: "b", Title: "Task B", Goal: "Do B"},
			{Key: "c", Title: "Task C", Goal: "Do C"},
		},
		Dependencies: []PlanDependency{
			{TaskKey: "b", DependsOnKey: "a"},
			{TaskKey: "c", DependsOnKey: "b"},
			{TaskKey: "a", DependsOnKey: "c"}, // cycle!
		},
	}

	errs := ValidatePlan(plan, map[string]bool{"research": true})
	foundCycle := false
	for _, e := range errs {
		if e == "cyclic dependency detected in child_tasks DAG" {
			foundCycle = true
		}
	}
	if !foundCycle {
		t.Fatalf("expected cycle detection error, got errors: %v", errs)
	}
}

func TestValidatePlan_DetectsUnknownManager(t *testing.T) {
	plan := &PlanPayload{
		Objective:          "Objective",
		InterpretedRequest: "Request",
		ManagerID:          "super_admin_manager",
		ChildTasks: []ChildTaskPlan{
			{Key: "t1", Title: "Task 1", Goal: "Goal 1"},
		},
	}

	errs := ValidatePlan(plan, map[string]bool{"research": true})
	foundManagerErr := false
	for _, e := range errs {
		if e == "unknown manager_id 'super_admin_manager'" {
			foundManagerErr = true
		}
	}
	if !foundManagerErr {
		t.Fatalf("expected unknown manager error, got errors: %v", errs)
	}
}

func TestValidatePlan_RequiresApprovalOnSensitiveCapability(t *testing.T) {
	plan := &PlanPayload{
		Objective:          "Send outbound emails",
		InterpretedRequest: "Send newsletter",
		ManagerID:          "career",
		ChildTasks: []ChildTaskPlan{
			{
				Key:                  "send_job_email",
				Title:                "Send cold email",
				Goal:                 "Send an email to recruiter",
				CapabilitiesRequired: []string{CapabilityEmailSend},
				ApprovalRequired:     false, // sensitive without approval!
			},
		},
	}

	errs := ValidatePlan(plan, map[string]bool{"career": true})
	foundApprovalErr := false
	for _, e := range errs {
		if e == "child_tasks[send_job_email]: capability 'email:send' is sensitive and requires approval_required=true" {
			foundApprovalErr = true
		}
	}
	if !foundApprovalErr {
		t.Fatalf("expected sensitive approval error, got: %v", errs)
	}
}

func TestWorkerDefinition_Validation(t *testing.T) {
	def := &WorkerDefinition{
		WorkerKey:           "valid_worker",
		ManagerID:           "engineering",
		Name:                "Code Reviewer",
		SystemPrompt:        "You are an expert code reviewer.",
		ExecutionTimeoutSec: 300,
		MaxRetries:          2,
		AllowedCapabilities: []string{CapabilityRepoRead},
	}

	if err := def.Validate(); err != nil {
		t.Fatalf("expected valid worker definition, got: %v", err)
	}

	// Invalid worker key
	def.WorkerKey = "Bad Key!"
	if err := def.Validate(); err == nil {
		t.Fatal("expected error on invalid worker key")
	}

	// Unknown capability
	def.WorkerKey = "valid_worker"
	def.AllowedCapabilities = []string{"super_root_access"}
	if err := def.Validate(); err == nil {
		t.Fatal("expected error on unknown capability")
	}
}

func TestToolModeFor_MalformedOrChatCanOnlyBeLessCapable(t *testing.T) {
	cases := map[string]ToolMode{
		``:                                  ToolNone,
		`{}`:                                ToolNone,
		`not json`:                          ToolNone,
		`{"tool_mode":"read"}`:              ToolRead,
		`{"tool_mode":"act"}`:               ToolAct,
		`{"tool_mode":"root"}`:              ToolNone,
		`{"tool_mode":"ACT"}`:               ToolNone,
		`{"kind":"chat","tool_mode":"act"}`: ToolNone,
		`{"tool_mode":["act"]}`:             ToolNone,
	}
	for payload, want := range cases {
		task := &Task{InputPayload: json.RawMessage(payload)}
		if got := ToolModeFor(task); got != want {
			t.Errorf("payload %q: got %q, want %q", payload, got, want)
		}
	}
}

func TestSuggestToolMode_NeverSuggestsAct(t *testing.T) {
	for _, c := range []struct {
		title, goal string
		want        ToolMode
	}{
		{"Scrape job listings", "collect postings", ToolRead},
		{"Log in and apply", "submit the application form", ToolRead}, // still only read: act is an explicit, approved choice
		{"Draft a cover letter", "tailor it", ToolNone},
	} {
		if got := SuggestToolMode(c.title, c.goal); got != c.want {
			t.Errorf("SuggestToolMode(%q) = %q, want %q", c.title, got, c.want)
		}
	}
}

func TestWithToolMode_RespectsExistingAndNonObjects(t *testing.T) {
	var m map[string]any
	out := WithToolMode(nil, "Scrape pages", "")
	if err := json.Unmarshal(out, &m); err != nil || m[ToolModeKey] != "read" {
		t.Fatalf("empty payload: %s %v", out, err)
	}
	pinned := json.RawMessage(`{"tool_mode":"none","x":1}`)
	if string(WithToolMode(pinned, "Scrape pages", "")) != string(pinned) {
		t.Fatal("an explicit mode must be respected")
	}
	arr := json.RawMessage(`[1]`)
	if string(WithToolMode(arr, "Scrape", "")) != string(arr) {
		t.Fatal("a non-object payload must be left alone")
	}
	merged := WithToolMode(json.RawMessage(`{"provider_hint":"agy"}`), "Scrape pages", "")
	_ = json.Unmarshal(merged, &m)
	if m["provider_hint"] != "agy" || m[ToolModeKey] != "read" {
		t.Fatalf("merge lost the provider hint: %s", merged)
	}
}

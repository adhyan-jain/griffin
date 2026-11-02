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

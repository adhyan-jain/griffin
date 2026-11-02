package manager

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/griffin/core/internal/domain"
)

func TestRegistry_Has11StandardManagers(t *testing.T) {
	reg := NewDefaultRegistry()
	list := reg.List()

	if len(list) != 11 {
		t.Fatalf("expected 11 standard managers, got %d", len(list))
	}

	expectedIDs := []string{
		"career", "research", "engineering", "hackathon",
		"freelance", "networking", "tech_intelligence", "learning",
		"personal_ops", "finance", "product_ideas",
	}

	for _, id := range expectedIDs {
		mgr, ok := reg.Get(id)
		if !ok || mgr == nil {
			t.Errorf("expected manager with id '%s' to exist", id)
			continue
		}
		def := mgr.Definition()
		if def.ID != id {
			t.Errorf("expected def.ID == '%s', got '%s'", id, def.ID)
		}
		if def.Name == "" || def.Domain == "" || len(def.Responsibilities) == 0 {
			t.Errorf("manager '%s' has missing required fields", id)
		}
	}
}

func TestRegistry_RejectsUnauthorizedNewManagerCreation(t *testing.T) {
	reg := NewDefaultRegistry()

	customDef := ManagerDefinition{
		ID:     "custom_shadow_manager",
		Name:   "Shadow Manager",
		Domain: "shadow",
	}
	customMgr := NewStandardManager(customDef)

	// User not authorized
	err := reg.Register(customMgr, false)
	if err == nil {
		t.Fatal("expected error when registering new manager without explicit user approval")
	}

	// User authorized
	err = reg.Register(customMgr, true)
	if err != nil {
		t.Fatalf("expected success with explicit user authorization, got: %v", err)
	}

	if !reg.IsKnownManager("custom_shadow_manager") {
		t.Fatal("expected custom_shadow_manager to be registered")
	}
}

func TestStandardManager_InterpretAndVerify(t *testing.T) {
	reg := NewDefaultRegistry()
	careerMgr, ok := reg.Get("career")
	if !ok {
		t.Fatal("expected career manager")
	}

	ctx := context.Background()
	task := &domain.Task{
		Title:        "Apply to Senior Systems Engineer",
		Goal:         "Submit application and resume to company",
		InputPayload: json.RawMessage(`{"company":"Acme"}`),
		Priority:     domain.TaskPriorityHighP1,
	}

	plan, err := careerMgr.Interpret(ctx, task)
	if err != nil {
		t.Fatalf("unexpected interpret error: %v", err)
	}

	if plan.ManagerID != "career" {
		t.Fatalf("expected manager_id 'career', got '%s'", plan.ManagerID)
	}
	if len(plan.ChildTasks) != 1 {
		t.Fatalf("expected 1 child task, got %d", len(plan.ChildTasks))
	}

	// Verify that career manager enforces approval on sensitive job apply / email send capabilities
	if !plan.ChildTasks[0].ApprovalRequired {
		t.Fatal("expected child task to require approval due to sensitive capabilities")
	}

	// Deliverable verification tests
	okVerify, _, err := careerMgr.VerifyDeliverable(ctx, task, json.RawMessage(`{"status":"success","resume_tailored":true}`))
	if err != nil || !okVerify {
		t.Fatalf("expected successful verification, got ok=%v, err=%v", okVerify, err)
	}

	// Empty output
	okVerify, _, _ = careerMgr.VerifyDeliverable(ctx, task, json.RawMessage(`{}`))
	if okVerify {
		t.Fatal("expected empty payload to fail verification")
	}

	// Error output
	okVerify, _, _ = careerMgr.VerifyDeliverable(ctx, task, json.RawMessage(`{"status":"error","error":"application closed"}`))
	if okVerify {
		t.Fatal("expected error status in output to fail verification")
	}
}

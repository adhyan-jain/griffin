package tests

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
)

func TestAgentFactory_CreateAndRetrieveWorkerDefinition(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "worker_creator@example.com")

	def := &domain.WorkerDefinition{
		UserID:               userID,
		WorkerKey:            "research_analyst",
		ManagerID:            "research",
		Name:                 "Research Analyst",
		Description:          "Conducts deep research across primary sources",
		SystemPrompt:         "You are a rigorous research analyst.",
		AllowedCapabilities:  []string{domain.CapabilityResearchRead, domain.CapabilityWebRead},
		AllowedTools:         []string{"web_search", "view_file"},
		ExecutionTimeoutSec:  600,
		MaxRetries:           3,
		VerificationCriteria: []string{"Must cite primary sources"},
		InputSchema:          json.RawMessage(`{"type":"object"}`),
		OutputSchema:         json.RawMessage(`{"type":"object"}`),
	}

	created, err := env.AgentFactory.CreateWorkerDefinition(ctx, def, "USER")
	if err != nil {
		t.Fatalf("expected successful worker creation, got: %v", err)
	}

	if created.ID == uuid.Nil {
		t.Fatal("expected non-nil ID")
	}
	if created.Version != 1 {
		t.Fatalf("expected initial version 1, got %d", created.Version)
	}
	if !created.IsActive {
		t.Fatal("expected worker to be active")
	}

	// Retrieve by ID
	retrieved, err := env.AgentFactory.GetWorkerDefinition(ctx, created.ID, userID)
	if err != nil {
		t.Fatalf("failed to retrieve worker by ID: %v", err)
	}
	if retrieved.Name != "Research Analyst" {
		t.Fatalf("expected name 'Research Analyst', got '%s'", retrieved.Name)
	}

	// Retrieve latest by key
	byKey, err := env.AgentFactory.GetWorkerDefinitionByKey(ctx, userID, "research_analyst")
	if err != nil {
		t.Fatalf("failed to retrieve worker by key: %v", err)
	}
	if byKey.ID != created.ID {
		t.Fatalf("expected retrieved ID %s, got %s", created.ID, byKey.ID)
	}
}

func TestAgentFactory_WorkerVersioningAndImmutability(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "version_tester@example.com")

	def := &domain.WorkerDefinition{
		UserID:              userID,
		WorkerKey:           "code_reviewer",
		ManagerID:           "engineering",
		Name:                "Code Reviewer v1",
		SystemPrompt:        "Initial reviewer prompt",
		AllowedCapabilities: []string{domain.CapabilityRepoRead},
	}

	v1, err := env.AgentFactory.CreateWorkerDefinition(ctx, def, "USER")
	if err != nil {
		t.Fatalf("failed to create v1: %v", err)
	}
	if v1.Version != 1 {
		t.Fatalf("expected version 1, got %d", v1.Version)
	}

	// Update to v2
	newName := "Code Reviewer v2"
	newPrompt := "Enhanced reviewer prompt with standards verification"
	v2, err := env.AgentFactory.UpdateWorkerDefinition(ctx, userID, "code_reviewer", domain.WorkerDefinitionUpdate{
		Name:         &newName,
		SystemPrompt: &newPrompt,
	}, "USER")
	if err != nil {
		t.Fatalf("failed to create v2: %v", err)
	}

	if v2.Version != 2 {
		t.Fatalf("expected version 2, got %d", v2.Version)
	}
	if v2.Name != "Code Reviewer v2" {
		t.Fatalf("expected name 'Code Reviewer v2', got '%s'", v2.Name)
	}

	// Verify v1 is still intact in history (immutability)
	v1Reloaded, err := env.AgentFactory.GetWorkerDefinition(ctx, v1.ID, userID)
	if err != nil {
		t.Fatalf("failed to load v1: %v", err)
	}
	if v1Reloaded.Version != 1 || v1Reloaded.Name != "Code Reviewer v1" {
		t.Fatalf("v1 record was mutated! Version=%d, Name=%s", v1Reloaded.Version, v1Reloaded.Name)
	}

	// Verify ListWorkerVersions returns both versions
	versions, err := env.AgentFactory.ListWorkerVersions(ctx, userID, "code_reviewer")
	if err != nil {
		t.Fatalf("failed to list versions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions in history, got %d", len(versions))
	}
	if versions[0].Version != 2 || versions[1].Version != 1 {
		t.Fatalf("expected versions sorted descending [2, 1], got [%d, %d]", versions[0].Version, versions[1].Version)
	}
}

func TestAgentFactory_ToggleActive(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "toggle_tester@example.com")

	def := &domain.WorkerDefinition{
		UserID:              userID,
		WorkerKey:           "hackathon_scout",
		ManagerID:           "hackathon",
		Name:                "Hackathon Scout",
		SystemPrompt:        "You scout hackathons.",
		AllowedCapabilities: []string{domain.CapabilityWebRead},
	}

	created, err := env.AgentFactory.CreateWorkerDefinition(ctx, def, "USER")
	if err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}

	// Disable worker
	disabled, err := env.AgentFactory.ToggleWorker(ctx, created.ID, userID, false, "USER")
	if err != nil {
		t.Fatalf("failed to disable worker: %v", err)
	}
	if disabled.IsActive {
		t.Fatal("expected is_active=false")
	}

	// Attempt to instantiate task from disabled worker should fail
	_, err = env.AgentFactory.InstantiateTask(ctx, disabled, nil, nil, "Task", "Goal", nil, domain.TaskPriorityNormalP2)
	if err == nil {
		t.Fatal("expected instantiation of disabled worker to fail")
	}

	// Re-enable worker
	enabled, err := env.AgentFactory.ToggleWorker(ctx, created.ID, userID, true, "USER")
	if err != nil {
		t.Fatalf("failed to enable worker: %v", err)
	}
	if !enabled.IsActive {
		t.Fatal("expected is_active=true")
	}
}

func TestAgentFactory_GovernanceAndActorValidation(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "gov_tester@example.com")

	def := &domain.WorkerDefinition{
		UserID:              userID,
		WorkerKey:           "finance_auditor",
		ManagerID:           "finance",
		Name:                "Finance Auditor",
		SystemPrompt:        "Audit cloud costs.",
		AllowedCapabilities: []string{domain.CapabilityFinancialAction},
	}

	// Unauthorized actor (e.g., worker trying to create another worker)
	_, err := env.AgentFactory.CreateWorkerDefinition(ctx, def, "WORKER_SUBPROCESS")
	if err == nil {
		t.Fatal("expected unauthorized actor to be rejected")
	}

	// Unknown manager rejection
	def.ManagerID = "unknown_phantom_manager"
	_, err = env.AgentFactory.CreateWorkerDefinition(ctx, def, "USER")
	if err == nil {
		t.Fatal("expected unknown manager to be rejected")
	}
}

func TestAgentFactory_InstantiateTask(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "instantiate_tester@example.com")

	def := &domain.WorkerDefinition{
		UserID:              userID,
		WorkerKey:           "learning_curriculum_designer",
		ManagerID:           "learning",
		Name:                "Curriculum Designer",
		SystemPrompt:        "Design structured learning roadmaps.",
		AllowedCapabilities: []string{domain.CapabilityResearchRead},
		AllowedTools:        []string{"web_search"},
		MaxRetries:          2,
		InputSchema:         json.RawMessage(`{"type":"object"}`),
		OutputSchema:        json.RawMessage(`{"type":"object"}`),
	}

	created, err := env.AgentFactory.CreateWorkerDefinition(ctx, def, "USER")
	if err != nil {
		t.Fatalf("failed to create worker: %v", err)
	}

	parentID := uuid.New()
	rootID := uuid.New()
	task, err := env.AgentFactory.InstantiateTask(
		ctx,
		created,
		&parentID,
		&rootID,
		"Learn Distributed Systems",
		"Generate 6-week curriculum",
		json.RawMessage(`{"depth":"intermediate"}`),
		domain.TaskPriorityHighP1,
	)
	if err != nil {
		t.Fatalf("failed to instantiate task: %v", err)
	}

	if *task.ParentID != parentID {
		t.Fatalf("expected parent ID %s, got %s", parentID, *task.ParentID)
	}
	if *task.RootID != rootID {
		t.Fatalf("expected root ID %s, got %s", rootID, *task.RootID)
	}
	if task.ManagerDomain != "learning" {
		t.Fatalf("expected manager domain 'learning', got '%s'", task.ManagerDomain)
	}
	if task.Priority != domain.TaskPriorityHighP1 {
		t.Fatalf("expected priority HIGH_P1, got %s", task.Priority)
	}
	if task.MaxRetries != 2 {
		t.Fatalf("expected max retries 2, got %d", task.MaxRetries)
	}

	// Verify assembled input payload contains embedded worker instructions
	var payloadMap map[string]any
	if err := json.Unmarshal(task.InputPayload, &payloadMap); err != nil {
		t.Fatalf("failed to unmarshal assembled payload: %v", err)
	}
	if payloadMap["_worker_key"] != "learning_curriculum_designer" {
		t.Fatalf("expected _worker_key in payload, got %v", payloadMap["_worker_key"])
	}
	if payloadMap["_system_prompt"] != "Design structured learning roadmaps." {
		t.Fatalf("expected _system_prompt in payload, got %v", payloadMap["_system_prompt"])
	}
	if payloadMap["depth"] != "intermediate" {
		t.Fatalf("expected user context preserved in payload, got %v", payloadMap["depth"])
	}
}

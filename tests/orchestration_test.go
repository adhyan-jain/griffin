package tests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/manager"
	"github.com/griffin/core/internal/provider/antigravity"
	"github.com/griffin/core/internal/service"
	"github.com/griffin/core/internal/worker"
)

func TestJJ_SubmitRequest_ProducesPlanAndDelegates(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "jj_user1@example.com")

	req := &domain.UserRequest{
		UserID:      userID,
		RequestText: "Research best practices for Google Antigravity CLI deployment on Linux",
		Priority:    domain.TaskPriorityHighP1,
	}

	orch, plan, err := env.Orchestrator.SubmitRequest(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error submitting request to JJ: %v", err)
	}

	if orch.ID == uuid.Nil {
		t.Fatal("expected non-nil orchestration ID")
	}
	if orch.Status != domain.OrchestrationStatusRunning {
		t.Fatalf("expected status RUNNING, got %s", orch.Status)
	}
	if orch.ManagerID != "research" {
		t.Fatalf("expected manager_id 'research', got '%s'", orch.ManagerID)
	}

	// Verify plan was persisted and validated
	if plan.Status != domain.PlanStatusExecuted {
		t.Fatalf("expected plan status EXECUTED, got %s", plan.Status)
	}
	if len(plan.ValidationErrors) > 0 {
		t.Fatalf("expected 0 validation errors, got: %v", plan.ValidationErrors)
	}

	// Verify root task exists in tasks table
	rootTask, err := env.TaskRepo.GetTaskByID(ctx, orch.RootTaskID)
	if err != nil {
		t.Fatalf("failed to retrieve root task: %v", err)
	}
	if rootTask.ManagerDomain != "JJ" {
		t.Fatalf("expected root task manager_domain 'JJ', got '%s'", rootTask.ManagerDomain)
	}

	// Verify child tasks and delegations
	delegations, err := env.OrchRepo.GetDelegationsByOrchestrationID(ctx, orch.ID, userID)
	if err != nil {
		t.Fatalf("failed to get delegations: %v", err)
	}
	if len(delegations) != 2 {
		t.Fatalf("expected 2 child tasks for research workflow, got %d", len(delegations))
	}

	// Check dependency ordering: second task should be WAITING_FOR_DEPENDENCY
	firstTask, err := env.TaskRepo.GetTaskByID(ctx, delegations[0].ChildTaskID)
	if err != nil {
		t.Fatalf("failed to get first child task: %v", err)
	}
	secondTask, err := env.TaskRepo.GetTaskByID(ctx, delegations[1].ChildTaskID)
	if err != nil {
		t.Fatalf("failed to get second child task: %v", err)
	}

	if firstTask.Status != domain.TaskStatusQueued {
		t.Fatalf("expected first child task to be QUEUED, got %s", firstTask.Status)
	}
	if secondTask.Status != domain.TaskStatusWaitingForDependency {
		t.Fatalf("expected second child task to be WAITING_FOR_DEPENDENCY, got %s", secondTask.Status)
	}
}

func TestJJ_ReconcileOrchestration_CompletesLifecycle(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "jj_user2@example.com")

	req := &domain.UserRequest{
		UserID:      userID,
		RequestText: "Research Linux kernel cgroup v2 limits",
		Priority:    domain.TaskPriorityNormalP2,
	}

	orch, _, err := env.Orchestrator.SubmitRequest(ctx, req)
	if err != nil {
		t.Fatalf("failed to submit request: %v", err)
	}

	delegations, err := env.OrchRepo.GetDelegationsByOrchestrationID(ctx, orch.ID, userID)
	if err != nil {
		t.Fatalf("failed to get delegations: %v", err)
	}

	// Step 1: Claim and complete first child task
	claimed1, err := env.Engine.ClaimTask(ctx, "worker-1", 60*time.Second)
	if err != nil || claimed1 == nil {
		t.Fatalf("failed to claim task 1: %v", err)
	}
	err = env.Engine.CompleteTask(ctx, claimed1.ID, "worker-1", json.RawMessage(`{"status":"success","citations":["kernel.org"]}`))
	if err != nil {
		t.Fatalf("failed to complete task 1: %v", err)
	}

	// Verify second task unblocked to QUEUED
	task2, err := env.TaskRepo.GetTaskByID(ctx, delegations[1].ChildTaskID)
	if err != nil {
		t.Fatalf("failed to load task 2: %v", err)
	}
	if task2.Status != domain.TaskStatusQueued {
		t.Fatalf("expected task 2 to be unblocked to QUEUED, got %s", task2.Status)
	}

	// Reconcile in intermediate state: should remain RUNNING
	intermediateOrch, err := env.Orchestrator.ReconcileOrchestration(ctx, orch.ID)
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}
	if intermediateOrch.Status != domain.OrchestrationStatusRunning {
		t.Fatalf("expected status RUNNING, got %s", intermediateOrch.Status)
	}

	// Step 2: Claim and complete second child task
	claimed2, err := env.Engine.ClaimTask(ctx, "worker-2", 60*time.Second)
	if err != nil || claimed2 == nil {
		t.Fatalf("failed to claim task 2: %v", err)
	}
	err = env.Engine.CompleteTask(ctx, claimed2.ID, "worker-2", json.RawMessage(`{"status":"success","summary":"Cgroup v2 unified hierarchy verified."}`))
	if err != nil {
		t.Fatalf("failed to complete task 2: %v", err)
	}

	// Final reconcile: should transition to COMPLETED
	completedOrch, err := env.Orchestrator.ReconcileOrchestration(ctx, orch.ID)
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}

	if completedOrch.Status != domain.OrchestrationStatusCompleted {
		t.Fatalf("expected status COMPLETED, got %s", completedOrch.Status)
	}
	if completedOrch.FinalResponse == nil || *completedOrch.FinalResponse == "" {
		t.Fatal("expected non-empty final response synthesized by JJ")
	}

	// Verify root task is also completed
	rootTask, err := env.TaskRepo.GetTaskByID(ctx, orch.RootTaskID)
	if err != nil {
		t.Fatalf("failed to load root task: %v", err)
	}
	if rootTask.Status != domain.TaskStatusCompleted {
		t.Fatalf("expected root task COMPLETED, got %s", rootTask.Status)
	}
}

func TestJJ_ReconcileOrchestration_ChildFailureAbortsOrchestration(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "jj_fail_user@example.com")

	req := &domain.UserRequest{
		UserID:      userID,
		RequestText: "Research non-existent quantum framework",
	}

	orch, _, err := env.Orchestrator.SubmitRequest(ctx, req)
	if err != nil {
		t.Fatalf("failed to submit request: %v", err)
	}

	// Claim child task before failing
	claimed, err := env.Engine.ClaimTask(ctx, "worker-1", 60*time.Second)
	if err != nil || claimed == nil {
		t.Fatalf("failed to claim task: %v", err)
	}

	// Fail child task non-retryably
	err = env.Engine.FailTask(ctx, claimed.ID, "worker-1", "Source not found 404", false)
	if err != nil {
		t.Fatalf("failed to fail task: %v", err)
	}

	// Reconcile
	reconciled, err := env.Orchestrator.ReconcileOrchestration(ctx, orch.ID)
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}

	if reconciled.Status != domain.OrchestrationStatusFailed {
		t.Fatalf("expected orchestration status FAILED, got %s", reconciled.Status)
	}
	if reconciled.ErrorMessage == nil || *reconciled.ErrorMessage == "" {
		t.Fatal("expected error message describing child failure")
	}

	// Verify root task marked failed
	rootTask, _ := env.TaskRepo.GetTaskByID(ctx, orch.RootTaskID)
	if rootTask.Status != domain.TaskStatusFailed {
		t.Fatalf("expected root task FAILED, got %s", rootTask.Status)
	}
}

func TestJJ_ApprovalRequirement_PausesOrchestrationAndResumesOnApproval(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "jj_approval_user@example.com")

	// Engineering request that modifies codebase
	req := &domain.UserRequest{
		UserID:      userID,
		RequestText: "Fix bug in task engine lease timeout and modify repo",
		Priority:    domain.TaskPriorityHighP1,
	}

	orch, _, err := env.Orchestrator.SubmitRequest(ctx, req)
	if err != nil {
		t.Fatalf("failed to submit request: %v", err)
	}

	delegations, _ := env.OrchRepo.GetDelegationsByOrchestrationID(ctx, orch.ID, userID)
	if len(delegations) != 2 {
		t.Fatalf("expected 2 engineering tasks, got %d", len(delegations))
	}

	// Retrieve pending approval for the sensitive modify task
	pendingStatus := domain.ApprovalStatusPending
	approvals, err := env.ApprovalRepo.ListApprovals(ctx, userID, &pendingStatus)
	if err != nil || len(approvals) == 0 {
		t.Fatalf("expected pending approval, got %v (count %d)", err, len(approvals))
	}

	// Orchestration status should reflect AWAITING_APPROVAL
	if orch.Status != domain.OrchestrationStatusAwaitingApproval {
		t.Fatalf("expected initial orchestration status AWAITING_APPROVAL, got %s", orch.Status)
	}

	// Claim and complete first task (inspection)
	claimedInspect, err := env.Engine.ClaimTask(ctx, "worker-1", 60*time.Second)
	if err != nil || claimedInspect == nil {
		t.Fatalf("failed to claim inspect task: %v", err)
	}
	_ = env.Engine.CompleteTask(ctx, claimedInspect.ID, "worker-1", json.RawMessage(`{"status":"success","diff":"found bug"}`))

	// User grants approval
	_, err = env.Engine.RespondApproval(ctx, approvals[0].ID, userID, true, "Approved to modify codebase")
	if err != nil {
		t.Fatalf("failed to respond to approval: %v", err)
	}

	// Now modify task should be unblocked and available to claim
	claimedModify, err := env.Engine.ClaimTask(ctx, "worker-2", 60*time.Second)
	if err != nil || claimedModify == nil {
		t.Fatalf("failed to claim modify task: %v", err)
	}
	_ = env.Engine.CompleteTask(ctx, claimedModify.ID, "worker-2", json.RawMessage(`{"status":"success","changes_applied":true}`))

	// Final reconcile: should reach COMPLETED
	finalOrch, err := env.Orchestrator.ReconcileOrchestration(ctx, orch.ID)
	if err != nil {
		t.Fatalf("reconcile error: %v", err)
	}
	if finalOrch.Status != domain.OrchestrationStatusCompleted {
		t.Fatalf("expected final status COMPLETED, got %s", finalOrch.Status)
	}
}

func TestJJ_ReconcileInFlight_ProcessRestartRecovery(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "jj_restart_user@example.com")

	// Submit two requests
	req1 := &domain.UserRequest{UserID: userID, RequestText: "Research concurrency patterns in Go"}
	req2 := &domain.UserRequest{UserID: userID, RequestText: "Research distributed locking with Postgres"}

	orch1, _, err := env.Orchestrator.SubmitRequest(ctx, req1)
	if err != nil {
		t.Fatalf("failed to submit req1: %v", err)
	}
	orch2, _, err := env.Orchestrator.SubmitRequest(ctx, req2)
	if err != nil {
		t.Fatalf("failed to submit req2: %v", err)
	}

	// Claim and complete all tasks for orch1
	// Orch1 has 2 research tasks (gather, then synthesize)
	for i := 0; i < 2; i++ {
		claimed, err := env.Engine.ClaimTask(ctx, "w1", 60*time.Second)
		if err != nil || claimed == nil {
			t.Fatalf("failed to claim task %d for orch1: %v", i, err)
		}
		_ = env.Engine.CompleteTask(ctx, claimed.ID, "w1", json.RawMessage(`{"status":"success","citations":["kernel.org"]}`))
	}

	// Simulate complete process crash and reboot by instantiating brand new JJOrchestrator instance
	newOrchestrator := service.NewJJOrchestrator(
		env.Engine, env.OrchRepo, env.TaskRepo, env.ManagerReg, env.AgentFactory, nil, nil,
	)

	// Run recovery reconciliation on startup
	if err := newOrchestrator.ReconcileInFlight(ctx); err != nil {
		t.Fatalf("failed to reconcile in-flight on startup: %v", err)
	}

	// Check orch1: should now be COMPLETED
	reloaded1, _ := env.OrchRepo.GetOrchestrationByID(ctx, orch1.ID, userID)
	if reloaded1.Status != domain.OrchestrationStatusCompleted {
		t.Fatalf("expected orch1 to be COMPLETED, got %s", reloaded1.Status)
	}

	// Check orch2: still in flight, should be RUNNING
	reloaded2, _ := env.OrchRepo.GetOrchestrationByID(ctx, orch2.ID, userID)
	if reloaded2.Status != domain.OrchestrationStatusRunning {
		t.Fatalf("expected orch2 to remain RUNNING, got %s", reloaded2.Status)
	}
}

type invalidMockPlanner struct{}

func (p *invalidMockPlanner) GeneratePlan(ctx context.Context, req *domain.UserRequest, mgrReg *manager.Registry) (*domain.PlanPayload, error) {
	// Returns a plan with a cyclic dependency loop
	return &domain.PlanPayload{
		Objective:          "Cyclic plan",
		InterpretedRequest: "Invalid loop",
		ManagerID:          "research",
		ChildTasks: []domain.ChildTaskPlan{
			{Key: "a", Title: "Task A", Goal: "Do A"},
			{Key: "b", Title: "Task B", Goal: "Do B"},
		},
		Dependencies: []domain.PlanDependency{
			{TaskKey: "b", DependsOnKey: "a"},
			{TaskKey: "a", DependsOnKey: "b"}, // cycle!
		},
	}, nil
}

func TestJJ_InvalidPlanRejection(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "jj_invalid_plan@example.com")

	orchestratorWithBadPlanner := service.NewJJOrchestrator(
		env.Engine, env.OrchRepo, env.TaskRepo, env.ManagerReg, env.AgentFactory, &invalidMockPlanner{}, nil,
	)

	req := &domain.UserRequest{
		UserID:      userID,
		RequestText: "Invalid cyclic request",
	}

	orch, plan, err := orchestratorWithBadPlanner.SubmitRequest(ctx, req)
	if err == nil {
		t.Fatal("expected error submitting invalid plan")
	}

	if plan.Status != domain.PlanStatusRejected {
		t.Fatalf("expected plan status REJECTED, got %s", plan.Status)
	}
	if len(plan.ValidationErrors) == 0 {
		t.Fatal("expected validation errors recorded in plan")
	}

	// Verify orchestration marked FAILED
	reloadedOrch, _ := env.OrchRepo.GetOrchestrationByID(ctx, orch.ID, userID)
	if reloadedOrch.Status != domain.OrchestrationStatusFailed {
		t.Fatalf("expected orchestration status FAILED, got %s", reloadedOrch.Status)
	}

	// Verify no child tasks were delegated
	delegations, _ := env.OrchRepo.GetDelegationsByOrchestrationID(ctx, orch.ID, userID)
	if len(delegations) > 0 {
		t.Fatalf("expected 0 delegations for rejected plan, got %d", len(delegations))
	}
}

func TestJJ_ConcurrentOrchestrations(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "jj_concurrent@example.com")

	const concurrency = 8
	var wg sync.WaitGroup
	errs := make(chan error, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := &domain.UserRequest{
				UserID:      userID,
				RequestText: fmt.Sprintf("Concurrent research query %d: Distributed consensus models", idx),
				Priority:    domain.TaskPriorityNormalP2,
			}
			orch, _, err := env.Orchestrator.SubmitRequest(ctx, req)
			if err != nil {
				errs <- fmt.Errorf("concurrent submit %d failed: %w", idx, err)
				return
			}
			if orch.ID == uuid.Nil {
				errs <- fmt.Errorf("concurrent submit %d returned nil ID", idx)
				return
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatal(err)
	}

	// Verify all 8 orchestrations exist and have child tasks
	list, err := env.OrchRepo.ListOrchestrations(ctx, userID, "")
	if err != nil {
		t.Fatalf("failed to list orchestrations: %v", err)
	}
	if len(list) != concurrency {
		t.Fatalf("expected %d orchestrations, got %d", concurrency, len(list))
	}
}

func TestJJ_OptInLiveAntigravityIntegration(t *testing.T) {
	if os.Getenv("GRIFFIN_ENABLE_LIVE_AGY_TEST") != "true" {
		t.Skip("Skipping live Antigravity CLI integration test. Set GRIFFIN_ENABLE_LIVE_AGY_TEST=true to execute.")
	}

	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	userID := CreateTestUser(t, env.DB, "jj_live_user@example.com")

	// Initialize real Antigravity executor
	agyExec, err := antigravity.NewAntigravityExecutor(antigravity.ExecutorConfig{
		BinaryPath:       "/usr/bin/agy",
		MaxConcurrent:    1,
		ExecutionTimeout: 45 * time.Second,
		WorkspaceRoot:    "/tmp/griffin_test_workspaces",
	})
	if err != nil {
		t.Fatalf("failed to initialize live agy executor: %v", err)
	}

	planAdapter := worker.NewPlanExecutorAdapter(agyExec)
	aiPlanner := service.NewAIPlanner(planAdapter)

	liveOrchestrator := service.NewJJOrchestrator(
		env.Engine, env.OrchRepo, env.TaskRepo, env.ManagerReg, env.AgentFactory, aiPlanner, nil,
	)

	req := &domain.UserRequest{
		UserID:      userID,
		RequestText: "Explain how memory management works in Go Graviton instances",
		Priority:    domain.TaskPriorityNormalP2,
	}

	orch, plan, err := liveOrchestrator.SubmitRequest(ctx, req)
	if err != nil {
		t.Fatalf("live submit failed: %v", err)
	}

	if orch.ID == uuid.Nil || plan.ID == uuid.Nil {
		t.Fatal("expected non-nil IDs from live orchestrator")
	}
	t.Logf("Live JJ Orchestration successful! ID=%s, Manager=%s", orch.ID, orch.ManagerID)
}

// A double tap or client retry must not queue the same paid plan twice, but a request that finished
// (or was cancelled) can be submitted again, and different text never collides.
func TestJJ_SubmitRequest_RejectsActiveDuplicate(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "jj_dup@example.com")
	other := CreateTestUser(t, env.DB, "jj_dup_other@example.com")
	text := "Research the weekly status report format"

	first, _, err := env.Orchestrator.SubmitRequest(ctx, &domain.UserRequest{UserID: userID, RequestText: text})
	if err != nil {
		t.Fatalf("first submit: %v", err)
	}
	if _, _, err := env.Orchestrator.SubmitRequest(ctx, &domain.UserRequest{UserID: userID, RequestText: text}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("identical active request: want ErrConflict, got %v", err)
	}
	if _, _, err := env.Orchestrator.SubmitRequest(ctx, &domain.UserRequest{UserID: userID, RequestText: text + " v2"}); err != nil {
		t.Fatalf("different text must be accepted: %v", err)
	}
	if _, _, err := env.Orchestrator.SubmitRequest(ctx, &domain.UserRequest{UserID: other, RequestText: text}); err != nil {
		t.Fatalf("another user's identical text must be accepted: %v", err)
	}

	// Once the first run is no longer active, the same text is allowed again.
	if err := env.OrchRepo.UpdateOrchestrationStatus(ctx, first.ID, domain.OrchestrationStatusCancelled, nil, nil); err != nil {
		t.Fatalf("cancel first: %v", err)
	}
	if _, _, err := env.Orchestrator.SubmitRequest(ctx, &domain.UserRequest{UserID: userID, RequestText: text}); err != nil {
		t.Fatalf("resubmit after the first finished: %v", err)
	}
}

// Every delegated step records which provider it is planned for, chosen by what the step needs.
func TestJJ_SubmitRequest_RecordsProviderHintOnEachStep(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "jj_hint@example.com")
	orch, _, err := env.Orchestrator.SubmitRequest(ctx, &domain.UserRequest{UserID: userID, RequestText: "Research best practices for Go worker pools"})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	dels, err := env.OrchRepo.GetDelegationsByOrchestrationID(ctx, orch.ID, userID)
	if err != nil || len(dels) == 0 {
		t.Fatalf("delegations: %v (%d)", err, len(dels))
	}
	for _, d := range dels {
		task, err := env.TaskRepo.GetTaskByID(ctx, d.ChildTaskID)
		if err != nil {
			t.Fatal(err)
		}
		var p map[string]any
		if err := json.Unmarshal(task.InputPayload, &p); err != nil {
			t.Fatalf("child payload is not an object: %s", task.InputPayload)
		}
		if h := p[domain.ProviderHintKey]; h != "agy" && h != "claude" {
			t.Fatalf("step %q has no provider hint: %s", task.Title, task.InputPayload)
		}
	}
}

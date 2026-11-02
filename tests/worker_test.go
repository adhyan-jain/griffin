package tests

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/worker"
)

func TestWorkerEndToEndExecution(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	userID := CreateTestUser(t, env.DB, "worker_e2e@example.com")
	mockExec := worker.NewMockExecutor()

	w := worker.NewWorker(worker.Config{
		ID:                "test-worker-1",
		Engine:            env.Engine,
		TaskRepo:          env.TaskRepo,
		Executor:          mockExec,
		LeaseDuration:     10 * time.Second,
		HeartbeatInterval: 1 * time.Second,
		PollInterval:      100 * time.Millisecond,
	})

	// Create task
	task, err := env.Engine.CreateTask(ctx, &domain.Task{
		UserID:   userID,
		Title:    "End-to-End Worker Task",
		Goal:     "Verify mock executor completes task and records attempt",
		Priority: domain.TaskPriorityHighP1,
	}, nil)
	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	// Worker processes 1 task
	processed, err := w.ProcessNext(ctx)
	if err != nil {
		t.Fatalf("worker ProcessNext returned error: %v", err)
	}
	if !processed {
		t.Fatal("expected worker to claim and process 1 task")
	}

	// Verify task reached COMPLETED
	completedTask, err := env.Engine.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("failed to retrieve task: %v", err)
	}
	if completedTask.Status != domain.TaskStatusCompleted {
		t.Fatalf("expected task status COMPLETED, got %s", completedTask.Status)
	}
	if completedTask.CompletedAt == nil {
		t.Fatal("expected completed_at to be populated")
	}

	// Verify output payload
	var outMap map[string]any
	if err := json.Unmarshal(completedTask.OutputPayload, &outMap); err != nil {
		t.Fatalf("failed to parse output payload: %v", err)
	}
	if outMap["status"] != "mock_execution_success" {
		t.Errorf("expected output status 'mock_execution_success', got %v", outMap["status"])
	}

	// Verify execution attempts recorded
	attempts, err := env.TaskRepo.GetAttempts(ctx, task.ID)
	if err != nil {
		t.Fatalf("failed to get attempts: %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("expected 1 attempt recorded, got %d", len(attempts))
	}
	if attempts[0].Status != "COMPLETED" {
		t.Errorf("expected attempt status COMPLETED, got %s", attempts[0].Status)
	}
	if attempts[0].WorkerID != "test-worker-1" {
		t.Errorf("expected worker_id 'test-worker-1', got %s", attempts[0].WorkerID)
	}
}

func TestWorkerFailureAndRetryAttempt(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	userID := CreateTestUser(t, env.DB, "worker_fail@example.com")
	mockExec := worker.NewMockExecutor()

	w := worker.NewWorker(worker.Config{
		ID:                "test-worker-fail",
		Engine:            env.Engine,
		TaskRepo:          env.TaskRepo,
		Executor:          mockExec,
		LeaseDuration:     10 * time.Second,
		HeartbeatInterval: 1 * time.Second,
		PollInterval:      100 * time.Millisecond,
	})

	task, err := env.Engine.CreateTask(ctx, &domain.Task{
		UserID:     userID,
		Title:      "Failing Worker Task",
		Goal:       "Verify worker handles executor error gracefully",
		MaxRetries: 3,
	}, nil)
	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	// Configure mock executor to fail this task
	mockExec.FailTasks[task.ID] = true

	// Worker processes task
	processed, err := w.ProcessNext(ctx)
	if err != nil {
		t.Fatalf("ProcessNext failed: %v", err)
	}
	if !processed {
		t.Fatal("expected task to be processed")
	}

	// Task should now be in RETRYING
	retriedTask, err := env.Engine.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if retriedTask.Status != domain.TaskStatusRetrying {
		t.Fatalf("expected task status RETRYING, got %s", retriedTask.Status)
	}
	if retriedTask.RetryCount != 1 {
		t.Errorf("expected retry_count 1, got %d", retriedTask.RetryCount)
	}

	// Attempt record should be FAILED
	attempts, err := env.TaskRepo.GetAttempts(ctx, task.ID)
	if err != nil {
		t.Fatalf("get attempts: %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(attempts))
	}
	if attempts[0].Status != "FAILED" {
		t.Errorf("expected attempt status FAILED, got %s", attempts[0].Status)
	}
}

func TestWorkerApprovalPause(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	userID := CreateTestUser(t, env.DB, "worker_app@example.com")
	mockExec := worker.NewMockExecutor()

	w := worker.NewWorker(worker.Config{
		ID:                "test-worker-app",
		Engine:            env.Engine,
		TaskRepo:          env.TaskRepo,
		Executor:          mockExec,
		LeaseDuration:     10 * time.Second,
		HeartbeatInterval: 1 * time.Second,
		PollInterval:      100 * time.Millisecond,
	})

	task, err := env.Engine.CreateTask(ctx, &domain.Task{
		UserID: userID,
		Title:  "Financial Transfer Task",
		Goal:   "Submit transfer requiring approval",
	}, nil)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	mockExec.RequireApprovalTasks[task.ID] = &domain.Approval{
		ID:            uuid.New(),
		UserID:        userID,
		TaskID:        task.ID,
		ActionType:    "FINANCIAL_TRANSFER",
		RiskLevel:     domain.ApprovalRiskCritical,
		Summary:       "Initiate $500 transfer to vendor",
		ActionPayload: json.RawMessage(`{"amount": 500, "vendor": "Stripe"}`),
	}

	processed, err := w.ProcessNext(ctx)
	if err != nil {
		t.Fatalf("ProcessNext failed: %v", err)
	}
	if !processed {
		t.Fatal("expected task to be processed")
	}

	// Task should be in AWAITING_APPROVAL
	pausedTask, err := env.Engine.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if pausedTask.Status != domain.TaskStatusAwaitingApproval {
		t.Fatalf("expected task status AWAITING_APPROVAL, got %s", pausedTask.Status)
	}
}

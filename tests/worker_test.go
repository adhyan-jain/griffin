package tests

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/provider/antigravity"
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

func TestWorkerAntigravityAuthFailureEvent(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	userID := CreateTestUser(t, env.DB, "worker_agy_auth@example.com")

	// Create mock script simulating expired credentials
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "mock_agy_auth_fail.sh")
	err := os.WriteFile(scriptPath, []byte("#!/bin/bash\necho 'error: authentication required. Please run agy auth login' >&2\nexit 1\n"), 0755)
	if err != nil {
		t.Fatalf("failed to write script: %v", err)
	}

	agyExec, err := antigravity.NewAntigravityExecutor(antigravity.ExecutorConfig{
		BinaryPath:       scriptPath,
		WorkspaceRoot:    t.TempDir(),
		ExecutionTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("create antigravity executor: %v", err)
	}

	w := worker.NewWorker(worker.Config{
		ID:                "test-worker-agy-auth",
		Engine:            env.Engine,
		TaskRepo:          env.TaskRepo,
		Executor:          agyExec,
		LeaseDuration:     10 * time.Second,
		HeartbeatInterval: 1 * time.Second,
		PollInterval:      100 * time.Millisecond,
	})

	task, err := env.Engine.CreateTask(ctx, &domain.Task{
		UserID:     userID,
		Title:      "Antigravity Auth Failure Task",
		Goal:       "Trigger auth error handling",
		MaxRetries: 3,
	}, nil)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	processed, err := w.ProcessNext(ctx)
	if err != nil {
		t.Fatalf("ProcessNext failed: %v", err)
	}
	if !processed {
		t.Fatal("expected task to be processed")
	}

	// Verify task failed permanently (no retries scheduled)
	failedTask, err := env.Engine.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if failedTask.Status != domain.TaskStatusFailed {
		t.Fatalf("expected task status FAILED, got %s", failedTask.Status)
	}
	if failedTask.RetryCount != 0 {
		t.Fatalf("expected RetryCount 0 (retries disabled for auth failure), got %d", failedTask.RetryCount)
	}

	// Verify AUTHENTICATION_REQUIRED event exists in the audit ledger
	events, err := env.TaskRepo.GetEvents(ctx, task.ID)
	if err != nil {
		t.Fatalf("get events: %v", err)
	}
	foundAuthEvent := false
	for _, e := range events {
		if e.EventType == "AUTHENTICATION_REQUIRED" {
			foundAuthEvent = true
			break
		}
	}
	if !foundAuthEvent {
		t.Fatalf("expected AUTHENTICATION_REQUIRED event in task events, got: %+v", events)
	}
}

func TestWorkerAntigravitySuccess(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	userID := CreateTestUser(t, env.DB, "worker_agy_ok@example.com")

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "mock_agy_ok.sh")
	err := os.WriteFile(scriptPath, []byte(`#!/bin/bash
echo '{"conversation_id":"conv-e2e-123","status":"SUCCESS","response":"Worker executed successfully","num_turns":1,"structured_output":{"result":"ok"},"usage":{"input_tokens":50,"output_tokens":25,"total_tokens":75}}'
exit 0
`), 0755)
	if err != nil {
		t.Fatalf("write script: %v", err)
	}

	agyExec, err := antigravity.NewAntigravityExecutor(antigravity.ExecutorConfig{
		BinaryPath:       scriptPath,
		WorkspaceRoot:    t.TempDir(),
		ExecutionTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("create executor: %v", err)
	}

	w := worker.NewWorker(worker.Config{
		ID:                "test-worker-agy-ok",
		Engine:            env.Engine,
		TaskRepo:          env.TaskRepo,
		Executor:          agyExec,
		LeaseDuration:     10 * time.Second,
		HeartbeatInterval: 1 * time.Second,
		PollInterval:      100 * time.Millisecond,
	})

	task, err := env.Engine.CreateTask(ctx, &domain.Task{
		UserID: userID,
		Title:  "Antigravity Success Task",
		Goal:   "Run successful prompt",
	}, nil)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	processed, err := w.ProcessNext(ctx)
	if err != nil {
		t.Fatalf("ProcessNext failed: %v", err)
	}
	if !processed {
		t.Fatal("expected task to be processed")
	}

	completedTask, err := env.Engine.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if completedTask.Status != domain.TaskStatusCompleted {
		t.Fatalf("expected COMPLETED status, got %s", completedTask.Status)
	}
	if len(completedTask.OutputPayload) == 0 {
		t.Fatal("expected non-empty output payload")
	}

	// Verify attempt record was saved with token usage
	attempts, err := env.TaskRepo.GetAttempts(ctx, task.ID)
	if err != nil {
		t.Fatalf("get attempts: %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(attempts))
	}
	if attempts[0].Status != "COMPLETED" {
		t.Fatalf("expected attempt status COMPLETED, got %s", attempts[0].Status)
	}
	if attempts[0].PromptTokens != 50 || attempts[0].CompletionTokens != 25 {
		t.Fatalf("expected prompt=50, completion=25 tokens, got %d / %d", attempts[0].PromptTokens, attempts[0].CompletionTokens)
	}
}

// A finished task's answer is kept as a file in Files, linked to the task.
func TestWorkerSavesResultAsArtifact(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	userID := CreateTestUser(t, env.DB, "result_file@example.com")
	task, err := env.Engine.CreateTask(ctx, &domain.Task{UserID: userID, ManagerDomain: "research", Title: "Weekly internship list", Goal: "list internships", Priority: domain.TaskPriorityNormalP2}, nil)
	if err != nil {
		t.Fatal(err)
	}
	w := worker.NewWorker(worker.Config{ID: "w-file", Engine: env.Engine, TaskRepo: env.TaskRepo, Executor: worker.NewMockExecutor(), Artifacts: env.StorageService, PollInterval: 50 * time.Millisecond})
	// Stop the worker and wait for it before the environment is torn down, so it can never hold a
	// lock on the shared test database while the next test truncates it.
	stopped := make(chan struct{})
	go func() { _ = w.Run(ctx); close(stopped) }()
	defer func() { cancel(); <-stopped }()

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		tid := task.ID
		arts, err := env.ArtifactRepo.ListArtifacts(ctx, userID, &tid)
		if err == nil && len(arts) == 1 {
			a := arts[0]
			if a.Title != "Weekly internship list" || a.Filename == nil || !strings.HasPrefix(*a.Filename, "weekly-internship-list.") {
				t.Fatalf("unexpected artifact: %+v", a)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("no result artifact was saved for the finished task")
}

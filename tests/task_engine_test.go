package tests

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
)

func TestTaskCreationAndRetrieval(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "user1@example.com")

	task := &domain.Task{
		UserID:        userID,
		ManagerDomain: "JJ",
		Title:         "Synthesize research report",
		Goal:          "Collect primary sources and compile markdown summary",
		InputPayload:  json.RawMessage(`{"topic":"Antigravity architecture"}`),
		Priority:      domain.TaskPriorityHighP1,
	}

	created, err := env.Engine.CreateTask(ctx, task, nil)
	if err != nil {
		t.Fatalf("expected successful task creation, got: %v", err)
	}

	if created.ID == uuid.Nil {
		t.Fatal("expected non-nil task ID")
	}
	if created.Status != domain.TaskStatusQueued {
		t.Fatalf("expected initial status QUEUED, got: %s", created.Status)
	}
	if created.Priority != domain.TaskPriorityHighP1 {
		t.Fatalf("expected priority HIGH_P1, got: %s", created.Priority)
	}

	// Retrieve by ID
	retrieved, err := env.Engine.GetTask(ctx, created.ID)
	if err != nil {
		t.Fatalf("failed to retrieve task: %v", err)
	}
	if retrieved.Title != task.Title {
		t.Errorf("expected title %q, got %q", task.Title, retrieved.Title)
	}

	// Verify events
	events, err := env.TaskRepo.GetEvents(ctx, created.ID)
	if err != nil {
		t.Fatalf("failed to retrieve task events: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected at least 1 task event recorded upon creation")
	}
}

func TestTaskIdempotency(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "user_idempotent@example.com")
	idemKey := "idem-key-unique-12345"

	task1 := &domain.Task{
		UserID:         userID,
		Title:          "First Task",
		Goal:           "Ensure idempotency works",
		IdempotencyKey: &idemKey,
	}

	created1, err := env.Engine.CreateTask(ctx, task1, nil)
	if err != nil {
		t.Fatalf("first creation failed: %v", err)
	}

	// Attempt duplicate submission with the same key
	task2 := &domain.Task{
		UserID:         userID,
		Title:          "Second Task (Duplicate)",
		Goal:           "This should be blocked by idempotency",
		IdempotencyKey: &idemKey,
	}

	_, err = env.Engine.CreateTask(ctx, task2, nil)
	if err == nil {
		t.Fatal("expected ErrIdempotentConflict, got nil")
	}
	if !errors.Is(err, domain.ErrIdempotentConflict) {
		t.Fatalf("expected ErrIdempotentConflict, got: %v", err)
	}

	// Verify only 1 task exists in database for this key
	retrieved, err := env.Engine.GetTask(ctx, created1.ID)
	if err != nil {
		t.Fatalf("failed to retrieve original task: %v", err)
	}
	if retrieved.Title != "First Task" {
		t.Errorf("expected original task title 'First Task', got %q", retrieved.Title)
	}
}

func TestConcurrentWorkerClaims(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "concurrency@example.com")

	// Create 10 tasks in QUEUED state
	numTasks := 10
	taskIDs := make(map[uuid.UUID]bool)
	for i := 0; i < numTasks; i++ {
		task := &domain.Task{
			UserID:   userID,
			Title:    "Concurrent Task",
			Goal:     "Test SKIP LOCKED claiming safety",
			Priority: domain.TaskPriorityNormalP2,
		}
		created, err := env.Engine.CreateTask(ctx, task, nil)
		if err != nil {
			t.Fatalf("failed to create task %d: %v", i, err)
		}
		taskIDs[created.ID] = true
	}

	// Launch 10 concurrent workers competing to claim tasks
	numWorkers := 10
	claimedTasks := make(map[uuid.UUID]string) // taskID -> workerID
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerIdx int) {
			defer wg.Done()
			workerID := "worker-" + uuid.New().String()[:8]

			claimed, err := env.Engine.ClaimTask(ctx, workerID, 60*time.Second)
			if err != nil {
				t.Errorf("worker %s encountered claim error: %v", workerID, err)
				return
			}
			if claimed != nil {
				mu.Lock()
				if priorWorker, exists := claimedTasks[claimed.ID]; exists {
					t.Errorf("DOUBLE CLAIM DETECTED! Task %s claimed by both %s and %s", claimed.ID, priorWorker, workerID)
				}
				claimedTasks[claimed.ID] = workerID
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()

	if len(claimedTasks) != numTasks {
		t.Fatalf("expected all %d tasks to be claimed by concurrent workers, but only %d were claimed", numTasks, len(claimedTasks))
	}

	// Verify each task is marked RUNNING with its specific leased_by
	for id, workerID := range claimedTasks {
		tRecord, err := env.Engine.GetTask(ctx, id)
		if err != nil {
			t.Fatalf("failed to get claimed task %s: %v", id, err)
		}
		if tRecord.Status != domain.TaskStatusRunning {
			t.Errorf("expected task %s status RUNNING, got %s", id, tRecord.Status)
		}
		if tRecord.LeasedBy == nil || *tRecord.LeasedBy != workerID {
			t.Errorf("expected leased_by %s, got %v", workerID, tRecord.LeasedBy)
		}
	}
}

func TestHeartbeatAndLeaseRenewal(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "heartbeat@example.com")
	workerID := "worker-hb-1"

	_, err := env.Engine.CreateTask(ctx, &domain.Task{
		UserID: userID,
		Title:  "Heartbeat Task",
		Goal:   "Validate lease renewal",
	}, nil)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	// Worker claims with 5 second lease
	claimed, err := env.Engine.ClaimTask(ctx, workerID, 5*time.Second)
	if err != nil || claimed == nil {
		t.Fatalf("claim task failed: %v", err)
	}

	initialExpiry := *claimed.LeaseExpiresAt

	// Sleep 1 second then send heartbeat renewing with 10 second lease
	time.Sleep(1 * time.Second)
	err = env.Engine.Heartbeat(ctx, claimed.ID, workerID, 10*time.Second)
	if err != nil {
		t.Fatalf("heartbeat renewal failed: %v", err)
	}

	renewed, err := env.Engine.GetTask(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("get renewed task: %v", err)
	}

	if !renewed.LeaseExpiresAt.After(initialExpiry) {
		t.Fatalf("expected new lease expiry (%v) to be after initial expiry (%v)", renewed.LeaseExpiresAt, initialExpiry)
	}

	// Different worker cannot renew the lease
	err = env.Engine.Heartbeat(ctx, claimed.ID, "worker-impostor", 10*time.Second)
	if err == nil {
		t.Fatal("expected ErrLeaseExpired on unauthorized worker heartbeat, got nil")
	}
}

func TestLeaseExpiryAndReaperRecovery(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "reaper@example.com")
	workerID := "worker-crashed-1"

	_, err := env.Engine.CreateTask(ctx, &domain.Task{
		UserID:     userID,
		Title:      "Orphan Task",
		Goal:       "Test lease reaper recovery",
		MaxRetries: 3,
	}, nil)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	// Claim task
	claimed, err := env.Engine.ClaimTask(ctx, workerID, 1*time.Second)
	if err != nil || claimed == nil {
		t.Fatalf("claim failed: %v", err)
	}

	// Artificially age the lease into the past
	pastTime := time.Now().UTC().Add(-10 * time.Second)
	claimed.LeaseExpiresAt = &pastTime
	if err := env.TaskRepo.UpdateTask(ctx, claimed); err != nil {
		t.Fatalf("failed to age lease: %v", err)
	}

	// Trigger reaper
	reapedCount, err := env.Engine.ReapExpiredLeases(ctx)
	if err != nil {
		t.Fatalf("reap expired leases error: %v", err)
	}
	if reapedCount != 1 {
		t.Fatalf("expected 1 reaped task, got %d", reapedCount)
	}

	// Verify task transitioned to RETRYING with retry_count = 1 and cleared lease
	reapedTask, err := env.Engine.GetTask(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("get reaped task: %v", err)
	}

	if reapedTask.Status != domain.TaskStatusRetrying {
		t.Errorf("expected status RETRYING, got %s", reapedTask.Status)
	}
	if reapedTask.RetryCount != 1 {
		t.Errorf("expected retry_count 1, got %d", reapedTask.RetryCount)
	}
	if reapedTask.LeasedBy != nil {
		t.Errorf("expected leased_by to be cleared, got %v", reapedTask.LeasedBy)
	}
}

func TestDependencyBlockingAndUnblocking(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "dag@example.com")
	workerID := "worker-dag-1"

	// 1. Create Parent Task
	parent, err := env.Engine.CreateTask(ctx, &domain.Task{
		UserID: userID,
		Title:  "Parent Task A",
		Goal:   "Generate dataset",
	}, nil)
	if err != nil {
		t.Fatalf("create parent task: %v", err)
	}
	if parent.Status != domain.TaskStatusQueued {
		t.Fatalf("parent should be QUEUED, got: %s", parent.Status)
	}

	// 2. Create Child Task depending on Parent
	child, err := env.Engine.CreateTask(ctx, &domain.Task{
		UserID: userID,
		Title:  "Child Task B",
		Goal:   "Train model on dataset",
	}, []uuid.UUID{parent.ID})
	if err != nil {
		t.Fatalf("create child task: %v", err)
	}
	if child.Status != domain.TaskStatusWaitingForDependency {
		t.Fatalf("child should be WAITING_FOR_DEPENDENCY, got: %s", child.Status)
	}

	// Worker claims parent task
	claimedParent, err := env.Engine.ClaimTask(ctx, workerID, 30*time.Second)
	if err != nil || claimedParent.ID != parent.ID {
		t.Fatalf("worker failed to claim parent: %v", err)
	}

	// Attempting to claim again yields nothing (child is still blocked!)
	emptyClaim, err := env.Engine.ClaimTask(ctx, workerID, 30*time.Second)
	if err != nil {
		t.Fatalf("claim attempt error: %v", err)
	}
	if emptyClaim != nil {
		t.Fatalf("child task should not be claimable while parent is running; claimed: %v", emptyClaim.ID)
	}

	// Complete parent task
	out := json.RawMessage(`{"dataset_size": 1000}`)
	err = env.Engine.CompleteTask(ctx, parent.ID, workerID, out)
	if err != nil {
		t.Fatalf("complete parent task failed: %v", err)
	}

	// Verify child task was automatically unblocked and transitioned to QUEUED
	unblockedChild, err := env.Engine.GetTask(ctx, child.ID)
	if err != nil {
		t.Fatalf("get unblocked child task: %v", err)
	}
	if unblockedChild.Status != domain.TaskStatusQueued {
		t.Fatalf("child task should have transitioned to QUEUED, but was %s", unblockedChild.Status)
	}

	// Now worker can claim child task!
	claimedChild, err := env.Engine.ClaimTask(ctx, workerID, 30*time.Second)
	if err != nil || claimedChild == nil {
		t.Fatalf("worker failed to claim unblocked child task: %v", err)
	}
	if claimedChild.ID != child.ID {
		t.Fatalf("expected claimed child ID %s, got %s", child.ID, claimedChild.ID)
	}
}

func TestCancellationCascadesToDependencies(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "cancel@example.com")

	// Parent task
	parent, _ := env.Engine.CreateTask(ctx, &domain.Task{
		UserID: userID,
		Title:  "Parent Task",
		Goal:   "Fetch remote resources",
	}, nil)

	// Child task depending on parent
	child, _ := env.Engine.CreateTask(ctx, &domain.Task{
		UserID: userID,
		Title:  "Child Task",
		Goal:   "Process remote resources",
	}, []uuid.UUID{parent.ID})

	// Cancel parent task
	err := env.Engine.CancelTask(ctx, parent.ID, "User cancelled batch job")
	if err != nil {
		t.Fatalf("cancel parent task failed: %v", err)
	}

	// Verify parent is CANCELLED
	pRecord, _ := env.Engine.GetTask(ctx, parent.ID)
	if pRecord.Status != domain.TaskStatusCancelled {
		t.Errorf("expected parent status CANCELLED, got %s", pRecord.Status)
	}

	// Verify child task was cascaded to CANCELLED
	cRecord, _ := env.Engine.GetTask(ctx, child.ID)
	if cRecord.Status != domain.TaskStatusCancelled {
		t.Errorf("expected child status CANCELLED, got %s", cRecord.Status)
	}

	// Attempting to cancel already cancelled task returns ErrTaskAlreadyTerminal
	err = env.Engine.CancelTask(ctx, parent.ID, "Duplicate cancel")
	if !errors.Is(err, domain.ErrTaskAlreadyTerminal) {
		t.Errorf("expected ErrTaskAlreadyTerminal, got %v", err)
	}
}

func TestApprovalCheckpointFlow(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "approvals@example.com")
	workerID := "worker-approval-1"

	task, _ := env.Engine.CreateTask(ctx, &domain.Task{
		UserID: userID,
		Title:  "Send Production Email",
		Goal:   "Email client regarding contract renewal",
	}, nil)

	// Worker claims task
	_, err := env.Engine.ClaimTask(ctx, workerID, 60*time.Second)
	if err != nil {
		t.Fatalf("claim task failed: %v", err)
	}

	// Worker requests approval for sensitive action
	approvalID := uuid.New()
	approval := &domain.Approval{
		ID:            approvalID,
		UserID:        userID,
		TaskID:        task.ID,
		ActionType:    "SEND_EMAIL",
		RiskLevel:     domain.ApprovalRiskHigh,
		Summary:       "Send proposal email to external partner",
		ActionPayload: json.RawMessage(`{"recipient": "partner@example.com"}`),
	}

	err = env.Engine.RequestApproval(ctx, task.ID, approval)
	if err != nil {
		t.Fatalf("request approval failed: %v", err)
	}

	// Verify task is now AWAITING_APPROVAL
	taskAfterApproval, _ := env.Engine.GetTask(ctx, task.ID)
	if taskAfterApproval.Status != domain.TaskStatusAwaitingApproval {
		t.Fatalf("expected task status AWAITING_APPROVAL, got %s", taskAfterApproval.Status)
	}

	// User approves
	responded, err := env.Engine.RespondApproval(ctx, approvalID, userID, true, "Looks great, proceed")
	if err != nil {
		t.Fatalf("respond approval failed: %v", err)
	}
	if responded.Status != domain.ApprovalStatusApproved {
		t.Errorf("expected approval status APPROVED, got %s", responded.Status)
	}

	// Verify task returned to QUEUED so worker can resume
	taskResumed, _ := env.Engine.GetTask(ctx, task.ID)
	if taskResumed.Status != domain.TaskStatusQueued {
		t.Fatalf("expected task to be QUEUED after approval, got %s", taskResumed.Status)
	}
}

func TestCountRunnableTasks(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "runnable_test@example.com")

	// Initially 0 runnable tasks
	count, err := env.TaskRepo.CountRunnableTasks(ctx)
	if err != nil {
		t.Fatalf("CountRunnableTasks failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 runnable tasks initially, got %d", count)
	}

	// Create parent task (QUEUED)
	parent, err := env.Engine.CreateTask(ctx, &domain.Task{
		UserID: userID,
		Title:  "Parent Task",
		Goal:   "Parent Goal",
	}, nil)
	if err != nil {
		t.Fatalf("create parent task: %v", err)
	}

	// Now count should be 1
	count, err = env.TaskRepo.CountRunnableTasks(ctx)
	if err != nil || count != 1 {
		t.Fatalf("expected 1 runnable task (parent queued), got %d (err: %v)", count, err)
	}

	// Create child task dependent on parent (WAITING_FOR_DEPENDENCY)
	child, err := env.Engine.CreateTask(ctx, &domain.Task{
		UserID: userID,
		Title:  "Child Task",
		Goal:   "Child Goal",
	}, []uuid.UUID{parent.ID})
	if err != nil {
		t.Fatalf("create child task: %v", err)
	}

	// Count should STILL be 1 because child is blocked on parent!
	count, err = env.TaskRepo.CountRunnableTasks(ctx)
	if err != nil || count != 1 {
		t.Fatalf("expected 1 runnable task (parent only, child blocked), got %d (err: %v)", count, err)
	}

	// Worker claims parent (now RUNNING)
	claimed, err := env.Engine.ClaimTask(ctx, "worker-1", 10*time.Second)
	if err != nil || claimed == nil {
		t.Fatalf("claim parent task failed: %v", err)
	}

	// Count should STILL be 1 (parent is RUNNING, child is still WAITING_FOR_DEPENDENCY)
	count, err = env.TaskRepo.CountRunnableTasks(ctx)
	if err != nil || count != 1 {
		t.Fatalf("expected 1 runnable task (parent running), got %d (err: %v)", count, err)
	}

	// Worker completes parent task
	err = env.Engine.CompleteTask(ctx, parent.ID, "worker-1", json.RawMessage(`{"result":"done"}`))
	if err != nil {
		t.Fatalf("complete parent task failed: %v", err)
	}

	// Child is now automatically unblocked into QUEUED!
	// Count should be 1 (child queued, parent completed)
	count, err = env.TaskRepo.CountRunnableTasks(ctx)
	if err != nil || count != 1 {
		t.Fatalf("expected 1 runnable task (child unblocked to queued), got %d (err: %v)", count, err)
	}

	// Claim and complete child
	claimedChild, err := env.Engine.ClaimTask(ctx, "worker-1", 10*time.Second)
	if err != nil || claimedChild == nil {
		t.Fatalf("claim child task failed: %v", err)
	}
	err = env.Engine.CompleteTask(ctx, child.ID, "worker-1", json.RawMessage(`{"result":"child done"}`))
	if err != nil {
		t.Fatalf("complete child task failed: %v", err)
	}

	// Both are completed: count should now be 0!
	count, err = env.TaskRepo.CountRunnableTasks(ctx)
	if err != nil || count != 0 {
		t.Fatalf("expected 0 runnable tasks after all completed, got %d (err: %v)", count, err)
	}
}

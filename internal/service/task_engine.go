package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

// orchestratorWorkerID is the synthetic lease holder the JJ orchestrator uses for root tasks.
const orchestratorWorkerID = "JJ_ORCHESTRATOR"

// TaskEngine coordinates task lifecycle, dependencies, execution leases, and approvals.
type TaskEngine struct {
	taskRepo     repository.TaskRepository
	approvalRepo repository.ApprovalRepository
	logger       *slog.Logger
	notifier     FinishNotifier
}

// FinishNotifier is told when a top-level task finishes, so the owner can be told. It must be quick and must
// never fail the task: errors are its own to log.
type FinishNotifier interface {
	TaskFinished(ctx context.Context, t *domain.Task)
}

// SetNotifier turns on finish notifications.
func (e *TaskEngine) SetNotifier(n FinishNotifier) { e.notifier = n }

// notifyFinished tells the notifier about a task that has just reached a final state. Steps of a plan are not
// announced (the plan is), and a task that is being retried is not final.
func (e *TaskEngine) notifyFinished(ctx context.Context, taskID uuid.UUID) {
	if e.notifier == nil {
		return
	}
	t, err := e.taskRepo.GetTaskByID(ctx, taskID)
	if err != nil || t == nil || t.ParentID != nil || !t.IsTerminal() {
		return
	}
	// The request may end the moment its response is sent; give the notification its own short deadline.
	nctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 8*time.Second)
	defer cancel()
	e.notifier.TaskFinished(nctx, t)
}

// NewTaskEngine constructs a new TaskEngine.
func NewTaskEngine(taskRepo repository.TaskRepository, approvalRepo repository.ApprovalRepository, logger *slog.Logger) *TaskEngine {
	if logger == nil {
		logger = slog.Default()
	}
	return &TaskEngine{
		taskRepo:     taskRepo,
		approvalRepo: approvalRepo,
		logger:       logger,
	}
}

// CreateTask registers a new task, links any prerequisite dependencies, and sets its initial state.
func (e *TaskEngine) CreateTask(ctx context.Context, task *domain.Task, dependencyIDs []uuid.UUID) (*domain.Task, error) {
	if err := task.Validate(); err != nil {
		return nil, err
	}

	// 1. Determine initial status based on dependencies if not explicitly pre-set
	if task.Status != domain.TaskStatusRunning && task.Status != domain.TaskStatusAwaitingApproval {
		initialStatus := domain.TaskStatusQueued
		if len(dependencyIDs) > 0 {
			initialStatus = domain.TaskStatusWaitingForDependency
		}
		task.Status = initialStatus
	}

	// 2. Persist task
	created, err := e.taskRepo.CreateTask(ctx, task)
	if err != nil {
		if errors.Is(err, domain.ErrIdempotentConflict) {
			e.logger.Warn("Idempotent task submission detected", "user_id", task.UserID, "key", task.IdempotencyKey)
			return nil, domain.ErrIdempotentConflict
		}
		return nil, fmt.Errorf("task_engine: create task: %w", err)
	}

	// 3. Link dependencies
	for _, depID := range dependencyIDs {
		if err := e.taskRepo.AddDependency(ctx, created.ID, depID); err != nil {
			return nil, fmt.Errorf("task_engine: add dependency: %w", err)
		}
	}

	// If dependencies were specified, check if they are already completed (e.g., historical tasks)
	if len(dependencyIDs) > 0 {
		allDone, err := e.taskRepo.CheckDependenciesCompleted(ctx, created.ID)
		if err == nil && allDone {
			created.Status = domain.TaskStatusQueued
			_ = e.taskRepo.UpdateTask(ctx, created)
		}
	}

	// 4. Record creation event
	meta, _ := json.Marshal(map[string]any{
		"dependencies": dependencyIDs,
		"priority":     created.Priority,
	})
	_ = e.taskRepo.RecordEvent(ctx, &domain.TaskEvent{
		TaskID:    created.ID,
		EventType: "STATUS_CHANGE",
		Message:   fmt.Sprintf("Task created in status %s", created.Status),
		Metadata:  meta,
	})

	return created, nil
}

// GetTask retrieves a task by ID.
func (e *TaskEngine) GetTask(ctx context.Context, taskID uuid.UUID) (*domain.Task, error) {
	return e.taskRepo.GetTaskByID(ctx, taskID)
}

// GetTaskForUser retrieves a task by ID scoped to a specific user.
func (e *TaskEngine) GetTaskForUser(ctx context.Context, taskID uuid.UUID, userID uuid.UUID) (*domain.Task, error) {
	return e.taskRepo.GetTaskByIDAndUser(ctx, taskID, userID)
}

// GetTaskByIdempotencyKey returns the user's live task that holds the idempotency key.
func (e *TaskEngine) GetTaskByIdempotencyKey(ctx context.Context, userID uuid.UUID, key string) (*domain.Task, error) {
	return e.taskRepo.GetTaskByIdempotencyKey(ctx, userID, key)
}

// ListTasks lists tasks matching the filter criteria.
func (e *TaskEngine) ListTasks(ctx context.Context, filter repository.TaskFilter) ([]*domain.Task, error) {
	return e.taskRepo.ListTasks(ctx, filter)
}

// UpdateTask updates an existing task record.
func (e *TaskEngine) UpdateTask(ctx context.Context, task *domain.Task) error {
	return e.taskRepo.UpdateTask(ctx, task)
}

// ClaimTask atomically leases the next available QUEUED task.
func (e *TaskEngine) ClaimTask(ctx context.Context, workerID string, leaseDuration time.Duration) (*domain.Task, error) {
	task, err := e.taskRepo.ClaimNextTask(ctx, workerID, leaseDuration)
	if err != nil {
		return nil, fmt.Errorf("task_engine: claim task: %w", err)
	}
	if task == nil {
		return nil, nil // No task in queue
	}

	meta, _ := json.Marshal(map[string]any{
		"worker_id":        workerID,
		"lease_expires_at": task.LeaseExpiresAt,
	})
	_ = e.taskRepo.RecordEvent(ctx, &domain.TaskEvent{
		TaskID:    task.ID,
		EventType: "STATUS_CHANGE",
		Message:   fmt.Sprintf("Task claimed by worker %s", workerID),
		Metadata:  meta,
	})

	return task, nil
}

// Heartbeat renews an active task lease for a running worker.
func (e *TaskEngine) Heartbeat(ctx context.Context, taskID uuid.UUID, workerID string, leaseDuration time.Duration) error {
	err := e.taskRepo.RenewLease(ctx, taskID, workerID, leaseDuration)
	if err != nil {
		return fmt.Errorf("task_engine: heartbeat: %w", err)
	}
	return nil
}

// CompleteTask marks a task as COMPLETED and unblocks any eligible downstream dependent tasks.
func (e *TaskEngine) CompleteTask(ctx context.Context, taskID uuid.UUID, workerID string, output json.RawMessage) error {
	if workerID != orchestratorWorkerID && domain.IsEmptyOutput(output) {
		return errors.Join(domain.ErrValidation, errors.New("task output is empty: a task cannot be completed without a result"))
	}
	err := e.taskRepo.CompleteTask(ctx, taskID, workerID, output)
	if err != nil {
		return fmt.Errorf("task_engine: complete task: %w", err)
	}

	_ = e.taskRepo.RecordEvent(ctx, &domain.TaskEvent{
		TaskID:    taskID,
		EventType: "STATUS_CHANGE",
		Message:   "Task execution completed successfully",
		Metadata:  json.RawMessage("{}"),
	})

	// Unblock dependent tasks
	blockedChildIDs, err := e.taskRepo.GetDependentTasks(ctx, taskID)
	if err == nil {
		for _, childID := range blockedChildIDs {
			unblocked, err := e.taskRepo.UnblockTaskIfEligible(ctx, childID)
			if err == nil && unblocked {
				e.logger.Info("Unblocked dependent task", "child_task_id", childID, "parent_task_id", taskID)
				_ = e.taskRepo.RecordEvent(ctx, &domain.TaskEvent{
					TaskID:    childID,
					EventType: "STATUS_CHANGE",
					Message:   "Dependencies fulfilled; task transitioned to QUEUED",
					Metadata:  json.RawMessage("{}"),
				})
			}
		}
	}

	e.notifyFinished(ctx, taskID)
	return nil
}

// FailTask marks a task execution as failed, scheduling a retry with exponential backoff if retries remain.
func (e *TaskEngine) FailTask(ctx context.Context, taskID uuid.UUID, workerID string, reason string, retryable bool) error {
	task, err := e.taskRepo.GetTaskByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("task_engine: fail task lookup: %w", err)
	}

	// Calculate exponential backoff with jitter
	backoff := calculateBackoff(task.RetryCount)

	err = e.taskRepo.FailTask(ctx, taskID, workerID, reason, retryable, backoff)
	if err != nil {
		return fmt.Errorf("task_engine: fail task update: %w", err)
	}

	permanent := false
	eventType := "RETRY"
	msg := fmt.Sprintf("Task failed (retryable): %s. Scheduled retry in %s", reason, backoff)
	if !retryable || task.RetryCount+1 >= task.MaxRetries {
		permanent = true
		eventType = "STATUS_CHANGE"
		msg = fmt.Sprintf("Task failed permanently: %s", reason)

		// Cascade cancellation to dependent children
		e.cascadeCancelDownstream(ctx, taskID, fmt.Sprintf("Prerequisite task %s failed", taskID))
	}

	_ = e.taskRepo.RecordEvent(ctx, &domain.TaskEvent{
		TaskID:    taskID,
		EventType: eventType,
		Message:   msg,
		Metadata:  json.RawMessage("{}"),
	})

	if permanent {
		e.notifyFinished(ctx, taskID)
	}
	return nil
}

// CancelTask aborts an active or pending task and cascades cancellation to all blocked dependent children.
func (e *TaskEngine) CancelTask(ctx context.Context, taskID uuid.UUID, reason string) error {
	err := e.taskRepo.CancelTask(ctx, taskID, reason)
	if err != nil {
		return fmt.Errorf("task_engine: cancel task: %w", err)
	}

	_ = e.taskRepo.RecordEvent(ctx, &domain.TaskEvent{
		TaskID:    taskID,
		EventType: "STATUS_CHANGE",
		Message:   fmt.Sprintf("Task cancelled: %s", reason),
		Metadata:  json.RawMessage("{}"),
	})

	// Cascade cancellation to dependent children
	e.cascadeCancelDownstream(ctx, taskID, fmt.Sprintf("Prerequisite task %s was cancelled", taskID))

	// Cancelling a parent (e.g. the JJ orchestration root) must not leave its children runnable.
	e.cascadeCancelChildren(ctx, taskID, fmt.Sprintf("Parent task %s was cancelled", taskID))

	return nil
}

// ResumeTask re-queues a failed or cancelled standalone task and records the event.
func (e *TaskEngine) ResumeTask(ctx context.Context, taskID uuid.UUID) error {
	if err := e.taskRepo.ResumeTask(ctx, taskID); err != nil {
		return fmt.Errorf("task_engine: resume task: %w", err)
	}
	_ = e.taskRepo.RecordEvent(ctx, &domain.TaskEvent{
		TaskID:    taskID,
		EventType: "STATUS_CHANGE",
		Message:   "Task resumed by user",
		Metadata:  json.RawMessage("{}"),
	})
	return nil
}

// DeleteTask permanently removes a finished task and its subtasks.
func (e *TaskEngine) DeleteTask(ctx context.Context, taskID uuid.UUID) error {
	if err := e.taskRepo.DeleteTaskTree(ctx, taskID); err != nil {
		return fmt.Errorf("task_engine: delete task: %w", err)
	}
	return nil
}

// cascadeCancelChildren cancels every non-terminal child (parent_id link) of a cancelled task.
func (e *TaskEngine) cascadeCancelChildren(ctx context.Context, parentID uuid.UUID, reason string) {
	children, err := e.taskRepo.ListTasks(ctx, repository.TaskFilter{ParentID: &parentID, Limit: 1000})
	if err != nil {
		e.logger.Warn("task_engine: list children for cancel cascade failed", "parent_id", parentID, "error", err)
		return
	}
	for _, child := range children {
		if err := e.taskRepo.CancelTask(ctx, child.ID, reason); err != nil {
			continue // already terminal
		}
		_ = e.taskRepo.RecordEvent(ctx, &domain.TaskEvent{
			TaskID:    child.ID,
			EventType: "STATUS_CHANGE",
			Message:   fmt.Sprintf("Cascaded cancellation: %s", reason),
			Metadata:  json.RawMessage("{}"),
		})
		e.cascadeCancelDownstream(ctx, child.ID, reason)
		e.cascadeCancelChildren(ctx, child.ID, reason)
	}
}

// RequestApproval pauses task execution and registers an approval gate requiring human sign-off.
func (e *TaskEngine) RequestApproval(ctx context.Context, taskID uuid.UUID, approval *domain.Approval) error {
	return e.taskRepo.TransitionToApproval(ctx, taskID, approval)
}

// HasApprovedAction reports whether the task's owner has approved the given action type for this task.
// It is how a task that wants to act on the user's behalf proves it was allowed to.
func (e *TaskEngine) HasApprovedAction(ctx context.Context, task *domain.Task, actionType string) (bool, error) {
	st := domain.ApprovalStatusApproved
	list, err := e.approvalRepo.ListApprovals(ctx, task.UserID, &st)
	if err != nil {
		return false, fmt.Errorf("task_engine: list approvals: %w", err)
	}
	for _, a := range list {
		if a.TaskID == task.ID && a.ActionType == actionType {
			return true, nil
		}
	}
	return false, nil
}

// ApprovedActionTypes lists the action types the owner has approved for this task.
func (e *TaskEngine) ApprovedActionTypes(ctx context.Context, task *domain.Task) ([]string, error) {
	st := domain.ApprovalStatusApproved
	list, err := e.approvalRepo.ListApprovals(ctx, task.UserID, &st)
	if err != nil {
		return nil, fmt.Errorf("task_engine: list approvals: %w", err)
	}
	var out []string
	for _, a := range list {
		if a.TaskID == task.ID {
			out = append(out, a.ActionType)
		}
	}
	return out, nil
}

// RespondApproval applies a human decision to an approval, resuming or cancelling the underlying task.
func (e *TaskEngine) RespondApproval(ctx context.Context, approvalID uuid.UUID, userID uuid.UUID, approved bool, reason string) (*domain.Approval, error) {
	app, err := e.approvalRepo.RespondToApproval(ctx, approvalID, userID, approved, reason)
	if err != nil {
		return nil, fmt.Errorf("task_engine: respond approval: %w", err)
	}

	eventType := "STATUS_CHANGE"
	msg := fmt.Sprintf("Approval granted by user: %s", reason)
	if !approved {
		msg = fmt.Sprintf("Approval rejected by user: %s", reason)
	}

	_ = e.taskRepo.RecordEvent(ctx, &domain.TaskEvent{
		TaskID:    app.TaskID,
		EventType: eventType,
		Message:   msg,
		Metadata:  json.RawMessage("{}"),
	})

	return app, nil
}

// EditApproval lets the owner change the message text of a pending approval before deciding on it.
func (e *TaskEngine) EditApproval(ctx context.Context, approvalID, userID uuid.UUID, text string) (*domain.Approval, error) {
	app, err := e.approvalRepo.EditPendingApproval(ctx, approvalID, userID, text)
	if err != nil {
		return nil, fmt.Errorf("task_engine: edit approval: %w", err)
	}
	_ = e.taskRepo.RecordEvent(ctx, &domain.TaskEvent{
		TaskID: app.TaskID, EventType: "STATUS_CHANGE", Message: "Owner edited the draft before approving", Metadata: json.RawMessage("{}"),
	})
	return app, nil
}

// ReapExpiredLeases reclaims tasks abandoned by crashed workers.
func (e *TaskEngine) ReapExpiredLeases(ctx context.Context) (int64, error) {
	reaped, err := e.taskRepo.ReapExpiredLeases(ctx)
	if err != nil {
		return 0, fmt.Errorf("task_engine: reap leases: %w", err)
	}
	if reaped > 0 {
		e.logger.Warn("Reaped expired task leases", "count", reaped)
	}
	return reaped, nil
}

func (e *TaskEngine) cascadeCancelDownstream(ctx context.Context, taskID uuid.UUID, reason string) {
	children, err := e.taskRepo.GetDependentTasks(ctx, taskID)
	if err != nil {
		return
	}
	for _, childID := range children {
		_ = e.taskRepo.CancelTask(ctx, childID, reason)
		_ = e.taskRepo.RecordEvent(ctx, &domain.TaskEvent{
			TaskID:    childID,
			EventType: "STATUS_CHANGE",
			Message:   fmt.Sprintf("Cascaded cancellation: %s", reason),
			Metadata:  json.RawMessage("{}"),
		})
		// Recurse to cancel grandchildren
		e.cascadeCancelDownstream(ctx, childID, reason)
	}
}

func calculateBackoff(retryCount int) time.Duration {
	base := 5.0     // 5 seconds
	maxSec := 600.0 // 10 minutes

	mult := math.Pow(2, float64(retryCount))
	sec := base * mult
	if sec > maxSec {
		sec = maxSec
	}

	// Add 10-20% jitter
	jitter := sec * (0.1 + rand.Float64()*0.1)
	return time.Duration(sec+jitter) * time.Second
}

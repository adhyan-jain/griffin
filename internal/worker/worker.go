package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/service"
)

// Worker polls the task engine, leases tasks, maintains heartbeats, and drives execution.
type Worker struct {
	ID                string
	engine            *service.TaskEngine
	taskRepo          repository.TaskRepository
	executor          TaskExecutor
	leaseDuration     time.Duration
	heartbeatInterval time.Duration
	pollInterval      time.Duration
	logger            *slog.Logger

	paused   atomic.Bool
	inFlight atomic.Int32
}

// Config holds settings for constructing a Worker.
type Config struct {
	ID                string
	Engine            *service.TaskEngine
	TaskRepo          repository.TaskRepository
	Executor          TaskExecutor
	LeaseDuration     time.Duration
	HeartbeatInterval time.Duration
	PollInterval      time.Duration
	Logger            *slog.Logger
}

// NewWorker initializes a Worker.
func NewWorker(cfg Config) *Worker {
	if cfg.ID == "" {
		cfg.ID = fmt.Sprintf("worker-%s", uuid.New().String()[:8])
	}
	if cfg.LeaseDuration <= 0 {
		cfg.LeaseDuration = 60 * time.Second
	}
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = 15 * time.Second
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 1 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	return &Worker{
		ID:                cfg.ID,
		engine:            cfg.Engine,
		taskRepo:          cfg.TaskRepo,
		executor:          cfg.Executor,
		leaseDuration:     cfg.LeaseDuration,
		heartbeatInterval: cfg.HeartbeatInterval,
		pollInterval:      cfg.PollInterval,
		logger:            cfg.Logger,
	}
}

// Pause halts new task claiming by this worker.
func (w *Worker) Pause() {
	w.paused.Store(true)
}

// Resume allows this worker to resume claiming tasks.
func (w *Worker) Resume() {
	w.paused.Store(false)
}

// InFlight returns the number of tasks currently being executed by this worker.
func (w *Worker) InFlight() int {
	return int(w.inFlight.Load())
}

// ProcessNext attempts to claim and execute one task. Returns true if a task was claimed and processed.
func (w *Worker) ProcessNext(ctx context.Context) (bool, error) {
	if w.paused.Load() {
		return false, nil
	}

	task, err := w.engine.ClaimTask(ctx, w.ID, w.leaseDuration)
	if err != nil {
		return false, fmt.Errorf("claim task: %w", err)
	}
	if task == nil {
		return false, nil // Queue was empty
	}

	w.inFlight.Add(1)
	defer w.inFlight.Add(-1)

	w.logger.Info("Worker claimed task", "worker_id", w.ID, "task_id", task.ID, "title", task.Title)

	// Context for task execution with cancellation tied to heartbeat failure
	taskCtx, cancelTask := context.WithCancel(ctx)
	defer cancelTask()

	// Start background lease renewal
	heartbeatDone := make(chan struct{})
	defer close(heartbeatDone)

	go func() {
		ticker := time.NewTicker(w.heartbeatInterval)
		defer ticker.Stop()

		for {
			select {
			case <-heartbeatDone:
				return
			case <-ticker.C:
				if err := w.engine.Heartbeat(ctx, task.ID, w.ID, w.leaseDuration); err != nil {
					w.logger.Error("Heartbeat failed; cancelling task execution", "worker_id", w.ID, "task_id", task.ID, "error", err)
					cancelTask()
					return
				}
				w.logger.Debug("Heartbeat renewed lease", "worker_id", w.ID, "task_id", task.ID)
			}
		}
	}()

	start := time.Now().UTC()
	attempt := &domain.TaskAttempt{
		ID:            uuid.New(),
		TaskID:        task.ID,
		WorkerID:      w.ID,
		AttemptNumber: task.RetryCount + 1,
		Status:        "RUNNING",
		StartedAt:     start,
	}
	if w.taskRepo != nil {
		_ = w.taskRepo.RecordAttempt(ctx, attempt)
	}

	res, execErr := w.executor.Execute(taskCtx, task)
	durationMs := int(time.Since(start).Milliseconds())

	if execErr != nil || (res != nil && res.Error != nil) {
		var failErr error = execErr
		retryable := false
		if res != nil {
			if res.Error != nil {
				failErr = res.Error
			}
			retryable = res.Retryable
		}

		errMsg := failErr.Error()
		isAuthErr := errors.Is(failErr, domain.ErrAuthenticationRequired) || strings.Contains(strings.ToLower(errMsg), "authentication required")
		if isAuthErr {
			retryable = false
			w.logger.Error("CRITICAL: Antigravity authentication required; task execution halted without retrying. Operator intervention required (run `agy auth login`)",
				"worker_id", w.ID,
				"task_id", task.ID,
			)
		} else {
			w.logger.Warn("Task execution failed", "worker_id", w.ID, "task_id", task.ID, "error", errMsg)
		}

		if err := w.engine.FailTask(ctx, task.ID, w.ID, errMsg, retryable); err != nil {
			w.logger.Error("engine.FailTask failed", "worker_id", w.ID, "task_id", task.ID, "error", err)
		}

		if isAuthErr && w.taskRepo != nil {
			_ = w.taskRepo.RecordEvent(ctx, &domain.TaskEvent{
				TaskID:    task.ID,
				EventType: "AUTHENTICATION_REQUIRED",
				Message:   "Antigravity authentication expired or required. Execution halted without retry.",
				Metadata:  json.RawMessage(`{"provider":"antigravity","error":"ErrAuthenticationRequired"}`),
			})
		}

		attempt.Status = "FAILED"
		attempt.DurationMs = durationMs
		attempt.ErrorMessage = &errMsg
		now := time.Now().UTC()
		attempt.CompletedAt = &now
		if w.taskRepo != nil {
			_ = w.taskRepo.RecordAttempt(ctx, attempt)
		}
		return true, nil
	}

	if res.RequiresApproval != nil {
		w.logger.Info("Task execution paused for approval", "worker_id", w.ID, "task_id", task.ID)
		_ = w.engine.RequestApproval(ctx, task.ID, res.RequiresApproval)
		return true, nil
	}

	w.logger.Info("Task execution completed successfully", "worker_id", w.ID, "task_id", task.ID)
	if err := w.engine.CompleteTask(ctx, task.ID, w.ID, res.Output); err != nil {
		w.logger.Error("engine.CompleteTask failed", "worker_id", w.ID, "task_id", task.ID, "error", err)
	}

	attempt.Status = "COMPLETED"
	attempt.DurationMs = durationMs
	attempt.PromptTokens = res.PromptTokens
	attempt.CompletionTokens = res.CompletionTokens
	now := time.Now().UTC()
	attempt.CompletedAt = &now
	if w.taskRepo != nil {
		_ = w.taskRepo.RecordAttempt(ctx, attempt)
	}

	return true, nil
}

// Run executes the worker loop until ctx is cancelled.
func (w *Worker) Run(ctx context.Context) error {
	w.logger.Info("Starting worker loop", "worker_id", w.ID)
	for {
		select {
		case <-ctx.Done():
			w.logger.Info("Worker shutting down", "worker_id", w.ID)
			return ctx.Err()
		default:
			processed, err := w.ProcessNext(ctx)
			if err != nil {
				w.logger.Error("Error processing task", "worker_id", w.ID, "error", err)
			}
			if !processed {
				// Queue empty, sleep for poll interval
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(w.pollInterval):
				}
			}
		}
	}
}

// WorkerPool groups multiple workers and satisfies the runner.WorkerDrainer interface.
type WorkerPool struct {
	workers []*Worker
}

// NewWorkerPool wraps multiple workers.
func NewWorkerPool(workers ...*Worker) *WorkerPool {
	return &WorkerPool{workers: workers}
}

// Add appends a worker to the pool.
func (p *WorkerPool) Add(w *Worker) {
	p.workers = append(p.workers, w)
}

// Pause pauses all workers in the pool.
func (p *WorkerPool) Pause() {
	for _, w := range p.workers {
		w.Pause()
	}
}

// Resume resumes all workers in the pool.
func (p *WorkerPool) Resume() {
	for _, w := range p.workers {
		w.Resume()
	}
}

// ActiveCount sums in-flight tasks across all workers.
func (p *WorkerPool) ActiveCount() int {
	total := 0
	for _, w := range p.workers {
		total += w.InFlight()
	}
	return total
}

// Workers returns the underlying worker slice.
func (p *WorkerPool) Workers() []*Worker {
	return p.workers
}

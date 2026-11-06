package worker

import (
	"bytes"
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
	"github.com/griffin/core/internal/storage"
)

// ErrEmptyOutput marks an execution that reported success but produced no result.
var ErrEmptyOutput = errors.New("execution reported success but produced no result")

// ToolCapable is implemented by executors that can run the read and act tiers. The worker only asks for
// an act approval when it is true, so with tools off behaviour is unchanged.
type ToolCapable interface {
	ToolsEnabled() bool
}

// ArtifactSaver stores a file and records it against a task. *storage.StorageService implements it.
type ArtifactSaver interface {
	SaveArtifact(ctx context.Context, input storage.SaveArtifactInput) (*domain.Artifact, error)
}

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
	onTaskFinished    func(ctx context.Context, taskID uuid.UUID)
	artifacts         ArtifactSaver

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

	// OnTaskFinished, if set, is called after this worker wrote a task's final state (completed,
	// or failed; retryable failures included, so the hook must tolerate non-terminal states).
	OnTaskFinished func(ctx context.Context, taskID uuid.UUID)

	// Artifacts, if set, keeps every finished task's answer as a file (Files page) so results outlive
	// scrolling back through tasks. A failure to save is logged and never fails the task.
	Artifacts ArtifactSaver
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

	if cfg.TaskRepo == nil {
		cfg.Logger.Warn("Worker constructed with no TaskRepo: attempts, events and dependency outputs will not be recorded or loaded", "worker_id", cfg.ID)
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
		onTaskFinished:    cfg.OnTaskFinished,
		artifacts:         cfg.Artifacts,
	}
}

// notifyFinished invokes the optional OnTaskFinished hook, never letting it break the worker loop.
func (w *Worker) notifyFinished(ctx context.Context, taskID uuid.UUID) {
	if w.onTaskFinished == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			w.logger.Error("OnTaskFinished hook panicked", "worker_id", w.ID, "task_id", taskID, "panic", r)
		}
	}()
	w.onTaskFinished(ctx, taskID)
}

// recordAttempt persists an attempt record; a failure is logged, never fatal to the run.
func (w *Worker) recordAttempt(ctx context.Context, attempt *domain.TaskAttempt) {
	if w.taskRepo == nil {
		return
	}
	if err := w.taskRepo.RecordAttempt(ctx, attempt); err != nil {
		w.logger.Error("RecordAttempt failed", "worker_id", w.ID, "task_id", attempt.TaskID, "attempt_status", attempt.Status, "error", err)
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
		w.recordAttempt(ctx, attempt)
	}

	// Give a dependent task the results of its prerequisites, for this run only (nothing is persisted).
	execTask := task
	if w.taskRepo != nil {
		deps, depErr := w.taskRepo.GetDependencyOutputs(ctx, task.ID)
		if depErr != nil {
			w.logger.Warn("Could not load dependency outputs; running with the task goal only", "worker_id", w.ID, "task_id", task.ID, "error", depErr)
		} else {
			execTask = withDependencyOutputs(task, deps, 0)
		}
	}

	// The act tier never runs without the owner's approval for this exact task: ask first and stop.
	if paused := w.gateActTier(ctx, task); paused {
		attempt.Status = "AWAITING_APPROVAL"
		attempt.DurationMs = int(time.Since(start).Milliseconds())
		now := time.Now().UTC()
		attempt.CompletedAt = &now
		if w.taskRepo != nil {
			w.recordAttempt(ctx, attempt)
		}
		return true, nil
	}

	res, execErr := w.executor.Execute(taskCtx, execTask)
	durationMs := int(time.Since(start).Milliseconds())

	// Provider-agnostic guard: a "successful" run with no result must never become COMPLETED.
	if execErr == nil && (res == nil || (res.Error == nil && res.RequiresApproval == nil && domain.IsEmptyOutput(res.Output))) {
		if res == nil {
			res = &ExecutionResult{}
		}
		res.Error = ErrEmptyOutput
		res.Retryable = true
	}

	// Chat tasks must yield non-blank assistant text; otherwise treat as a retryable failure.
	var chatText string
	chat := isChatTask(task)
	if chat && execErr == nil && res != nil && res.Error == nil && res.RequiresApproval == nil {
		if chatText = chatResponseText(res); strings.TrimSpace(chatText) == "" {
			res = &ExecutionResult{ExecutionID: res.ExecutionID, ProviderName: res.ProviderName, Status: "FAILED", Error: ErrEmptyChatResponse, Retryable: true}
		}
	}

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
		} else {
			w.notifyFinished(ctx, task.ID)
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
			w.recordAttempt(ctx, attempt)
		}
		return true, nil
	}

	if res.RequiresApproval != nil {
		w.logger.Info("Task execution paused for approval", "worker_id", w.ID, "task_id", task.ID)
		_ = w.engine.RequestApproval(ctx, task.ID, res.RequiresApproval)
		return true, nil
	}

	w.logger.Info("Task execution completed successfully", "worker_id", w.ID, "task_id", task.ID)
	output := res.Output
	if chat {
		var facts RuntimeFacts
		if rp, ok := w.executor.(RuntimeFactsProvider); ok {
			facts = rp.RuntimeFacts()
		}
		output = buildChatOutput(chatText, w.executor.Name(), facts)
	}
	if err := w.engine.CompleteTask(ctx, task.ID, w.ID, output); err != nil {
		w.logger.Error("engine.CompleteTask failed", "worker_id", w.ID, "task_id", task.ID, "error", err)
	} else {
		if !chat {
			w.saveResultFile(ctx, task, res)
		}
		w.notifyFinished(ctx, task.ID)
	}

	attempt.Status = "COMPLETED"
	attempt.DurationMs = durationMs
	attempt.PromptTokens = res.PromptTokens
	attempt.CompletionTokens = res.CompletionTokens
	now := time.Now().UTC()
	attempt.CompletedAt = &now
	if w.taskRepo != nil {
		w.recordAttempt(ctx, attempt)
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

// resultFile picks what to keep: the answer text as Markdown when there is one, otherwise the raw
// output as JSON. ok is false when the run produced nothing worth saving.
func resultFile(res *ExecutionResult) (body []byte, ext, artifactType, contentType string, ok bool) {
	text := strings.TrimSpace(res.TextOutput)
	if text == "" {
		var o map[string]any
		if json.Unmarshal(res.Output, &o) == nil {
			if s, isStr := o["response"].(string); isStr {
				text = strings.TrimSpace(s)
			}
		}
	}
	if text != "" {
		return []byte(text + "\n"), "md", "MARKDOWN", "text/markdown; charset=utf-8", true
	}
	if len(res.Output) > 0 {
		var pretty any
		if json.Unmarshal(res.Output, &pretty) == nil {
			if b, err := json.MarshalIndent(pretty, "", "  "); err == nil {
				return append(b, '\n'), "json", "JSON", "application/json", true
			}
		}
	}
	return nil, "", "", "", false
}

// fileSlug makes a short, filename-safe name from a task title.
func fileSlug(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
		if b.Len() >= 60 {
			break
		}
	}
	if out := strings.Trim(b.String(), "-"); out != "" {
		return out
	}
	return "result"
}

// saveResultFile keeps the finished task's answer in the Files area. Best effort by design.
func (w *Worker) saveResultFile(ctx context.Context, task *domain.Task, res *ExecutionResult) {
	if w.artifacts == nil {
		return
	}
	body, ext, artifactType, contentType, ok := resultFile(res)
	if !ok {
		return
	}
	title := strings.TrimSpace(task.Title)
	if title == "" {
		title = "Result"
	}
	meta, _ := json.Marshal(map[string]string{"source": "task_result", "provider": res.ProviderName})
	taskID := task.ID
	if _, err := w.artifacts.SaveArtifact(ctx, storage.SaveArtifactInput{
		UserID:       task.UserID,
		TaskID:       &taskID,
		Title:        title,
		Filename:     fileSlug(title) + "." + ext,
		ArtifactType: artifactType,
		ContentType:  contentType,
		Body:         bytes.NewReader(body),
		Metadata:     meta,
	}); err != nil {
		w.logger.Warn("Could not save the task result as a file", "task_id", task.ID, "error", err)
	}
}

// gateActTier pauses a task that asked to act on the user's behalf until they approve it. It returns
// true when the task was paused (or could not be cleared), meaning the executor must not run. When the
// approval check itself fails the task is not run either: the safe default is to do nothing.
func (w *Worker) gateActTier(ctx context.Context, task *domain.Task) bool {
	if domain.ToolModeFor(task) != domain.ToolAct {
		return false
	}
	tc, ok := w.executor.(ToolCapable)
	if !ok || !tc.ToolsEnabled() {
		return false // tools are off: the task runs text-only, so there is nothing to approve
	}
	approved, err := w.engine.HasApprovedAction(ctx, task, domain.ToolApprovalAction)
	if err != nil {
		w.logger.Error("Could not check the act approval; not running the task", "worker_id", w.ID, "task_id", task.ID, "error", err)
		return true
	}
	if approved {
		return false
	}
	payload, _ := json.Marshal(map[string]string{"task_title": task.Title, "goal": task.Goal, "tool_mode": string(domain.ToolAct)})
	summary := "Allow Griffin to act for you on: " + strings.TrimSpace(task.Title)
	if err := w.engine.RequestApproval(ctx, task.ID, &domain.Approval{
		ID:            uuid.New(),
		UserID:        task.UserID,
		TaskID:        task.ID,
		ActionType:    domain.ToolApprovalAction,
		RiskLevel:     domain.ApprovalRiskHigh,
		Summary:       summary,
		ActionPayload: payload,
	}); err != nil {
		w.logger.Error("Could not request approval for the act tier", "worker_id", w.ID, "task_id", task.ID, "error", err)
	} else {
		w.logger.Info("Task wants to act on the user's behalf; paused for approval", "worker_id", w.ID, "task_id", task.ID)
	}
	return true
}

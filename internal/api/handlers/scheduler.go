package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/scheduler"
	"github.com/griffin/core/internal/service"
)

// SchedulerHandler handles periodic scheduler tick invocations (e.g. from AWS EventBridge Scheduler).
type SchedulerHandler struct {
	engine       *service.TaskEngine
	taskRepo     repository.TaskRepository
	scheduleRepo repository.ScheduleRepository
	wakeup       RunnerWakeup
	logger       *slog.Logger
	proactive    []ProactiveOp
}

// ProactiveOp is a durable, idempotent operation (brief, sync, sweep...) the tick runs in API mode,
// so proactive work happens from the external cadence even while the worker host is stopped.
// Each op owns its own idempotency (see repository.OperationRepository); the tick only invokes it.
type ProactiveOp struct {
	Name    string
	Timeout time.Duration // zero means 20s
	Run     func(ctx context.Context) error
}

// WithProactive returns a copy of the handler that runs ops on every tick.
func (h *SchedulerHandler) WithProactive(ops ...ProactiveOp) *SchedulerHandler {
	cp := *h
	cp.proactive = append(append([]ProactiveOp(nil), h.proactive...), ops...)
	return &cp
}

// NewSchedulerHandler creates a SchedulerHandler.
func NewSchedulerHandler(
	engine *service.TaskEngine,
	taskRepo repository.TaskRepository,
	scheduleRepo repository.ScheduleRepository,
	wakeup RunnerWakeup,
	logger *slog.Logger,
) *SchedulerHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &SchedulerHandler{
		engine:       engine,
		taskRepo:     taskRepo,
		scheduleRepo: scheduleRepo,
		wakeup:       wakeup,
		logger:       logger,
	}
}

// WithScheduleRepo returns a copy of SchedulerHandler with the given ScheduleRepository.
func (h *SchedulerHandler) WithScheduleRepo(repo repository.ScheduleRepository) *SchedulerHandler {
	cp := *h
	cp.scheduleRepo = repo
	return &cp
}

// TickResponse captures the result of a scheduled tick evaluation.
type TickResponse struct {
	EvaluatedAt      time.Time `json:"evaluated_at"`
	RunnableTasks    int       `json:"runnable_tasks"`
	MaterializedJobs int       `json:"materialized_jobs"`
	WakeupTriggered  bool      `json:"wakeup_triggered"`
	// Operations maps each proactive operation to "ok" or its error, so a failing sync is visible
	// to the external scheduler's logs without failing the whole tick.
	Operations map[string]string `json:"operations,omitempty"`
}

// Tick evaluates due recurring schedules, materializes tasks, and triggers wake-up if work is ready.
func (h *SchedulerHandler) Tick(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := time.Now().UTC()

	materializedCount := 0

	// 1. Materialize due recurring schedules from the schedules table (if configured)
	if h.scheduleRepo != nil && h.engine != nil {
		dueSchedules, err := h.scheduleRepo.ClaimDueSchedules(ctx, now, 50)
		if err != nil {
			h.logger.Error("Failed to claim due schedules", "error", err)
			httputil.ErrorJSON(w, err)
			return
		}

		for _, s := range dueSchedules {
			if s.IsPaused {
				continue
			}

			// Generate next occurrence strictly after now
			nextRun, err := scheduler.NextOccurrence(s.CronExpression, now)
			if err != nil {
				h.logger.Error("Invalid cron expression on schedule; pausing schedule", "schedule_id", s.ID, "cron", s.CronExpression, "error", err)
				_ = h.scheduleRepo.SetPaused(ctx, s.ID, s.UserID, true)
				continue
			}

			// Materialize task spec from template
			var taskSpec struct {
				Title        string          `json:"title"`
				Goal         string          `json:"goal"`
				Priority     string          `json:"priority"`
				InputPayload json.RawMessage `json:"input_payload"`
			}
			if len(s.TaskSpecTemplate) > 0 {
				_ = json.Unmarshal(s.TaskSpecTemplate, &taskSpec)
			}
			if taskSpec.Title == "" {
				taskSpec.Title = s.Name
			}
			if taskSpec.Goal == "" {
				taskSpec.Goal = fmt.Sprintf("Recurring execution of schedule %q", s.Name)
			}

			// Merge schedule metadata into input_payload
			payloadMap := make(map[string]any)
			if len(taskSpec.InputPayload) > 0 {
				_ = json.Unmarshal(taskSpec.InputPayload, &payloadMap)
			}
			payloadMap["schedule_id"] = s.ID.String()
			payloadMap["scheduled_cadence"] = s.CronExpression
			payloadMap["scheduled_for"] = s.NextRunAt.Format(time.RFC3339)
			mergedPayload, _ := json.Marshal(payloadMap)

			// Idempotency key per schedule occurrence prevents duplicate runs
			idemKey := fmt.Sprintf("sched:%s:%d", s.ID.String(), s.NextRunAt.Unix())

			prio := domain.TaskPriorityNormalP2
			if taskSpec.Priority != "" {
				prio = domain.TaskPriority(taskSpec.Priority)
			}

			task := &domain.Task{
				UserID:         s.UserID,
				ManagerDomain:  s.ManagerDomain,
				Title:          taskSpec.Title,
				Goal:           taskSpec.Goal,
				Priority:       prio,
				InputPayload:   mergedPayload,
				IdempotencyKey: &idemKey,
				ScheduledFor:   &s.NextRunAt,
			}

			created, createErr := h.engine.CreateTask(ctx, task, nil)
			if createErr != nil && !errors.Is(createErr, domain.ErrIdempotentConflict) {
				h.logger.Error("Failed to materialize task for schedule", "schedule_id", s.ID, "error", createErr)
				continue
			}

			// Advance schedule's next_run_at and record last_run_at
			if advErr := h.scheduleRepo.AdvanceNextRun(ctx, s.ID, now, nextRun); advErr != nil {
				h.logger.Error("Failed to advance next run for schedule", "schedule_id", s.ID, "error", advErr)
			}

			if created != nil {
				materializedCount++
				h.logger.Info("Materialized recurring schedule run", "schedule_id", s.ID, "task_id", created.ID, "next_run_at", nextRun)
			}
		}
	}

	// 1b. Run durable proactive operations. A failure in one never blocks the others or the wake-up.
	var opResults map[string]string
	if len(h.proactive) > 0 {
		opResults = make(map[string]string, len(h.proactive))
		for _, op := range h.proactive {
			timeout := op.Timeout
			if timeout <= 0 {
				timeout = 20 * time.Second
			}
			opCtx, cancel := context.WithTimeout(ctx, timeout)
			err := func() (err error) {
				defer func() {
					if rec := recover(); rec != nil {
						err = fmt.Errorf("panic: %v", rec)
					}
				}()
				return op.Run(opCtx)
			}()
			cancel()
			if err != nil {
				h.logger.Error("Proactive operation failed", "op", op.Name, "error", err)
				opResults[op.Name] = err.Error()
			} else {
				opResults[op.Name] = "ok"
			}
		}
	}

	// 2. Inspect runnable tasks in the queue
	count, err := h.taskRepo.CountRunnableTasks(ctx)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	// 3. If runnable work exists and wakeup controller is configured, signal wake-up
	wakeupTriggered := false
	if count > 0 && h.wakeup != nil {
		if err := h.wakeup.TriggerWakeup(ctx); err != nil {
			// Surface failure so EventBridge/alarms see it; the next tick retries.
			httputil.ErrorJSON(w, err)
			return
		}
		wakeupTriggered = true
	}

	httputil.JSON(w, http.StatusOK, TickResponse{
		EvaluatedAt:      now,
		RunnableTasks:    int(count),
		MaterializedJobs: materializedCount,
		WakeupTriggered:  wakeupTriggered,
		Operations:       opResults,
	})
}

// EventBridgeOnly lets through only an EventBridge scheduled event that the Lambda Web Adapter passed straight to
// the app (POST /events). A request that came through API Gateway carries the adapter's x-amzn-request-context
// header, which a caller cannot remove, so the public API can never reach this route.
func EventBridgeOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-amzn-request-context") != "" {
			http.NotFound(w, r)
			return
		}
		var ev struct {
			Source     string `json:"source"`
			DetailType string `json:"detail-type"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&ev); err != nil ||
			ev.Source != "aws.events" || ev.DetailType != "Scheduled Event" {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

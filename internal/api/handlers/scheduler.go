package handlers

import (
	"net/http"
	"time"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/service"
)

// SchedulerHandler handles periodic scheduler tick invocations (e.g. from AWS EventBridge Scheduler).
type SchedulerHandler struct {
	engine   *service.TaskEngine
	taskRepo repository.TaskRepository
	wakeup   RunnerWakeup
}

// NewSchedulerHandler creates a SchedulerHandler.
func NewSchedulerHandler(engine *service.TaskEngine, taskRepo repository.TaskRepository, wakeup RunnerWakeup) *SchedulerHandler {
	return &SchedulerHandler{
		engine:   engine,
		taskRepo: taskRepo,
		wakeup:   wakeup,
	}
}

// TickResponse captures the result of a scheduled tick evaluation.
type TickResponse struct {
	EvaluatedAt     time.Time `json:"evaluated_at"`
	RunnableTasks   int       `json:"runnable_tasks"`
	WakeupTriggered bool      `json:"wakeup_triggered"`
}

// Tick evaluates due recurring schedules and triggers wake-up if work is ready.
func (h *SchedulerHandler) Tick(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// 1. Inspect runnable tasks in the queue
	count, err := h.taskRepo.CountRunnableTasks(ctx)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	// 2. If runnable work exists and wakeup controller is configured, signal wake-up
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
		EvaluatedAt:     time.Now().UTC(),
		RunnableTasks:   int(count),
		WakeupTriggered: wakeupTriggered,
	})
}

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/service"
)

// badRequestFromDecode turns a JSON decode failure into a validation error a client can act on.
// The common mistake is a wrongly typed priority (a number instead of the enum string), which
// otherwise surfaces as a Go-internal "cannot unmarshal number into Go struct field" message.
func badRequestFromDecode(err error) error {
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		if strings.HasSuffix(typeErr.Field, "priority") {
			return fmt.Errorf("%w: priority must be a string, one of %s", domain.ErrValidation, domain.PriorityList())
		}
		return fmt.Errorf("%w: field %q has the wrong type (expected %s)", domain.ErrValidation, typeErr.Field, typeErr.Type)
	}
	return errors.Join(domain.ErrValidation, err)
}

// RunnerWakeup defines the interface for triggering an on-demand runner startup.
type RunnerWakeup interface {
	TriggerWakeup(ctx context.Context) error
}

// PlanReconciler finishes plans whose subtasks are all done. *service.JJOrchestrator implements it.
type PlanReconciler interface {
	ReconcileActiveForUser(ctx context.Context, userID uuid.UUID) error
}

// planReconcileEvery bounds how often one process re-checks a user's plans on task-list reads.
const planReconcileEvery = 10 * time.Second

type TaskHandler struct {
	engine             *service.TaskEngine
	taskRepo           repository.TaskRepository
	wakeup             RunnerWakeup
	scaleToZeroEnabled bool

	reconciler    PlanReconciler
	reconcileMu   sync.Mutex
	lastReconcile map[uuid.UUID]time.Time

	objectives ObjectiveSubmitter
}

// WithObjectives lets the handler route top-level objective requests through the orchestrator.
func (h *TaskHandler) WithObjectives(o ObjectiveSubmitter) *TaskHandler {
	h.objectives = o
	return h
}

// WithPlanReconciler makes task-list reads finish plans whose subtasks have all completed.
func (h *TaskHandler) WithPlanReconciler(r PlanReconciler) *TaskHandler {
	h.reconciler = r
	return h
}

// reconcilePlans is best effort and throttled: it never delays or fails the read it piggybacks on.
func (h *TaskHandler) reconcilePlans(ctx context.Context, userID uuid.UUID) {
	if h.reconciler == nil {
		return
	}
	h.reconcileMu.Lock()
	if h.lastReconcile == nil {
		h.lastReconcile = map[uuid.UUID]time.Time{}
	}
	if time.Since(h.lastReconcile[userID]) < planReconcileEvery {
		h.reconcileMu.Unlock()
		return
	}
	h.lastReconcile[userID] = time.Now()
	h.reconcileMu.Unlock()

	rctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := h.reconciler.ReconcileActiveForUser(rctx, userID); err != nil {
		slog.Warn("plan reconcile on task list failed", "user_id", userID, "error", err)
	}
}

func NewTaskHandler(engine *service.TaskEngine, taskRepo repository.TaskRepository, wakeup RunnerWakeup, scaleToZeroEnabled bool) *TaskHandler {
	return &TaskHandler{
		engine:             engine,
		taskRepo:           taskRepo,
		wakeup:             wakeup,
		scaleToZeroEnabled: scaleToZeroEnabled,
	}
}

type CreateTaskRequest struct {
	WorkspaceID    *uuid.UUID          `json:"workspace_id,omitempty"`
	ParentID       *uuid.UUID          `json:"parent_id,omitempty"`
	ManagerDomain  string              `json:"manager_domain"`
	Title          string              `json:"title"`
	Goal           string              `json:"goal"`
	InputPayload   json.RawMessage     `json:"input_payload"`
	Priority       domain.TaskPriority `json:"priority"`
	IdempotencyKey *string             `json:"idempotency_key,omitempty"`
	ScheduledFor   *time.Time          `json:"scheduled_for,omitempty"`
	Dependencies   []uuid.UUID         `json:"dependencies,omitempty"`
}

func (h *TaskHandler) CreateTask(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	var req CreateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.ErrorJSON(w, badRequestFromDecode(err))
		return
	}
	if err := domain.ValidatePriority(req.Priority); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	// Support idempotency key via header as well
	if headerKey := r.Header.Get("Idempotency-Key"); headerKey != "" && req.IdempotencyKey == nil {
		req.IdempotencyKey = &headerKey
	}

	// Route top-level JJ objective requests through the orchestrator so they get broken down into a plan
	// with proper capability constraints (e.g., messaging device hands) rather than running un-orchestrated.
	managerDomain := strings.TrimSpace(req.ManagerDomain)
	isJJ := managerDomain == "" || managerDomain == "JJ" || managerDomain == domain.DefaultManagerName
	if req.ParentID == nil && len(req.Dependencies) == 0 && h.objectives != nil && isJJ {
		goalText := strings.TrimSpace(req.Goal)
		if goalText == "" {
			goalText = strings.TrimSpace(req.Title)
		}
		if goalText != "" && service.ClassifyMessage(goalText) == service.MessageKindObjective {
			userReq := &domain.UserRequest{
				UserID:      userID,
				WorkspaceID: req.WorkspaceID,
				RequestText: goalText,
				Context:     req.InputPayload,
				Priority:    req.Priority,
			}
			orch, _, err := h.objectives.SubmitRequest(r.Context(), userReq)
			if err != nil {
				httputil.ErrorJSON(w, err)
				return
			}
			rootTask, err := h.engine.GetTaskForUser(r.Context(), orch.RootTaskID, userID)
			if err != nil {
				httputil.ErrorJSON(w, err)
				return
			}
			if h.scaleToZeroEnabled && h.wakeup != nil {
				if n, err := h.objectives.RunnableChildCount(r.Context(), orch.ID); err == nil && n > 0 {
					wakeCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
					_ = h.wakeup.TriggerWakeup(wakeCtx)
					cancel()
				}
			}
			httputil.JSON(w, http.StatusCreated, rootTask)
			return
		}
	}

	task := &domain.Task{
		UserID:         userID,
		WorkspaceID:    req.WorkspaceID,
		ParentID:       req.ParentID,
		ManagerDomain:  req.ManagerDomain,
		Title:          req.Title,
		Goal:           req.Goal,
		InputPayload:   req.InputPayload,
		Priority:       req.Priority,
		IdempotencyKey: req.IdempotencyKey,
		ScheduledFor:   req.ScheduledFor,
		MaxRetries:     domain.DefaultMaxRetries,
	}

	created, err := h.engine.CreateTask(r.Context(), task, req.Dependencies)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	// In scale-to-zero mode, when work is queued for a runner, asynchronously trigger wake-up
	// and return 202 Accepted with status header.
	if h.scaleToZeroEnabled && created.Status == domain.TaskStatusQueued {
		if h.wakeup != nil {
			// Synchronous (bounded) so it also completes on Lambda, where the container is frozen
			// right after the response. Best-effort: the task is durably queued and the scheduler
			// tick retries the wake-up if this fails.
			wakeCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			_ = h.wakeup.TriggerWakeup(wakeCtx)
			cancel()
		}
		w.Header().Set("X-Runner-Status", "WAKEUP_SIGNALED")
		httputil.JSON(w, http.StatusAccepted, created)
		return
	}

	httputil.JSON(w, http.StatusCreated, created)
}

func (h *TaskHandler) GetTask(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	taskID, err := uuid.Parse(idStr)
	if err != nil {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("invalid task id format")))
		return
	}

	task, err := h.engine.GetTaskForUser(r.Context(), taskID, userID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	httputil.JSON(w, http.StatusOK, task)
}

func (h *TaskHandler) ListTasks(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	h.reconcilePlans(r.Context(), userID)

	query := r.URL.Query()
	filter := repository.TaskFilter{
		UserID: &userID,
	}

	if statusStr := query.Get("status"); statusStr != "" {
		st := domain.TaskStatus(statusStr)
		filter.Status = &st
	}

	if domainStr := query.Get("manager_domain"); domainStr != "" {
		filter.ManagerDomain = &domainStr
	}

	if parentStr := query.Get("parent_id"); parentStr != "" {
		if pid, err := uuid.Parse(parentStr); err == nil {
			filter.ParentID = &pid
		}
	}

	if limitStr := query.Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			filter.Limit = l
		}
	}

	if offsetStr := query.Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			filter.Offset = o
		}
	}

	tasks, err := h.engine.ListTasks(r.Context(), filter)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	if tasks == nil {
		tasks = []*domain.Task{}
	}

	httputil.JSON(w, http.StatusOK, tasks)
}

type CancelTaskRequest struct {
	Reason string `json:"reason"`
}

func (h *TaskHandler) CancelTask(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	taskID, err := uuid.Parse(idStr)
	if err != nil {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("invalid task id format")))
		return
	}

	// Verify task belongs to user
	_, err = h.engine.GetTaskForUser(r.Context(), taskID, userID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	var req CancelTaskRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Reason == "" {
		req.Reason = "Cancelled by user"
	}

	if err := h.engine.CancelTask(r.Context(), taskID, req.Reason); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]string{
		"message": "Task cancelled successfully",
	})
}

// ownedTaskID parses {id} and verifies the task belongs to the caller.
func (h *TaskHandler) ownedTaskID(w http.ResponseWriter, r *http.Request) (uuid.UUID, *domain.Task, bool) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return uuid.Nil, nil, false
	}
	taskID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("invalid task id format")))
		return uuid.Nil, nil, false
	}
	task, err := h.engine.GetTaskForUser(r.Context(), taskID, userID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return uuid.Nil, nil, false
	}
	return taskID, task, true
}

// ResumeTask re-queues a failed or cancelled standalone task.
func (h *TaskHandler) ResumeTask(w http.ResponseWriter, r *http.Request) {
	taskID, _, ok := h.ownedTaskID(w, r)
	if !ok {
		return
	}
	if err := h.engine.ResumeTask(r.Context(), taskID); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if h.scaleToZeroEnabled && h.wakeup != nil {
		wakeCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		_ = h.wakeup.TriggerWakeup(wakeCtx) // best-effort: the task is durably queued
		cancel()
	}
	task, err := h.engine.GetTask(r.Context(), taskID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, task)
}

// DeleteTask permanently removes a finished task and its subtasks.
func (h *TaskHandler) DeleteTask(w http.ResponseWriter, r *http.Request) {
	taskID, _, ok := h.ownedTaskID(w, r)
	if !ok {
		return
	}
	if err := h.engine.DeleteTask(r.Context(), taskID); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *TaskHandler) GetTaskEvents(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	taskID, err := uuid.Parse(idStr)
	if err != nil {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("invalid task id format")))
		return
	}

	// Verify ownership
	if _, err := h.engine.GetTaskForUser(r.Context(), taskID, userID); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	events, err := h.taskRepo.GetEvents(r.Context(), taskID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if events == nil {
		events = []*domain.TaskEvent{}
	}

	httputil.JSON(w, http.StatusOK, events)
}

func (h *TaskHandler) GetTaskAttempts(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	taskID, err := uuid.Parse(idStr)
	if err != nil {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("invalid task id format")))
		return
	}

	// Verify ownership
	if _, err := h.engine.GetTaskForUser(r.Context(), taskID, userID); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	attempts, err := h.taskRepo.GetAttempts(r.Context(), taskID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if attempts == nil {
		attempts = []*domain.TaskAttempt{}
	}

	httputil.JSON(w, http.StatusOK, attempts)
}

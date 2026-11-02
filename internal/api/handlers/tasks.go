package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/service"
)

type TaskHandler struct {
	engine   *service.TaskEngine
	taskRepo repository.TaskRepository
}

func NewTaskHandler(engine *service.TaskEngine, taskRepo repository.TaskRepository) *TaskHandler {
	return &TaskHandler{
		engine:   engine,
		taskRepo: taskRepo,
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
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, err))
		return
	}

	// Support idempotency key via header as well
	if headerKey := r.Header.Get("Idempotency-Key"); headerKey != "" && req.IdempotencyKey == nil {
		req.IdempotencyKey = &headerKey
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
		MaxRetries:     3,
	}

	created, err := h.engine.CreateTask(r.Context(), task, req.Dependencies)
	if err != nil {
		httputil.ErrorJSON(w, err)
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

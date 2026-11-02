package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/service"
)

type WorkerHandler struct {
	engine        *service.TaskEngine
	leaseDuration time.Duration
}

func NewWorkerHandler(engine *service.TaskEngine, leaseDuration time.Duration) *WorkerHandler {
	if leaseDuration <= 0 {
		leaseDuration = 60 * time.Second
	}
	return &WorkerHandler{
		engine:        engine,
		leaseDuration: leaseDuration,
	}
}

type ClaimTaskRequest struct {
	WorkerID string `json:"worker_id,omitempty"`
}

func (h *WorkerHandler) ClaimTask(w http.ResponseWriter, r *http.Request) {
	workerID := middleware.GetWorkerID(r.Context())

	// Optional override from body
	var req ClaimTaskRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.WorkerID != "" {
		workerID = req.WorkerID
	}

	task, err := h.engine.ClaimTask(r.Context(), workerID, h.leaseDuration)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	if task == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	httputil.JSON(w, http.StatusOK, task)
}

func (h *WorkerHandler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	workerID := middleware.GetWorkerID(r.Context())

	idStr := r.PathValue("task_id")
	taskID, err := uuid.Parse(idStr)
	if err != nil {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("invalid task_id format")))
		return
	}

	if err := h.engine.Heartbeat(r.Context(), taskID, workerID, h.leaseDuration); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]string{
		"message": "Lease renewed successfully",
	})
}

type CompleteTaskRequest struct {
	Output json.RawMessage `json:"output"`
}

func (h *WorkerHandler) CompleteTask(w http.ResponseWriter, r *http.Request) {
	workerID := middleware.GetWorkerID(r.Context())

	idStr := r.PathValue("task_id")
	taskID, err := uuid.Parse(idStr)
	if err != nil {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("invalid task_id format")))
		return
	}

	var req CompleteTaskRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Output == nil {
		req.Output = json.RawMessage("{}")
	}

	if err := h.engine.CompleteTask(r.Context(), taskID, workerID, req.Output); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]string{
		"message": "Task completed successfully",
	})
}

type FailTaskRequest struct {
	Reason    string `json:"reason"`
	Retryable bool   `json:"retryable"`
}

func (h *WorkerHandler) FailTask(w http.ResponseWriter, r *http.Request) {
	workerID := middleware.GetWorkerID(r.Context())

	idStr := r.PathValue("task_id")
	taskID, err := uuid.Parse(idStr)
	if err != nil {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("invalid task_id format")))
		return
	}

	var req FailTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, err))
		return
	}

	if req.Reason == "" {
		req.Reason = "Worker reported execution failure"
	}

	if err := h.engine.FailTask(r.Context(), taskID, workerID, req.Reason, req.Retryable); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]string{
		"message": "Task failure recorded successfully",
	})
}

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/manager"
	"github.com/griffin/core/internal/service"
)

// OrchestratorHandler exposes JJ Orchestrator endpoints and Manager registry inspection.
type OrchestratorHandler struct {
	orchestrator *service.JJOrchestrator
	mgrReg       *manager.Registry

	wakeup             RunnerWakeup
	scaleToZeroEnabled bool
}

// NewOrchestratorHandler constructs a new OrchestratorHandler.
func NewOrchestratorHandler(orchestrator *service.JJOrchestrator, mgrReg *manager.Registry, wakeup RunnerWakeup, scaleToZeroEnabled bool) *OrchestratorHandler {
	return &OrchestratorHandler{
		orchestrator:       orchestrator,
		mgrReg:             mgrReg,
		wakeup:             wakeup,
		scaleToZeroEnabled: scaleToZeroEnabled,
	}
}

// SubmitRequestRequest represents the JSON payload to submit a goal to JJ.
type SubmitRequestRequest struct {
	RequestText string              `json:"request_text"`
	Context     json.RawMessage     `json:"context,omitempty"`
	Priority    domain.TaskPriority `json:"priority,omitempty"`
}

// SubmitRequest handles POST /api/v1/orchestrator/requests.
func (h *OrchestratorHandler) SubmitRequest(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	var req SubmitRequestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.ErrorJSON(w, badRequestFromDecode(err))
		return
	}
	if err := domain.ValidatePriority(req.Priority); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	if req.RequestText == "" {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("request_text is required")))
		return
	}

	userReq := &domain.UserRequest{
		UserID:      userID,
		RequestText: req.RequestText,
		Context:     req.Context,
		Priority:    req.Priority,
	}

	orch, plan, err := h.orchestrator.SubmitRequest(r.Context(), userReq)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	h.wakeRunnerIfRunnable(r.Context(), orch.ID)

	httputil.JSON(w, http.StatusCreated, map[string]any{
		"orchestration": orch,
		"plan":          plan,
	})
}

// wakeRunnerIfRunnable wakes a scale-to-zero runner when the orchestration produced at least one
// QUEUED child. Synchronous and bounded (Lambda freezes right after the response). Best-effort:
// the orchestration is durably persisted and the scheduler tick retries, so a failure is only logged.
func (h *OrchestratorHandler) wakeRunnerIfRunnable(ctx context.Context, orchID uuid.UUID) {
	if !h.scaleToZeroEnabled || h.wakeup == nil {
		return
	}
	n, err := h.orchestrator.RunnableChildCount(ctx, orchID)
	if err != nil {
		slog.Warn("orchestrator: could not determine runnable children; skipping wake", "orchestration_id", orchID, "error", err)
		return
	}
	if n == 0 {
		return
	}
	wakeCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := h.wakeup.TriggerWakeup(wakeCtx); err != nil {
		slog.Error("orchestrator: runner wake failed; scheduler tick will retry", "orchestration_id", orchID, "runnable_children", n, "error", err)
	}
}

// GetRequest handles GET /api/v1/orchestrator/requests/{id}.
func (h *OrchestratorHandler) GetRequest(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("invalid orchestration id")))
		return
	}

	// Reconcile latest child task states before responding
	_, _ = h.orchestrator.ReconcileOrchestration(r.Context(), id)

	orch, delegations, err := h.orchestrator.GetOrchestration(r.Context(), id, userID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]any{
		"orchestration": orch,
		"delegations":   delegations,
	})
}

// GetPlan handles GET /api/v1/orchestrator/plans/{id}.
func (h *OrchestratorHandler) GetPlan(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("invalid plan id")))
		return
	}

	plan, err := h.orchestrator.GetPlan(r.Context(), id, userID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]any{
		"plan": plan,
	})
}

// ListManagers handles GET /api/v1/managers.
func (h *OrchestratorHandler) ListManagers(w http.ResponseWriter, r *http.Request) {
	managers := h.mgrReg.List()
	httputil.JSON(w, http.StatusOK, managers)
}

// GetManager handles GET /api/v1/managers/{id}.
func (h *OrchestratorHandler) GetManager(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	mgr, ok := h.mgrReg.Get(id)
	if !ok {
		httputil.ErrorJSON(w, domain.ErrNotFound)
		return
	}

	httputil.JSON(w, http.StatusOK, mgr.Definition())
}

package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

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
}

// NewOrchestratorHandler constructs a new OrchestratorHandler.
func NewOrchestratorHandler(orchestrator *service.JJOrchestrator, mgrReg *manager.Registry) *OrchestratorHandler {
	return &OrchestratorHandler{
		orchestrator: orchestrator,
		mgrReg:       mgrReg,
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

	httputil.JSON(w, http.StatusCreated, map[string]any{
		"orchestration": orch,
		"plan":          plan,
	})
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

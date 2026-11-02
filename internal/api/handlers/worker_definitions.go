package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/service"
)

// WorkerDefinitionHandler exposes Agent Factory endpoints for specialist workers.
type WorkerDefinitionHandler struct {
	factory *service.AgentFactoryService
}

// NewWorkerDefinitionHandler constructs a new WorkerDefinitionHandler.
func NewWorkerDefinitionHandler(factory *service.AgentFactoryService) *WorkerDefinitionHandler {
	return &WorkerDefinitionHandler{factory: factory}
}

// ListDefinitions handles GET /api/v1/workers/definitions.
func (h *WorkerDefinitionHandler) ListDefinitions(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	managerID := r.URL.Query().Get("manager_id")
	defs, err := h.factory.ListWorkerDefinitions(r.Context(), userID, managerID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if defs == nil {
		defs = []*domain.WorkerDefinition{}
	}

	httputil.JSON(w, http.StatusOK, defs)
}

// GetDefinition handles GET /api/v1/workers/definitions/{id}.
func (h *WorkerDefinitionHandler) GetDefinition(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("invalid definition id")))
		return
	}

	def, err := h.factory.GetWorkerDefinition(r.Context(), id, userID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	httputil.JSON(w, http.StatusOK, def)
}

// GetDefinitionVersions handles GET /api/v1/workers/definitions/{id}/versions.
func (h *WorkerDefinitionHandler) GetDefinitionVersions(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("invalid definition id")))
		return
	}

	def, err := h.factory.GetWorkerDefinition(r.Context(), id, userID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	versions, err := h.factory.ListWorkerVersions(r.Context(), userID, def.WorkerKey)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if versions == nil {
		versions = []*domain.WorkerDefinition{}
	}

	httputil.JSON(w, http.StatusOK, versions)
}

// CreateDefinition handles POST /api/v1/workers/definitions.
func (h *WorkerDefinitionHandler) CreateDefinition(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	var def domain.WorkerDefinition
	if err := json.NewDecoder(r.Body).Decode(&def); err != nil {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, err))
		return
	}

	def.UserID = userID
	created, err := h.factory.CreateWorkerDefinition(r.Context(), &def, "USER")
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	httputil.JSON(w, http.StatusCreated, created)
}

// UpdateDefinition handles PUT /api/v1/workers/definitions/{id}.
func (h *WorkerDefinitionHandler) UpdateDefinition(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("invalid definition id")))
		return
	}

	existing, err := h.factory.GetWorkerDefinition(r.Context(), id, userID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	var updates domain.WorkerDefinitionUpdate
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, err))
		return
	}

	newVersion, err := h.factory.UpdateWorkerDefinition(r.Context(), userID, existing.WorkerKey, updates, "USER")
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	httputil.JSON(w, http.StatusOK, newVersion)
}

// ToggleDefinitionRequest represents the toggle payload.
type ToggleDefinitionRequest struct {
	IsActive bool `json:"is_active"`
}

// ToggleDefinition handles POST /api/v1/workers/definitions/{id}/toggle.
func (h *WorkerDefinitionHandler) ToggleDefinition(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("invalid definition id")))
		return
	}

	var req ToggleDefinitionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, err))
		return
	}

	updated, err := h.factory.ToggleWorker(r.Context(), id, userID, req.IsActive, "USER")
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	httputil.JSON(w, http.StatusOK, updated)
}

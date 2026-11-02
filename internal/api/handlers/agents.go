package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

type AgentHandler struct {
	agentRepo repository.AgentRepository
}

func NewAgentHandler(agentRepo repository.AgentRepository) *AgentHandler {
	return &AgentHandler{agentRepo: agentRepo}
}

func (h *AgentHandler) ListAgents(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	agents, err := h.agentRepo.ListAgents(r.Context(), userID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if agents == nil {
		agents = []*domain.Agent{}
	}

	httputil.JSON(w, http.StatusOK, agents)
}

type RegisterAgentRequest struct {
	Name                   string `json:"name"`
	RoleType               string `json:"role_type"`
	ManagerDomain          string `json:"manager_domain"`
	Description            string `json:"description"`
	SystemPrompt           string `json:"system_prompt"`
	ProviderPreference     string `json:"provider_preference"`
	ModelPreference        string `json:"model_preference"`
	MaxExecutionTimeoutSec int    `json:"max_execution_timeout_sec"`
}

func (h *AgentHandler) RegisterAgent(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	var req RegisterAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, err))
		return
	}

	if req.Name == "" || req.RoleType == "" || req.ManagerDomain == "" {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("name, role_type, and manager_domain are required")))
		return
	}

	if req.MaxExecutionTimeoutSec <= 0 {
		req.MaxExecutionTimeoutSec = 300
	}

	agent := &domain.Agent{
		UserID:                 userID,
		Name:                   req.Name,
		RoleType:               req.RoleType,
		ManagerDomain:          req.ManagerDomain,
		Description:            req.Description,
		SystemPrompt:           req.SystemPrompt,
		ProviderPreference:     req.ProviderPreference,
		ModelPreference:        req.ModelPreference,
		MaxExecutionTimeoutSec: req.MaxExecutionTimeoutSec,
		IsActive:               true,
	}

	created, err := h.agentRepo.RegisterAgent(r.Context(), agent)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	httputil.JSON(w, http.StatusCreated, created)
}

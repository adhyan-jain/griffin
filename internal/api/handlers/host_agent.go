package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/agent"
	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
)

// AgentToolRunner is the agent's typed tools as an outside caller sees them. *agent.Agent implements it.
type AgentToolRunner interface {
	ToolList() []agent.ToolInfo
	RunTool(ctx context.Context, userID, taskID uuid.UUID, name string, args json.RawMessage) (map[string]any, error)
}

// TaskGetter reads a task (the task engine implements it).
type TaskGetter interface {
	GetTask(ctx context.Context, id uuid.UUID) (*domain.Task, error)
}

// HostAgentHandler serves a host's model tasks the same typed tools JJ's chat uses. Every call is tied to a task the
// calling host currently holds the lease on, and runs as that task's owner with the scheduled origin.
type HostAgentHandler struct {
	Tools AgentToolRunner
	Tasks TaskGetter
}

// ListTools handles GET /api/v1/host/agent/tools.
func (h HostAgentHandler) ListTools(w http.ResponseWriter, r *http.Request) {
	if middleware.GetHost(r.Context()) == nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"tools": h.Tools.ToolList()})
}

type hostToolRequest struct {
	TaskID string          `json:"task_id"`
	Args   json.RawMessage `json:"args"`
}

// CallTool handles POST /api/v1/host/agent/tools/{name}.
func (h HostAgentHandler) CallTool(w http.ResponseWriter, r *http.Request) {
	host := middleware.GetHost(r.Context())
	if host == nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}
	var req hostToolRequest
	if err := decodeBody(r, &req); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	taskID, err := uuid.Parse(req.TaskID)
	if err != nil {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("task_id is required")))
		return
	}
	task, err := h.Tasks.GetTask(r.Context(), taskID)
	if err != nil || task == nil || task.UserID != host.UserID || task.Status != domain.TaskStatusRunning ||
		task.LeasedBy == nil || *task.LeasedBy != host.WorkerID() {
		httputil.ErrorJSON(w, domain.ErrForbidden)
		return
	}
	resp, err := h.Tools.RunTool(r.Context(), task.UserID, task.ID, r.PathValue("name"), req.Args)
	if err != nil {
		httputil.ErrorJSON(w, errors.Join(domain.ErrNotFound, err))
		return
	}
	httputil.JSON(w, http.StatusOK, resp)
}

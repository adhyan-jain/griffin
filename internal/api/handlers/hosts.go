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

// HostHandler serves two audiences: the owner (user JWT) manages hosts, and a host (host token) takes work.
type HostHandler struct {
	svc    *service.HostService
	engine *service.TaskEngine
}

// NewHostHandler builds a HostHandler.
func NewHostHandler(svc *service.HostService, engine *service.TaskEngine) *HostHandler {
	return &HostHandler{svc: svc, engine: engine}
}

func decodeBody(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return errors.Join(domain.ErrValidation, err)
	}
	return nil
}

// ---- owner endpoints (user JWT) ----

type registerHostRequest struct {
	Name         string          `json:"name"`
	Kind         domain.HostKind `json:"kind"`
	Capabilities []string        `json:"capabilities"`
}

// RegisterHost creates a host and returns its token once.
func (h *HostHandler) RegisterHost(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	var req registerHostRequest
	if err := decodeBody(r, &req); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	host, token, err := h.svc.Register(r.Context(), userID, req.Name, req.Kind, req.Capabilities)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httputil.JSON(w, http.StatusCreated, map[string]any{
		"host":  host,
		"token": token, // shown once; only its hash is kept
	})
}

// ListHosts returns the owner's hosts, the elected mode and tasks waiting for a host.
func (h *HostHandler) ListHosts(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	ov, err := h.svc.Overview(r.Context(), userID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, ov)
}

type updateHostRequest struct {
	Paused       *bool    `json:"paused,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	// SetCapabilities distinguishes "no change" from "grant nothing".
	SetCapabilities bool `json:"set_capabilities,omitempty"`
}

// UpdateHost pauses or resumes a host and/or replaces the capabilities the owner grants it.
func (h *HostHandler) UpdateHost(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("invalid host id")))
		return
	}
	var req updateHostRequest
	if err := decodeBody(r, &req); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if req.Paused == nil && !req.SetCapabilities {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("nothing to update")))
		return
	}
	if req.Paused != nil {
		if err := h.svc.SetPaused(r.Context(), userID, id, *req.Paused); err != nil {
			httputil.ErrorJSON(w, err)
			return
		}
	}
	if req.SetCapabilities {
		if err := h.svc.SetGranted(r.Context(), userID, id, req.Capabilities); err != nil {
			httputil.ErrorJSON(w, err)
			return
		}
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"message": "Host updated"})
}

// RevokeHost permanently disables a host's token.
func (h *HostHandler) RevokeHost(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("invalid host id")))
		return
	}
	if err := h.svc.Revoke(r.Context(), userID, id); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"message": "Host revoked"})
}

// ---- host endpoints (host token) ----

type hostHeartbeatRequest struct {
	Capabilities []string `json:"capabilities"`
	Version      string   `json:"version"`
}

// Heartbeat records presence and what the host can currently do. The reply tells the host what the owner
// allows right now, so a pause or a capability change takes effect within one beat.
func (h *HostHandler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	host := middleware.GetHost(r.Context())
	var req hostHeartbeatRequest
	if err := decodeBody(r, &req); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if req.Capabilities == nil {
		req.Capabilities = []string{}
	}
	if err := h.svc.Heartbeat(r.Context(), host, req.Capabilities, req.Version); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	// Report the owner-granted set intersected with what was just advertised.
	adv, _ := domain.NormalizeHostCapabilities(req.Capabilities)
	effective := (&domain.Host{Capabilities: host.Capabilities, Advertised: adv}).EffectiveCapabilities()
	if effective == nil {
		effective = []string{}
	}
	httputil.JSON(w, http.StatusOK, map[string]any{
		"now":                    time.Now().UTC(),
		"paused":                 host.Paused,
		"granted":                host.Capabilities,
		"effective_capabilities": effective,
		"live_window_seconds":    int(domain.HostLiveWindow / time.Second),
	})
}

// Claim leases the next task this host can do (204 when there is none).
func (h *HostHandler) Claim(w http.ResponseWriter, r *http.Request) {
	host := middleware.GetHost(r.Context())
	res, err := h.svc.Claim(r.Context(), host)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if res == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	httputil.JSON(w, http.StatusOK, res)
}

func taskIDParam(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue("task_id"))
	if err != nil {
		return uuid.Nil, errors.Join(domain.ErrValidation, errors.New("invalid task_id format"))
	}
	return id, nil
}

// TaskHeartbeat renews the lease on a task this host is running.
func (h *HostHandler) TaskHeartbeat(w http.ResponseWriter, r *http.Request) {
	host := middleware.GetHost(r.Context())
	id, err := taskIDParam(r)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	// A task heartbeat is also host presence.
	if err := h.svc.Heartbeat(r.Context(), host, host.Advertised, host.Version); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if err := h.engine.Heartbeat(r.Context(), id, host.WorkerID(), h.svc.LeaseDuration()); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"message": "Lease renewed successfully"})
}

// CompleteTask records a finished task. Only the host that holds the lease can complete it.
func (h *HostHandler) CompleteTask(w http.ResponseWriter, r *http.Request) {
	host := middleware.GetHost(r.Context())
	id, err := taskIDParam(r)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	var req CompleteTaskRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Output == nil {
		req.Output = json.RawMessage("{}")
	}
	if err := h.engine.CompleteTask(r.Context(), id, host.WorkerID(), req.Output); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"message": "Task completed successfully"})
}

// FailTask records a failed task. Only the host that holds the lease can fail it.
func (h *HostHandler) FailTask(w http.ResponseWriter, r *http.Request) {
	host := middleware.GetHost(r.Context())
	id, err := taskIDParam(r)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	var req FailTaskRequest
	if err := decodeBody(r, &req); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if req.Reason == "" {
		req.Reason = "Host reported execution failure"
	}
	if err := h.engine.FailTask(r.Context(), id, host.WorkerID(), req.Reason, req.Retryable); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"message": "Task failure recorded successfully"})
}

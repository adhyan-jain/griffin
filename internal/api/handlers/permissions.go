package handlers

import (
	"net/http"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/policy"
	"github.com/griffin/core/internal/service"
)

// PermissionsHandler serves the owner's permission policy to the owner (JWT) and to their hosts (token).
type PermissionsHandler struct{ svc *service.PolicyService }

// NewPermissionsHandler builds a PermissionsHandler.
func NewPermissionsHandler(svc *service.PolicyService) *PermissionsHandler {
	return &PermissionsHandler{svc: svc}
}

// Get returns the owner's policy and its version.
func (h *PermissionsHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	v, err := h.svc.Get(r.Context(), userID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, v)
}

type putPolicyRequest struct {
	Policy  policy.Policy `json:"policy"`
	Version int           `json:"version"`
}

// Put replaces the policy. The request names the version it read; a stale one is a 409.
func (h *PermissionsHandler) Put(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	var req putPolicyRequest
	if err := decodeBody(r, &req); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	v, err := h.svc.Put(r.Context(), userID, req.Policy, req.Version)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, v)
}

// HostGet returns the policy to the host that belongs to the owner. It is read-only for hosts.
func (h *PermissionsHandler) HostGet(w http.ResponseWriter, r *http.Request) {
	host := middleware.GetHost(r.Context())
	v, err := h.svc.Get(r.Context(), host.UserID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, v)
}

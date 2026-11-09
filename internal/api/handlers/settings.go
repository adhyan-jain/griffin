package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
)

// ProfileStore is the slice of profile storage the settings and chat endpoints need.
type ProfileStore interface {
	ManagerName(ctx context.Context, userID uuid.UUID) (string, error)
	SetManagerName(ctx context.Context, userID uuid.UUID, name string) error
}

// ResolveManagerName returns the owner's chosen name, or the default when none is set, the store is
// missing or it fails: a settings problem must never stop a chat message.
func ResolveManagerName(ctx context.Context, store ProfileStore, userID uuid.UUID) string {
	if store == nil {
		return domain.DefaultManagerName
	}
	stored, err := store.ManagerName(ctx, userID)
	if err != nil || stored == "" {
		return domain.DefaultManagerName
	}
	name, err := domain.NormalizeManagerName(stored)
	if err != nil {
		return domain.DefaultManagerName
	}
	return name
}

// SettingsHandler serves the owner's settings.
type SettingsHandler struct{ profiles ProfileStore }

// NewSettingsHandler constructs a SettingsHandler.
func NewSettingsHandler(p ProfileStore) *SettingsHandler { return &SettingsHandler{profiles: p} }

func managerNameBody(name string) map[string]any {
	return map[string]any{"manager_name": name, "default": domain.DefaultManagerName, "is_default": name == domain.DefaultManagerName}
}

// GetManagerName handles GET /api/v1/settings/manager-name.
func (h *SettingsHandler) GetManagerName(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}
	httputil.JSON(w, http.StatusOK, managerNameBody(ResolveManagerName(r.Context(), h.profiles, userID)))
}

// PutManagerName handles PUT /api/v1/settings/manager-name with {"manager_name": "..."}. Sending the
// default name puts it back.
func (h *SettingsHandler) PutManagerName(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}
	var body struct {
		ManagerName string `json:"manager_name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		httputil.ErrorJSON(w, badRequestFromDecode(err))
		return
	}
	name, err := domain.NormalizeManagerName(body.ManagerName)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if err := h.profiles.SetManagerName(r.Context(), userID, name); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, managerNameBody(name))
}

package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/database"
)

type HealthHandler struct {
	db *database.DB
}

func NewHealthHandler(db *database.DB) *HealthHandler {
	return &HealthHandler{db: db}
}

// Healthz reports server liveness.
func (h *HealthHandler) Healthz(w http.ResponseWriter, r *http.Request) {
	httputil.JSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

// Readyz reports whether the server is ready to accept traffic by verifying DB connectivity.
func (h *HealthHandler) Readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := h.db.Ping(ctx); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]any{
		"status":   "ready",
		"database": "connected",
		"time":     time.Now().UTC().Format(time.RFC3339),
	})
}

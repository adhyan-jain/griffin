package handlers

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
)

// ListingStore is what the listings routes need.
type ListingStore interface {
	List(ctx context.Context, userID uuid.UUID, stages []string, limit int) ([]*domain.Listing, error)
	Get(ctx context.Context, userID, id uuid.UUID) (*domain.Listing, error)
	Move(ctx context.Context, userID, id uuid.UUID, from, to, note string, taskID *uuid.UUID) error
}

// ListingsHandler serves the verified internship listings.
type ListingsHandler struct{ repo ListingStore }

// NewListingsHandler builds a ListingsHandler.
func NewListingsHandler(repo ListingStore) *ListingsHandler { return &ListingsHandler{repo: repo} }

// List returns the owner's listings, newest first. ?stage=verified,drafted filters.
func (h *ListingsHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	var stages []string
	if s := strings.TrimSpace(r.URL.Query().Get("stage")); s != "" {
		stages = strings.Split(s, ",")
	}
	rows, err := h.repo.List(r.Context(), userID, stages, 500)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if rows == nil {
		rows = []*domain.Listing{}
	}
	httputil.JSON(w, http.StatusOK, rows)
}

// SourceHealthStore is what the source-health route needs.
type SourceHealthStore interface {
	SourceHealth(ctx context.Context, userID uuid.UUID) ([]domain.SourceHealth, error)
}

// SourcesHandler serves how each opportunity source is doing.
type SourcesHandler struct{ repo SourceHealthStore }

// NewSourcesHandler builds a SourcesHandler.
func NewSourcesHandler(repo SourceHealthStore) *SourcesHandler { return &SourcesHandler{repo: repo} }

// Health returns the latest scan of every source target the owner has scanned, with how many failed since the last
// good one, so a source that went quiet is visible instead of reading as "no openings".
func (h *SourcesHandler) Health(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	rows, err := h.repo.SourceHealth(r.Context(), userID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if rows == nil {
		rows = []domain.SourceHealth{}
	}
	httputil.JSON(w, http.StatusOK, rows)
}

// Close lets the owner drop a listing they do not want.
func (h *ListingsHandler) Close(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrNotFound)
		return
	}
	l, err := h.repo.Get(r.Context(), userID, id)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if err := h.repo.Move(r.Context(), userID, id, l.Stage, domain.StageClosed, domain.ClosedByOwnerNote, nil); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

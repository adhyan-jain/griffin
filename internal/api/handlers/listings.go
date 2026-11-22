package handlers

import (
	"context"
	"fmt"
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

// AssessedLister is the store read that adds each listing's assessment and filters by it. The real repository
// implements it; a store that does not still serves the plain list.
type AssessedLister interface {
	ListAssessed(ctx context.Context, userID uuid.UUID, f domain.AssessedFilter) ([]*domain.Listing, error)
}

var (
	valueClasses  = map[string]bool{domain.ValueExceptional: true, domain.ValueStrong: true, domain.ValueGood: true, domain.ValueExploratory: true, domain.ValueLow: true}
	eligibilities = map[string]bool{domain.EligibilityConfirmed: true, domain.EligibilityLikely: true, domain.EligibilityUnknown: true, domain.EligibilityIneligible: true}
)

// listParam reads a comma-separated query value, keeping only the allowed words. A word that is not allowed is a
// validation error, so a typo never silently widens the list.
func listParam(r *http.Request, name string, allowed map[string]bool) ([]string, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return nil, nil
	}
	var out []string
	for _, v := range strings.Split(raw, ",") {
		v = strings.TrimSpace(v)
		if !allowed[v] {
			return nil, fmt.Errorf("%w: %s has no value %q", domain.ErrValidation, name, v)
		}
		out = append(out, v)
	}
	return out, nil
}

// List returns the owner's listings with their assessments, best value first (listings not yet assessed last).
// ?stage=verified,drafted filters by stage, ?class=exceptional,strong by value class and ?eligibility=unknown by
// eligibility (unknown also matches a listing not assessed yet: it is waiting for review).
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
	classes, err := listParam(r, "class", valueClasses)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	elig, err := listParam(r, "eligibility", eligibilities)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	var rows []*domain.Listing
	if lister, ok := h.repo.(AssessedLister); ok {
		rows, err = lister.ListAssessed(r.Context(), userID, domain.AssessedFilter{Stages: stages, Classes: classes, Eligibility: elig, Limit: 500})
	} else {
		rows, err = h.repo.List(r.Context(), userID, stages, 500)
	}
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

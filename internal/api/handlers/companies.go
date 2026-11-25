package handlers

import (
	"context"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

const (
	defaultListLimit = 50
	maxListLimit     = 200
)

// listLimit reads ?limit=, defaulting to 50 and capping at 200. A value that is not a positive number is a
// validation error, not a silent default.
func listLimit(r *http.Request) (int, error) {
	s := r.URL.Query().Get("limit")
	if s == "" {
		return defaultListLimit, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, domain.ErrValidation
	}
	return min(n, maxListLimit), nil
}

// CompanyStore reads the companies table with the research attached to it.
type CompanyStore interface {
	ListCompanySummaries(ctx context.Context, userID uuid.UUID, limit int) ([]*repository.CompanySummary, error)
	GetCompanyDetail(ctx context.Context, userID, id uuid.UUID) (*repository.CompanyDetail, error)
}

// CompaniesHandler serves the Companies page: the companies the opportunity scan found, with research attached.
type CompaniesHandler struct {
	Companies CompanyStore
}

// NewCompaniesHandler creates a CompaniesHandler.
func NewCompaniesHandler(c CompanyStore) *CompaniesHandler { return &CompaniesHandler{Companies: c} }

// List returns the owner's companies with their open-listing count and the latest observation of each property.
// It always returns an array, never null.
func (h *CompaniesHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}
	limit, err := listLimit(r)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	list, err := h.Companies.ListCompanySummaries(r.Context(), userID, limit)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if list == nil {
		list = []*repository.CompanySummary{}
	}
	httputil.JSON(w, http.StatusOK, list)
}

// Get returns one company with its listings (linked to the employer's own posting), its observations with their
// source and age, and the research evidence behind them.
func (h *CompaniesHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrValidation)
		return
	}
	d, err := h.Companies.GetCompanyDetail(r.Context(), userID, id)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, d)
}

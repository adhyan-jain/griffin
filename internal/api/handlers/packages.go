package handlers

import (
	"context"
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

// PackagePreparer prepares the application package for one listing.
type PackagePreparer interface {
	Prepare(ctx context.Context, userID, listingID uuid.UUID) (*domain.ApplicationPackage, error)
}

// PackageStore reads packages.
type PackageStore interface {
	PackageFor(ctx context.Context, userID, listingID uuid.UUID) (*domain.ApplicationPackage, error)
	GetPackage(ctx context.Context, userID, id uuid.UUID) (*domain.ApplicationPackage, error)
	PackageBriefs(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]*domain.PackageBrief, error)
}

// PackagesHandler serves application packages. Preparing one asks the model at most twice; nothing it makes is sent.
type PackagesHandler struct {
	prep  PackagePreparer
	store PackageStore
}

// NewPackagesHandler builds a PackagesHandler. prep may be nil: the routes then report that preparation is off.
func NewPackagesHandler(prep PackagePreparer, store PackageStore) *PackagesHandler {
	return &PackagesHandler{prep: prep, store: store}
}

// prepareTimeout keeps a request inside the 29s gateway: the model is cut off before it, and nothing is stored then.
const prepareTimeout = 24 * time.Second

func writeAPIError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(httputil.Response{Error: &httputil.APIError{Code: code, Message: msg}})
}

func pathID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return uuid.Nil, domain.ErrNotFound
	}
	return id, nil
}

// Prepare prepares (or, for a package that still needs the owner, prepares again) the listing's application package.
func (h *PackagesHandler) Prepare(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	id, err := pathID(r)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if h.prep == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", service.ErrNoPrep.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), prepareTimeout)
	defer cancel()
	pkg, err := h.prep.Prepare(ctx, userID, id)
	switch {
	case errors.Is(err, service.ErrModelUnavailable):
		writeAPIError(w, http.StatusBadGateway, "MODEL_UNAVAILABLE", err.Error())
		return
	case errors.Is(err, service.ErrNoPrep):
		writeAPIError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", err.Error())
		return
	case err != nil && pkg == nil:
		httputil.ErrorJSON(w, err)
		return
	case err != nil:
		// The package is stored but its approval could not be requested: say so, with the package.
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(httputil.Response{Success: true, Data: pkg, Error: &httputil.APIError{Code: "APPROVAL_PENDING", Message: err.Error()}})
		return
	}
	httputil.JSON(w, http.StatusOK, pkg)
}

// ForListing returns a listing's package.
func (h *PackagesHandler) ForListing(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	id, err := pathID(r)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	pkg, err := h.store.PackageFor(r.Context(), userID, id)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if pkg == nil {
		httputil.ErrorJSON(w, domain.ErrNotFound)
		return
	}
	httputil.JSON(w, http.StatusOK, pkg)
}

// Get returns a package by its id (the approvals page holds that id).
func (h *PackagesHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	id, err := pathID(r)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	pkg, err := h.store.GetPackage(r.Context(), userID, id)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, pkg)
}

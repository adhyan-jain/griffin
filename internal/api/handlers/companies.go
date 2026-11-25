package handlers

import (
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/research"
)

// CompaniesHandler serves Company Explorer endpoints.
type CompaniesHandler struct {
	Knowledge research.KnowledgeStore
	Listings  *repository.OpportunityRepository
}

// NewCompaniesHandler creates a CompaniesHandler.
func NewCompaniesHandler(k research.KnowledgeStore, l *repository.OpportunityRepository) *CompaniesHandler {
	return &CompaniesHandler{Knowledge: k, Listings: l}
}

// Explorer returns discovered companies for the Company Explorer view.
func (h *CompaniesHandler) Explorer(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	limit := 50
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil && l > 0 {
			limit = l
		}
	}

	entities, err := h.Knowledge.ListEntities(r.Context(), userID, domain.EntityCompany, limit)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	type CompanyCard struct {
		ID        string         `json:"id"`
		Name      string         `json:"name"`
		Domain    string         `json:"domain"`
		Summary   string         `json:"summary"`
		Metadata  map[string]any `json:"metadata"`
		UpdatedAt string         `json:"updated_at"`
	}

	var cards []CompanyCard
	for _, e := range entities {
		cards = append(cards, CompanyCard{
			ID:        e.ID.String(),
			Name:      e.Name,
			Domain:    e.CanonicalKey,
			Summary:   e.Summary,
			Metadata:  e.Metadata,
			UpdatedAt: e.UpdatedAt.Format("2006-01-02"),
		})
	}

	httputil.JSON(w, http.StatusOK, cards)
}

// Get returns full profile, relationships, and observations for one company.
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

	entity, err := h.Knowledge.GetEntity(r.Context(), userID, id)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	rels, _ := h.Knowledge.ListRelationships(r.Context(), userID, id)
	obs, _ := h.Knowledge.ListObservations(r.Context(), userID, id)

	payload := map[string]any{
		"company":       entity,
		"relationships": rels,
		"observations":  obs,
	}
	httputil.JSON(w, http.StatusOK, payload)
}

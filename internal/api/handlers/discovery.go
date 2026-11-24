package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/service"
	"github.com/griffin/core/internal/sources/ats"
)

// DiscoveryHandler provides endpoints for running job discovery and viewing staged listings.
type DiscoveryHandler struct {
	discovery *service.JobDiscoveryService
	repo      *repository.DiscoveryRepository
}

// NewDiscoveryHandler constructs a DiscoveryHandler.
func NewDiscoveryHandler(discovery *service.JobDiscoveryService, repo *repository.DiscoveryRepository) *DiscoveryHandler {
	return &DiscoveryHandler{discovery: discovery, repo: repo}
}

// RunRequest specifies optional target companies to sweep.
type RunRequest struct {
	Targets []struct {
		Source  string `json:"source"`
		Company string `json:"company"`
	} `json:"targets"`
}

// Run executes the discovery routine and stages matching opportunities.
func (h *DiscoveryHandler) Run(w http.ResponseWriter, r *http.Request) {
	if h.discovery == nil {
		http.Error(w, "discovery service unavailable", http.StatusServiceUnavailable)
		return
	}

	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req RunRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	var targets []struct {
		Source  domain.DiscoverySource
		Company string
	}
	if len(req.Targets) > 0 {
		for _, t := range req.Targets {
			targets = append(targets, struct {
				Source  domain.DiscoverySource
				Company string
			}{Source: domain.DiscoverySource(t.Source), Company: t.Company})
		}
	} else {
		targets = defaultDiscoveryTargets()
	}

	staged, err := h.discovery.SweepAndStage(r.Context(), userID, targets)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"staged_count": len(staged),
		"staged_jobs":  staged,
	})
}

// Jobs returns discovered opportunities for the user.
func (h *DiscoveryHandler) Jobs(w http.ResponseWriter, r *http.Request) {
	if h.repo == nil {
		http.Error(w, "discovery repo unavailable", http.StatusServiceUnavailable)
		return
	}

	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	status := r.URL.Query().Get("status")
	jobs, err := h.repo.ListDiscoveredJobs(r.Context(), userID, status, 50)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jobs": jobs,
	})
}

// defaultDiscoveryTargets is every verified seed board on a provider the discovery service reads (Greenhouse, Lever,
// Ashby), from the seed list in internal/sources/ats.
func defaultDiscoveryTargets() []struct {
	Source  domain.DiscoverySource
	Company string
} {
	var out []struct {
		Source  domain.DiscoverySource
		Company string
	}
	for _, s := range ats.Seeds().Seeds {
		switch src := domain.DiscoverySource(s.Provider); src {
		case domain.SourceGreenhouse, domain.SourceLever, domain.SourceAshby:
			out = append(out, struct {
				Source  domain.DiscoverySource
				Company string
			}{Source: src, Company: s.Token})
		}
	}
	return out
}

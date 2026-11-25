package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/database"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/research"
)

// ResearchHandler serves research engine HTTP endpoints.
type ResearchHandler struct {
	Engine *research.Engine
	Repo   research.ResearchStore
}

// NewResearchHandler creates a ResearchHandler.
func NewResearchHandler(e *research.Engine, repo research.ResearchStore) *ResearchHandler {
	return &ResearchHandler{Engine: e, Repo: repo}
}

// Create initiates a new research run.
func (h *ResearchHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	var req domain.ResearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.ErrorJSON(w, domain.ErrValidation)
		return
	}
	if req.Objective == "" {
		httputil.ErrorJSON(w, domain.ErrValidation)
		return
	}

	run, err := h.Engine.StartRun(r.Context(), userID, req)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	// Trigger execution cycle asynchronously
	go func() {
		_, _ = h.Engine.ExecuteCycle(contextBackground(), userID, run.ID)
	}()

	httputil.JSON(w, http.StatusAccepted, run)
}

// contextBackground wraps context.Background for detached async execution.
func contextBackground() context.Context {
	return database.WithSystem(context.Background())
}

// List returns recent research runs for the authenticated user.
func (h *ResearchHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	limit := 20
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil && l > 0 {
			limit = l
		}
	}

	runs, err := h.Repo.ListRuns(r.Context(), userID, limit)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, runs)
}

// Get returns one research run with findings and progress.
func (h *ResearchHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	runID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrValidation)
		return
	}

	run, err := h.Repo.GetRun(r.Context(), userID, runID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	httputil.JSON(w, http.StatusOK, run)
}

// Pause pauses an active research run.
func (h *ResearchHandler) Pause(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	runID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrValidation)
		return
	}

	if err := h.Engine.PauseRun(r.Context(), userID, runID); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "paused"})
}

// Resume resumes a paused research run.
func (h *ResearchHandler) Resume(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	runID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrValidation)
		return
	}

	run, err := h.Engine.ResumeRun(r.Context(), userID, runID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, run)
}

// Evidence returns all atomic evidence items gathered during a research run.
func (h *ResearchHandler) Evidence(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	runID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrValidation)
		return
	}

	items, err := h.Repo.ListEvidenceForRun(r.Context(), userID, runID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, items)
}

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/research"
)

// maxResearchBody caps a research request body.
const maxResearchBody = 64 << 10

// maxResearchList caps how many runs one listing returns.
const maxResearchList = 200

// ResearchHandler serves research engine HTTP endpoints.
type ResearchHandler struct {
	Engine *research.Engine
	Repo   research.ResearchStore
	// schedule creates the task that advances a run. Research never runs inside a request: the API runs on Lambda,
	// which freezes work left running after the response, so a run only moves when its task is claimed.
	schedule func(ctx context.Context, t *domain.Task) error
}

// NewResearchHandler creates a ResearchHandler.
func NewResearchHandler(e *research.Engine, repo research.ResearchStore) *ResearchHandler {
	return &ResearchHandler{Engine: e, Repo: repo}
}

// WithScheduler sets how the handler creates the task that advances a run.
func (h *ResearchHandler) WithScheduler(fn func(ctx context.Context, t *domain.Task) error) *ResearchHandler {
	h.schedule = fn
	return h
}

// scheduleRun creates the task that advances a run from where it stands. A task that already exists for the same
// point in the run is not an error.
func (h *ResearchHandler) scheduleRun(ctx context.Context, run *domain.ResearchRun) error {
	if h.schedule == nil {
		return errors.New("research scheduling is not configured")
	}
	err := h.schedule(ctx, domain.NewResearchRunTask(run.UserID, run.ID, run.NextStepIndex, run.Revision, run.Objective))
	if err != nil && !errors.Is(err, domain.ErrIdempotentConflict) {
		return err
	}
	return nil
}

// Create persists a new research run and schedules the task that carries it out; it returns at once.
func (h *ResearchHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}
	if h.schedule == nil {
		httputil.ErrorJSON(w, errors.New("research scheduling is not configured"))
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxResearchBody)
	var req domain.ResearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			httputil.ErrorJSON(w, err)
			return
		}
		httputil.ErrorJSON(w, fmt.Errorf("%w: invalid JSON body", domain.ErrValidation))
		return
	}
	if err := req.Normalize(); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	run, err := h.Engine.StartRun(r.Context(), userID, req)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if err := h.scheduleRun(r.Context(), run); err != nil {
		_ = h.Engine.FailRun(r.Context(), userID, run.ID, "could not schedule the run: "+err.Error())
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusAccepted, run)
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
			limit = min(l, maxResearchList)
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
	// A run that is active (just resumed, or stalled because its task was lost) gets a task for its next step. The
	// task's key names the point the run has reached, so asking twice does not make two.
	if isActiveResearch(run.Status) {
		if err := h.scheduleRun(r.Context(), run); err != nil {
			httputil.ErrorJSON(w, err)
			return
		}
	}
	httputil.JSON(w, http.StatusOK, run)
}

func isActiveResearch(p domain.ResearchPhase) bool {
	return !p.IsTerminal() && p != domain.PhasePaused
}

// Cancel stops a research run that has not finished.
func (h *ResearchHandler) Cancel(w http.ResponseWriter, r *http.Request) {
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
	if err := h.Engine.CancelRun(r.Context(), userID, runID); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
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

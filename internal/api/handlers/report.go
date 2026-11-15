package handlers

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

// ReportHandler serves the scorecard and a task's audit trail.
type ReportHandler struct{ repo *repository.ReportRepository }

// NewReportHandler builds a ReportHandler.
func NewReportHandler(repo *repository.ReportRepository) *ReportHandler {
	return &ReportHandler{repo: repo}
}

// Scorecard returns the counts and rates for the last ?days= (default 30, at most 365). With ?format=md it
// returns the table ready to paste into a README.
func (h *ReportHandler) Scorecard(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	days := 30
	if v := r.URL.Query().Get("days"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 365 {
			httputil.ErrorJSON(w, domain.ErrValidation)
			return
		}
		days = n
	}
	rows, err := h.repo.ScoreRows(r.Context(), userID, time.Now().AddDate(0, 0, -days))
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	sc := domain.ComputeScorecard(rows)
	if r.URL.Query().Get("format") == "md" {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		_, _ = w.Write([]byte(domain.ScorecardMarkdown(sc, days)))
		return
	}
	httputil.JSON(w, http.StatusOK, sc)
}

// Audit returns the chained audit trail of one task and whether the chain verifies.
func (h *ReportHandler) Audit(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrValidation)
		return
	}
	entries, err := h.repo.Audit(r.Context(), userID, id)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{
		"task_id": id, "entries": entries, "intact": domain.VerifyAudit(entries) == -1,
	})
}

// BriefBuilder builds the morning brief for an owner.
type BriefBuilder interface {
	Build(ctx context.Context, userID uuid.UUID) (domain.Brief, error)
}

// Brief returns today's brief. ?format=text returns just the words.
func BriefHandler(b BriefBuilder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, err := middleware.GetUserID(r.Context())
		if err != nil {
			httputil.ErrorJSON(w, err)
			return
		}
		br, err := b.Build(r.Context(), userID)
		if err != nil {
			httputil.ErrorJSON(w, err)
			return
		}
		if r.URL.Query().Get("format") == "text" {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte(br.Text))
			return
		}
		httputil.JSON(w, http.StatusOK, br)
	}
}

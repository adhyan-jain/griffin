package handlers

import (
	"net/http"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/service"
)

// DsaHandler serves the DSA coach.
type DsaHandler struct{ coach *service.DsaCoach }

// NewDsaHandler builds a DsaHandler.
func NewDsaHandler(c *service.DsaCoach) *DsaHandler { return &DsaHandler{coach: c} }

// Get returns today's problems, the owner's settings and a per-state count.
func (h *DsaHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	today, err := h.coach.Today(r.Context(), userID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	settings, err := h.coach.Store.Settings(r.Context(), userID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	all, err := h.coach.Store.Items(r.Context(), userID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	counts := map[string]int{}
	for _, it := range all {
		counts[it.State]++
	}
	type item struct {
		*domain.DsaItem
		URL string `json:"url"`
	}
	out := make([]item, 0, len(today))
	for _, it := range today {
		out = append(out, item{it, it.URL()})
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"today": out, "settings": settings, "counts": counts})
}

// PutSettings replaces the coach settings.
func (h *DsaHandler) PutSettings(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	var s domain.DsaSettings
	if err := decodeBody(r, &s); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if err := s.Validate(); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if err := h.coach.Store.SaveSettings(r.Context(), userID, s); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, s)
}

// Sync checks LeetCode now and reports which problems it just marked solved.
func (h *DsaHandler) Sync(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if _, err := h.coach.Today(r.Context(), userID); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	solved, err := h.coach.Sync(r.Context(), userID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if solved == nil {
		solved = []string{}
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"solved": solved})
}

// MarkSolved marks one problem solved by hand.
func (h *DsaHandler) MarkSolved(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if err := h.coach.MarkSolved(r.Context(), userID, r.PathValue("slug")); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"slug": r.PathValue("slug"), "state": domain.DsaSolved})
}

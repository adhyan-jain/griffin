package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/service"
)

type InboundEventHandler struct {
	Triage *service.InboundTriageService
	Repo   *repository.InboundEventRepository
}

func NewInboundEventHandler(triage *service.InboundTriageService, repo *repository.InboundEventRepository) *InboundEventHandler {
	return &InboundEventHandler{
		Triage: triage,
		Repo:   repo,
	}
}

type InboundEventRequest struct {
	Source      string          `json:"source"`
	PackageName string          `json:"package_name"`
	Title       string          `json:"title"`
	Text        string          `json:"text"`
	Payload     json.RawMessage `json:"payload"`
}

// Ingest handles POST /api/v1/events/inbound.
func (h *InboundEventHandler) Ingest(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	var req InboundEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.ErrorJSON(w, domain.ErrValidation)
		return
	}

	if req.Source == "" {
		req.Source = "unknown"
	}

	event, triageResult, err := h.Triage.Ingest(r.Context(), userID, req.Source, req.PackageName, req.Title, req.Text, req.Payload)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	httputil.JSON(w, http.StatusCreated, map[string]any{
		"event":  event,
		"triage": triageResult,
	})
}

// List handles GET /api/v1/events/inbound.
func (h *InboundEventHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	limit := 50
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	onlyActionable := r.URL.Query().Get("actionable") == "true"
	var events []*domain.InboundEvent
	if onlyActionable {
		events, err = h.Repo.ListActionable(r.Context(), userID, limit)
	} else {
		events, err = h.Repo.List(r.Context(), userID, limit)
	}
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]any{
		"events": events,
		"count":  len(events),
	})
}

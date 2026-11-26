package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/service"
)

// CampaignHandler handles HTTP requests for the VIT Alumni prospecting campaign.
type CampaignHandler struct {
	svc *service.VITCampaignService
}

// NewCampaignHandler constructs a CampaignHandler.
func NewCampaignHandler(svc *service.VITCampaignService) *CampaignHandler {
	return &CampaignHandler{svc: svc}
}

// GetStats returns live cohort and prospecting statistics.
func (h *CampaignHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	stats, err := h.svc.GetCampaignStats(r.Context(), userID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, stats)
}

// GetQueue returns the prioritised pilot outreach queue.
func (h *CampaignHandler) GetQueue(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	limit := 25
	if s := r.URL.Query().Get("limit"); s != "" {
		if l, err := strconv.Atoi(s); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	queue, err := h.svc.GetCampaignQueue(r.Context(), userID, limit)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if queue == nil {
		queue = []*domain.VITCampaignTarget{}
	}
	httputil.JSON(w, http.StatusOK, queue)
}

// BuildQueue turns the owner's own people records into outreach targets.
func (h *CampaignHandler) BuildQueue(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	stats, err := h.svc.BuildQueue(r.Context(), userID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, stats)
}

// UpdateTargetAction transitions a target's status in response to user action.
func (h *CampaignHandler) UpdateTargetAction(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	targetID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrValidation)
		return
	}

	var body struct {
		Action string `json:"action"` // approve, sent_manually, reply, dnc
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httputil.ErrorJSON(w, fmt.Errorf("%w: invalid JSON body", domain.ErrValidation))
		return
	}

	body.Action = strings.ToLower(strings.TrimSpace(body.Action))
	if err := h.svc.UpdateTargetAction(r.Context(), userID, targetID, body.Action); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]string{"status": "updated", "action": body.Action})
}

// GetLinkedInSearchQueries returns pre-composed native Boolean search queries with one-click URLs.
func (h *CampaignHandler) GetLinkedInSearchQueries(w http.ResponseWriter, r *http.Request) {
	queries := h.svc.GetLinkedInQueries()
	httputil.JSON(w, http.StatusOK, queries)
}

package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/service"
)

func TestCampaignHandler_GetLinkedInSearchQueries(t *testing.T) {
	svc := service.NewVITCampaignService(nil, nil, nil)
	h := NewCampaignHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alumni/search-query", nil)
	rec := httptest.NewRecorder()

	h.GetLinkedInSearchQueries(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Success bool                         `json:"success"`
		Data    []domain.LinkedInSearchQuery `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	queries := resp.Data
	if len(queries) == 0 {
		t.Fatalf("expected queries, got 0")
	}

	if queries[0].Query == "" || queries[0].URL == "" {
		t.Errorf("query or URL is empty: %+v", queries[0])
	}
}

func TestCampaignHandler_BuildQueueRequiresUser(t *testing.T) {
	h := NewCampaignHandler(service.NewVITCampaignService(nil, nil, nil))
	rec := httptest.NewRecorder()
	h.BuildQueue(rec, httptest.NewRequest(http.MethodPost, "/api/v1/campaigns/vit-alumni/build-queue", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without a user, got %d", rec.Code)
	}
}

package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
)

func TestAlumniHandler(t *testing.T) {
	userID := uuid.New()
	kStore := newMockKnowledgeStore()
	appRepo := &fakeApprovalRepo{}
	handler := NewAlumniHandler(kStore, appRepo)

	// Seed alumni
	alumnus := &domain.CanonicalEntity{
		ID:           uuid.New(),
		UserID:       userID,
		Kind:         domain.EntityPerson,
		Name:         "Karan Mehta",
		CanonicalKey: "karan-mehta-vit-2021",
		Summary:      "Staff Engineer at Razorpay",
		Metadata: map[string]any{
			"title":     "Staff Engineer",
			"company":   "Razorpay",
			"function":  "engineering",
			"grad_year": float64(2021),
		},
		UpdatedAt: time.Now(),
	}
	require.NoError(t, kStore.UpsertEntity(context.Background(), alumnus))

	// 1. Explorer list
	req := withUserContext(httptest.NewRequest(http.MethodGet, "/api/v1/alumni/explorer", nil), userID)
	w := httptest.NewRecorder()
	handler.Explorer(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var listResp struct {
		Success bool `json:"success"`
		Data    []struct {
			ID             string `json:"id"`
			Name           string `json:"name"`
			GraduationYear int    `json:"graduation_year"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listResp))
	require.True(t, listResp.Success)
	require.Len(t, listResp.Data, 1)
	require.Equal(t, "Karan Mehta", listResp.Data[0].Name)
	require.Equal(t, 2021, listResp.Data[0].GraduationYear)

	// 2. Propose outreach draft (creates approval)
	body, _ := json.Marshal(map[string]string{
		"draft_text": "Hi Karan, loved your tech talk on payments infra. Let's connect!",
		"channel":    "linkedin_dm",
	})
	outreachReq := withUserContext(httptest.NewRequest(http.MethodPost, "/api/v1/alumni/"+alumnus.ID.String()+"/outreach", bytes.NewReader(body)), userID)
	outreachReq.SetPathValue("id", alumnus.ID.String())
	outreachW := httptest.NewRecorder()
	handler.Outreach(outreachW, outreachReq)
	require.Equal(t, http.StatusCreated, outreachW.Code)

	var appResp struct {
		Success bool             `json:"success"`
		Data    *domain.Approval `json:"data"`
	}
	require.NoError(t, json.Unmarshal(outreachW.Body.Bytes(), &appResp))
	require.True(t, appResp.Success)
	require.Equal(t, "alumni_outreach", appResp.Data.ActionType)
	require.Equal(t, domain.ApprovalStatusPending, appResp.Data.Status)
}

package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/service"
)

// InboxHandler provides HTTP endpoints for Gmail synchronization and assessment tracking.
type InboxHandler struct {
	syncer *service.GmailSyncer
	repo   *repository.InboxRepository
}

// NewInboxHandler constructs an InboxHandler.
func NewInboxHandler(syncer *service.GmailSyncer, repo *repository.InboxRepository) *InboxHandler {
	return &InboxHandler{syncer: syncer, repo: repo}
}

// SyncRequest represents the payload to process new or incremental email batches.
type SyncRequest struct {
	Messages []domain.EmailMessage `json:"messages"`
}

// Sync processes email messages and extracts recruiter assessments and deadlines.
func (h *InboxHandler) Sync(w http.ResponseWriter, r *http.Request) {
	if h.syncer == nil {
		http.Error(w, "syncer not configured", http.StatusServiceUnavailable)
		return
	}

	userIDVal := r.Context().Value("user_id")
	userID, ok := userIDVal.(uuid.UUID)
	if !ok || userID == uuid.Nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req SyncRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	res, err := h.syncer.ProcessMessages(r.Context(), userID, req.Messages)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

// Assessments lists pending technical assessments for the authenticated user.
func (h *InboxHandler) Assessments(w http.ResponseWriter, r *http.Request) {
	if h.repo == nil {
		http.Error(w, "inbox repository not configured", http.StatusServiceUnavailable)
		return
	}

	userIDVal := r.Context().Value("user_id")
	userID, ok := userIDVal.(uuid.UUID)
	if !ok || userID == uuid.Nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	assessments, err := h.repo.ListActiveAssessments(r.Context(), userID, nil)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"assessments": assessments,
	})
}

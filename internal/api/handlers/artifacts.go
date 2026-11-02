package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

type ArtifactHandler struct {
	artifactRepo repository.ArtifactRepository
}

func NewArtifactHandler(artifactRepo repository.ArtifactRepository) *ArtifactHandler {
	return &ArtifactHandler{artifactRepo: artifactRepo}
}

func (h *ArtifactHandler) ListArtifacts(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	var taskID *uuid.UUID
	if tIDStr := r.URL.Query().Get("task_id"); tIDStr != "" {
		if tid, err := uuid.Parse(tIDStr); err == nil {
			taskID = &tid
		}
	}

	artifacts, err := h.artifactRepo.ListArtifacts(r.Context(), userID, taskID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if artifacts == nil {
		artifacts = []*domain.Artifact{}
	}

	httputil.JSON(w, http.StatusOK, artifacts)
}

func (h *ArtifactHandler) GetArtifact(w http.ResponseWriter, r *http.Request) {
	_, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	idStr := r.PathValue("id")
	artifactID, err := uuid.Parse(idStr)
	if err != nil {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("invalid artifact id format")))
		return
	}

	art, err := h.artifactRepo.GetArtifactByID(r.Context(), artifactID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	httputil.JSON(w, http.StatusOK, art)
}

type CreateArtifactRequest struct {
	TaskID        *uuid.UUID      `json:"task_id,omitempty"`
	Title         string          `json:"title"`
	ArtifactType  string          `json:"artifact_type"`
	StorageURI    string          `json:"storage_uri"`
	FileSizeBytes *int64          `json:"file_size_bytes,omitempty"`
	MimeType      *string         `json:"mime_type,omitempty"`
	Metadata      json.RawMessage `json:"metadata,omitempty"`
}

func (h *ArtifactHandler) CreateArtifact(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	var req CreateArtifactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, err))
		return
	}

	if req.Title == "" || req.ArtifactType == "" || req.StorageURI == "" {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("title, artifact_type, and storage_uri are required")))
		return
	}

	art := &domain.Artifact{
		UserID:        userID,
		TaskID:        req.TaskID,
		Title:         req.Title,
		ArtifactType:  req.ArtifactType,
		StorageURI:    req.StorageURI,
		FileSizeBytes: req.FileSizeBytes,
		MimeType:      req.MimeType,
		Metadata:      req.Metadata,
	}

	created, err := h.artifactRepo.CreateArtifact(r.Context(), art)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	httputil.JSON(w, http.StatusCreated, created)
}

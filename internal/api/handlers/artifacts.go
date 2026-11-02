package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/storage"
)

type ArtifactHandler struct {
	artifactRepo repository.ArtifactRepository
	storageSvc   *storage.StorageService
}

func NewArtifactHandler(artifactRepo repository.ArtifactRepository, storageSvc *storage.StorageService) *ArtifactHandler {
	return &ArtifactHandler{
		artifactRepo: artifactRepo,
		storageSvc:   storageSvc,
	}
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
	userID, err := middleware.GetUserID(r.Context())
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

	art, err := h.artifactRepo.GetArtifactByIDAndUser(r.Context(), artifactID, userID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	httputil.JSON(w, http.StatusOK, art)
}

type CreateArtifactRequest struct {
	TaskID          *uuid.UUID      `json:"task_id,omitempty"`
	Title           string          `json:"title"`
	ArtifactType    string          `json:"artifact_type"`
	StorageURI      string          `json:"storage_uri"`
	StorageProvider string          `json:"storage_provider,omitempty"`
	Bucket          *string         `json:"bucket,omitempty"`
	ObjectKey       *string         `json:"object_key,omitempty"`
	Filename        *string         `json:"filename,omitempty"`
	FileSizeBytes   *int64          `json:"file_size_bytes,omitempty"`
	MimeType        *string         `json:"mime_type,omitempty"`
	Checksum        *string         `json:"checksum,omitempty"`
	Metadata        json.RawMessage `json:"metadata,omitempty"`
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

	provider := req.StorageProvider
	if provider == "" {
		provider = "S3"
	}

	art := &domain.Artifact{
		UserID:          userID,
		TaskID:          req.TaskID,
		Title:           req.Title,
		ArtifactType:    req.ArtifactType,
		StorageURI:      req.StorageURI,
		StorageProvider: provider,
		Bucket:          req.Bucket,
		ObjectKey:       req.ObjectKey,
		Filename:        req.Filename,
		FileSizeBytes:   req.FileSizeBytes,
		MimeType:        req.MimeType,
		Checksum:        req.Checksum,
		Metadata:        req.Metadata,
	}

	created, err := h.artifactRepo.CreateArtifact(r.Context(), art)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	httputil.JSON(w, http.StatusCreated, created)
}

func (h *ArtifactHandler) UploadArtifact(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	if h.storageSvc == nil {
		httputil.ErrorJSON(w, fmt.Errorf("%w: storage service is not initialized", domain.ErrValidation))
		return
	}

	var title, filename, artifactType, contentType string
	var taskID *uuid.UUID
	var bodyReader io.Reader

	// Check if request is multipart/form-data
	contentTypeHeader := r.Header.Get("Content-Type")
	if len(contentTypeHeader) >= 19 && contentTypeHeader[:19] == "multipart/form-data" {
		// Max memory 32MB for form parsing
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, fmt.Errorf("failed parsing multipart form: %w", err)))
			return
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("form field 'file' is required")))
			return
		}
		defer file.Close()

		bodyReader = file
		filename = header.Filename
		contentType = header.Header.Get("Content-Type")

		title = r.FormValue("title")
		artifactType = r.FormValue("artifact_type")
		if tIDStr := r.FormValue("task_id"); tIDStr != "" {
			if tid, err := uuid.Parse(tIDStr); err == nil {
				taskID = &tid
			}
		}
	} else {
		// Raw stream upload
		bodyReader = r.Body
		filename = r.Header.Get("X-Filename")
		if filename == "" {
			filename = r.URL.Query().Get("filename")
		}
		title = r.Header.Get("X-Title")
		if title == "" {
			title = r.URL.Query().Get("title")
		}
		artifactType = r.Header.Get("X-Artifact-Type")
		if artifactType == "" {
			artifactType = r.URL.Query().Get("artifact_type")
		}
		contentType = r.Header.Get("Content-Type")

		tIDStr := r.Header.Get("X-Task-ID")
		if tIDStr == "" {
			tIDStr = r.URL.Query().Get("task_id")
		}
		if tIDStr != "" {
			if tid, err := uuid.Parse(tIDStr); err == nil {
				taskID = &tid
			}
		}
	}

	if filename == "" {
		filename = "artifact.dat"
	}
	if title == "" {
		title = filepath.Base(filename)
	}
	if artifactType == "" {
		artifactType = "FILE"
	}

	created, err := h.storageSvc.SaveArtifact(r.Context(), storage.SaveArtifactInput{
		UserID:       userID,
		TaskID:       taskID,
		Title:        title,
		Filename:     filename,
		ArtifactType: artifactType,
		ContentType:  contentType,
		Body:         bodyReader,
	})
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	httputil.JSON(w, http.StatusCreated, created)
}

func (h *ArtifactHandler) DownloadArtifact(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	if h.storageSvc == nil {
		httputil.ErrorJSON(w, fmt.Errorf("%w: storage service is not initialized", domain.ErrValidation))
		return
	}

	idStr := r.PathValue("id")
	artifactID, err := uuid.Parse(idStr)
	if err != nil {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("invalid artifact id format")))
		return
	}

	art, downOut, err := h.storageSvc.DownloadArtifact(r.Context(), artifactID, userID)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	defer downOut.Body.Close()

	ct := "application/octet-stream"
	if art.MimeType != nil && *art.MimeType != "" {
		ct = *art.MimeType
	} else if downOut.ContentType != "" {
		ct = downOut.ContentType
	}

	fn := "artifact.dat"
	if art.Filename != nil && *art.Filename != "" {
		fn = *art.Filename
	}

	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filepath.Base(fn)))
	if downOut.ContentLength > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(downOut.ContentLength, 10))
	} else if art.FileSizeBytes != nil && *art.FileSizeBytes > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(*art.FileSizeBytes, 10))
	}
	if downOut.ETag != "" {
		w.Header().Set("ETag", downOut.ETag)
	}

	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, downOut.Body)
}

func (h *ArtifactHandler) PresignArtifact(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	if h.storageSvc == nil {
		httputil.ErrorJSON(w, fmt.Errorf("%w: storage service is not initialized", domain.ErrValidation))
		return
	}

	idStr := r.PathValue("id")
	artifactID, err := uuid.Parse(idStr)
	if err != nil {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("invalid artifact id format")))
		return
	}

	expiry := 15 * time.Minute
	if expStr := r.URL.Query().Get("expires_in"); expStr != "" {
		if dur, err := time.ParseDuration(expStr); err == nil && dur > 0 {
			expiry = dur
		}
	}

	presigned, err := h.storageSvc.GetPresignedDownloadURL(r.Context(), artifactID, userID, expiry)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	httputil.JSON(w, http.StatusOK, presigned)
}

func (h *ArtifactHandler) DeleteArtifact(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
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

	var delErr error
	if h.storageSvc != nil {
		delErr = h.storageSvc.DeleteArtifact(r.Context(), artifactID, userID)
	} else {
		delErr = h.artifactRepo.DeleteArtifact(r.Context(), artifactID, userID)
	}

	if delErr != nil {
		httputil.ErrorJSON(w, delErr)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

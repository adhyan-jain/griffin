package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
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
	taskRepo     repository.TaskRepository
	maxUpload    int64
	presignTTL   time.Duration
}

// NewArtifactHandler builds the artifact handler. taskRepo is used to verify that any task_id a
// caller supplies belongs to them; maxUpload caps request bodies (bytes, <=0 means 5 MiB).
func NewArtifactHandler(artifactRepo repository.ArtifactRepository, storageSvc *storage.StorageService, taskRepo repository.TaskRepository, maxUpload int64) *ArtifactHandler {
	if maxUpload <= 0 {
		maxUpload = 5 << 20
	}
	return &ArtifactHandler{
		artifactRepo: artifactRepo,
		storageSvc:   storageSvc,
		taskRepo:     taskRepo,
		maxUpload:    maxUpload,
		presignTTL:   15 * time.Minute,
	}
}

// WithPresignExpiry sets the default (and maximum) presigned URL lifetime; <=0 is ignored.
func (h *ArtifactHandler) WithPresignExpiry(d time.Duration) *ArtifactHandler {
	if d > 0 {
		h.presignTTL = d
	}
	return h
}

// ownsTask verifies a caller-supplied task id belongs to the user. Unknown and foreign tasks are
// indistinguishable (both ErrNotFound).
func (h *ArtifactHandler) ownsTask(r *http.Request, taskID *uuid.UUID, userID uuid.UUID) error {
	if taskID == nil {
		return nil
	}
	if h.taskRepo == nil {
		return domain.ErrNotFound
	}
	if _, err := h.taskRepo.GetTaskByIDAndUser(r.Context(), *taskID, userID); err != nil {
		return domain.ErrNotFound
	}
	return nil
}

// multipartParseError keeps an over-limit body distinguishable (413) from a malformed form (400).
func multipartParseError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return err
	}
	return errors.Join(domain.ErrValidation, fmt.Errorf("failed parsing multipart form: %w", err))
}

func (h *ArtifactHandler) ListArtifacts(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}

	var taskID *uuid.UUID
	if tIDStr := r.URL.Query().Get("task_id"); tIDStr != "" {
		tid, err := uuid.Parse(tIDStr)
		if err != nil {
			httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("invalid task_id format")))
			return
		}
		taskID = &tid
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

	if h.storageSvc == nil {
		httputil.ErrorJSON(w, fmt.Errorf("%w: storage service is not initialized", domain.ErrValidation))
		return
	}
	if req.ObjectKey == nil || *req.ObjectKey == "" {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("title, artifact_type and object_key are required")))
		return
	}
	if err := h.ownsTask(r, req.TaskID, userID); err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	// bucket, storage_uri and storage_provider from the client are never trusted.
	created, err := h.storageSvc.RegisterArtifact(r.Context(), storage.RegisterInput{
		UserID:       userID,
		TaskID:       req.TaskID,
		Title:        req.Title,
		ArtifactType: req.ArtifactType,
		Bucket:       req.Bucket,
		ObjectKey:    *req.ObjectKey,
		Filename:     req.Filename,
		MimeType:     req.MimeType,
		Checksum:     req.Checksum,
		Metadata:     req.Metadata,
	})
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

	r.Body = http.MaxBytesReader(w, r.Body, h.maxUpload)

	// Check if request is multipart/form-data
	contentTypeHeader := r.Header.Get("Content-Type")
	if len(contentTypeHeader) >= 19 && contentTypeHeader[:19] == "multipart/form-data" {
		// Max memory 32MB for form parsing
		if err := r.ParseMultipartForm(h.maxUpload); err != nil {
			httputil.ErrorJSON(w, multipartParseError(err))
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
			tid, err := uuid.Parse(tIDStr)
			if err != nil {
				httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("invalid task_id format")))
				return
			}
			taskID = &tid
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
			tid, err := uuid.Parse(tIDStr)
			if err != nil {
				httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("invalid task_id format")))
				return
			}
			taskID = &tid
		}
	}

	if err := h.ownsTask(r, taskID, userID); err != nil {
		httputil.ErrorJSON(w, err)
		return
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
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": storage.SanitizeFilename(fn)}))
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

	// expires_in may only shorten the configured lifetime, never extend it.
	expiry := h.presignTTL
	if expStr := r.URL.Query().Get("expires_in"); expStr != "" {
		if dur, err := time.ParseDuration(expStr); err == nil && dur > 0 && dur < expiry {
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

// CreateListRequest is the body of POST /api/v1/lists.
type CreateListRequest struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

// CreateList makes a new living list: a Markdown file marked as a list that can be appended to.
func (h *ArtifactHandler) CreateList(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}
	if h.storageSvc == nil {
		httputil.ErrorJSON(w, fmt.Errorf("%w: storage service is not initialized", domain.ErrValidation))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, h.maxUpload)
	var req CreateListRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("invalid JSON body")))
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		httputil.ErrorJSON(w, fmt.Errorf("%w: a list needs a title", domain.ErrValidation))
		return
	}
	body := strings.TrimRight(req.Content, "\n ")
	if body == "" {
		body = "# " + title
	}
	created, err := h.storageSvc.SaveArtifact(r.Context(), storage.SaveArtifactInput{
		UserID:       userID,
		Title:        title,
		Filename:     storage.SanitizeFilename(strings.ToLower(strings.ReplaceAll(title, " ", "-"))) + ".md",
		ArtifactType: "MARKDOWN",
		ContentType:  "text/markdown; charset=utf-8",
		Body:         strings.NewReader(body + "\n"),
		Metadata:     json.RawMessage(`{"kind":"` + storage.ListKind + `"}`),
	})
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusCreated, created)
}

// UpdateTextRequest is the body of PUT /api/v1/artifacts/{id}/text.
type UpdateTextRequest struct {
	Text string `json:"text"`
	Mode string `json:"mode"` // "replace" (default) or "append"
}

// UpdateArtifactText replaces or appends to a Markdown file the caller owns.
func (h *ArtifactHandler) UpdateArtifactText(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}
	if h.storageSvc == nil {
		httputil.ErrorJSON(w, fmt.Errorf("%w: storage service is not initialized", domain.ErrValidation))
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("invalid artifact id format")))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, h.maxUpload)
	var req UpdateTextRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.ErrorJSON(w, errors.Join(domain.ErrValidation, errors.New("invalid JSON body")))
		return
	}
	if req.Mode != "" && req.Mode != "replace" && req.Mode != "append" {
		httputil.ErrorJSON(w, fmt.Errorf("%w: mode must be replace or append", domain.ErrValidation))
		return
	}
	if req.Mode == "append" && strings.TrimSpace(req.Text) == "" {
		httputil.ErrorJSON(w, fmt.Errorf("%w: nothing to append", domain.ErrValidation))
		return
	}
	updated, err := h.storageSvc.UpdateText(r.Context(), id, userID, req.Text, req.Mode == "append", h.maxUpload)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, updated)
}

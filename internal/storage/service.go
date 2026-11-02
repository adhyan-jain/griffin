package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

// SaveArtifactInput defines parameters for saving an artifact to storage and database.
type SaveArtifactInput struct {
	UserID       uuid.UUID
	TaskID       *uuid.UUID
	Title        string
	Filename     string
	ArtifactType string
	ContentType  string
	Body         io.Reader
	Metadata     json.RawMessage
}

// StorageService coordinates object storage operations with artifact metadata persistence.
type StorageService struct {
	provider     Provider
	artifactRepo repository.ArtifactRepository
	bucket       string
	logger       *slog.Logger
}

// NewStorageService initializes a new StorageService.
func NewStorageService(
	provider Provider,
	artifactRepo repository.ArtifactRepository,
	bucket string,
	logger *slog.Logger,
) *StorageService {
	if logger == nil {
		logger = slog.Default()
	}
	return &StorageService{
		provider:     provider,
		artifactRepo: artifactRepo,
		bucket:       bucket,
		logger:       logger,
	}
}

// Provider returns the underlying storage provider.
func (s *StorageService) Provider() Provider {
	return s.provider
}

// Bucket returns the configured default bucket name.
func (s *StorageService) Bucket() string {
	return s.bucket
}

// GenerateObjectKey produces a task-scoped or artifact-scoped object key.
// Path structure:
//   - Task-scoped: tasks/{task_id}/outputs/{filename}
//   - Unscoped:    artifacts/{uuid}/{filename}
func GenerateObjectKey(taskID *uuid.UUID, filename string) string {
	cleanName := filepath.Base(filename)
	cleanName = strings.ReplaceAll(cleanName, " ", "_")
	if cleanName == "." || cleanName == "/" || cleanName == "" {
		cleanName = fmt.Sprintf("artifact-%d.dat", time.Now().UnixNano())
	}

	if taskID != nil && *taskID != uuid.Nil {
		return fmt.Sprintf("tasks/%s/outputs/%s", taskID.String(), cleanName)
	}
	return fmt.Sprintf("artifacts/%s/%s", uuid.New().String(), cleanName)
}

// DetectContentType determines the MIME type based on file extension and byte prefix.
func DetectContentType(filename string, headerBytes []byte) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".json":
		return "application/json"
	case ".md":
		return "text/markdown"
	case ".pdf":
		return "application/pdf"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".csv":
		return "text/csv"
	case ".txt":
		return "text/plain; charset=utf-8"
	case ".zip":
		return "application/zip"
	case ".html":
		return "text/html; charset=utf-8"
	default:
		if len(headerBytes) > 0 {
			detected := http.DetectContentType(headerBytes)
			if detected != "" {
				return detected
			}
		}
		return "application/octet-stream"
	}
}

// SaveArtifact streams object data to S3, records metadata in PostgreSQL, and enforces
// the compensating Saga pattern if the database write fails.
func (s *StorageService) SaveArtifact(ctx context.Context, input SaveArtifactInput) (*domain.Artifact, error) {
	if input.UserID == uuid.Nil {
		return nil, domain.ErrUnauthorized
	}
	if strings.TrimSpace(input.Title) == "" {
		return nil, fmt.Errorf("%w: title is required", domain.ErrValidation)
	}
	if strings.TrimSpace(input.Filename) == "" {
		return nil, fmt.Errorf("%w: filename is required", domain.ErrValidation)
	}
	if input.Body == nil {
		return nil, fmt.Errorf("%w: body reader is required", domain.ErrValidation)
	}

	cleanFilename := filepath.Base(input.Filename)
	objectKey := GenerateObjectKey(input.TaskID, cleanFilename)

	// Stream and compute SHA-256 checksum and size
	hasher := sha256.New()
	var buf bytes.Buffer
	tee := io.TeeReader(input.Body, hasher)
	size, err := io.Copy(&buf, tee)
	if err != nil {
		return nil, fmt.Errorf("%w: failed reading artifact body: %v", ErrUploadFailed, err)
	}
	checksum := hex.EncodeToString(hasher.Sum(nil))

	contentType := input.ContentType
	if contentType == "" || contentType == "application/octet-stream" {
		header := buf.Bytes()
		if len(header) > 512 {
			header = header[:512]
		}
		contentType = DetectContentType(cleanFilename, header)
	}

	artType := input.ArtifactType
	if artType == "" {
		artType = "FILE"
	}

	// 1. Upload to storage provider
	uploadOut, err := s.provider.Upload(ctx, UploadInput{
		Bucket:      s.bucket,
		Key:         objectKey,
		Body:        &buf,
		ContentType: contentType,
		Size:        size,
		Metadata: map[string]string{
			"griffin-user-id":  input.UserID.String(),
			"griffin-checksum": checksum,
		},
	})
	if err != nil {
		s.logger.Error("Storage provider upload failed",
			"bucket", s.bucket,
			"key", objectKey,
			"error", err,
		)
		return nil, fmt.Errorf("storage upload: %w", err)
	}

	meta := input.Metadata
	if meta == nil {
		meta = json.RawMessage("{}")
	}

	// 2. Prepare database artifact record
	art := &domain.Artifact{
		ID:              uuid.New(),
		UserID:          input.UserID,
		TaskID:          input.TaskID,
		Title:           input.Title,
		ArtifactType:    artType,
		StorageURI:      uploadOut.URI,
		StorageProvider: s.provider.Name(),
		Bucket:          &uploadOut.Bucket,
		ObjectKey:       &uploadOut.Key,
		Filename:        &cleanFilename,
		FileSizeBytes:   &size,
		MimeType:        &contentType,
		Checksum:        &checksum,
		Metadata:        meta,
	}

	// 3. Persist metadata to PostgreSQL
	created, err := s.artifactRepo.CreateArtifact(ctx, art)
	if err != nil {
		s.logger.Error("Artifact metadata persistence failed; initiating compensating delete",
			"bucket", uploadOut.Bucket,
			"key", uploadOut.Key,
			"error", err,
		)
		// Compensating Saga rollback: delete uploaded object so storage does not drift
		compCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if delErr := s.provider.Delete(compCtx, DeleteInput{
			Bucket: uploadOut.Bucket,
			Key:    uploadOut.Key,
		}); delErr != nil {
			s.logger.Error("Compensating delete failed for orphaned object",
				"bucket", uploadOut.Bucket,
				"key", uploadOut.Key,
				"delete_error", delErr,
			)
		}
		return nil, fmt.Errorf("persisting artifact metadata: %w", err)
	}

	s.logger.Info("Artifact created and stored successfully",
		"artifact_id", created.ID,
		"bucket", uploadOut.Bucket,
		"key", uploadOut.Key,
		"size", size,
		"checksum", checksum,
	)

	return created, nil
}

// DownloadArtifact retrieves the artifact metadata and opens a download stream from storage.
// Caller is responsible for closing the returned DownloadOutput.Body.
func (s *StorageService) DownloadArtifact(ctx context.Context, id uuid.UUID, userID uuid.UUID) (*domain.Artifact, *DownloadOutput, error) {
	art, err := s.artifactRepo.GetArtifactByIDAndUser(ctx, id, userID)
	if err != nil {
		return nil, nil, err
	}

	if art.Bucket == nil || art.ObjectKey == nil || *art.ObjectKey == "" {
		return nil, nil, fmt.Errorf("%w: artifact has no storage location", ErrObjectNotFound)
	}

	downOut, err := s.provider.Download(ctx, DownloadInput{
		Bucket: *art.Bucket,
		Key:    *art.ObjectKey,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("storage download: %w", err)
	}

	return art, downOut, nil
}

// DeleteArtifact deletes the object from storage and removes its metadata from PostgreSQL.
func (s *StorageService) DeleteArtifact(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	art, err := s.artifactRepo.GetArtifactByIDAndUser(ctx, id, userID)
	if err != nil {
		return err
	}

	if art.Bucket != nil && art.ObjectKey != nil && *art.ObjectKey != "" {
		if delErr := s.provider.Delete(ctx, DeleteInput{
			Bucket: *art.Bucket,
			Key:    *art.ObjectKey,
		}); delErr != nil && !errors.Is(delErr, ErrObjectNotFound) {
			s.logger.Warn("Failed to delete object from storage provider",
				"bucket", *art.Bucket,
				"key", *art.ObjectKey,
				"error", delErr,
			)
		}
	}

	return s.artifactRepo.DeleteArtifact(ctx, id, userID)
}

// GetPresignedDownloadURL generates a secure, time-limited presigned download URL for the artifact.
func (s *StorageService) GetPresignedDownloadURL(ctx context.Context, id uuid.UUID, userID uuid.UUID, lifetime time.Duration) (*PresignOutput, error) {
	art, err := s.artifactRepo.GetArtifactByIDAndUser(ctx, id, userID)
	if err != nil {
		return nil, err
	}

	if art.Bucket == nil || art.ObjectKey == nil || *art.ObjectKey == "" {
		return nil, fmt.Errorf("%w: artifact has no storage location", ErrObjectNotFound)
	}

	return s.provider.PresignDownloadURL(ctx, PresignInput{
		Bucket:    *art.Bucket,
		Key:       *art.ObjectKey,
		ExpiresIn: lifetime,
	})
}

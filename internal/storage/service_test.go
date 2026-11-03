package storage_test

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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/storage"
)

type mockArtifactRepo struct {
	mu        sync.RWMutex
	artifacts map[uuid.UUID]*domain.Artifact

	FailCreate error
	FailGet    error
	FailDelete error
}

func newMockArtifactRepo() *mockArtifactRepo {
	return &mockArtifactRepo{
		artifacts: make(map[uuid.UUID]*domain.Artifact),
	}
}

func (m *mockArtifactRepo) CreateArtifact(ctx context.Context, a *domain.Artifact) (*domain.Artifact, error) {
	if m.FailCreate != nil {
		return nil, m.FailCreate
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	a.CreatedAt = time.Now().UTC()
	m.artifacts[a.ID] = a
	return a, nil
}

func (m *mockArtifactRepo) GetArtifactByID(ctx context.Context, id uuid.UUID) (*domain.Artifact, error) {
	if m.FailGet != nil {
		return nil, m.FailGet
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	art, ok := m.artifacts[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return art, nil
}

func (m *mockArtifactRepo) GetArtifactByIDAndUser(ctx context.Context, id uuid.UUID, userID uuid.UUID) (*domain.Artifact, error) {
	if m.FailGet != nil {
		return nil, m.FailGet
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	art, ok := m.artifacts[id]
	if !ok || art.UserID != userID {
		return nil, domain.ErrNotFound
	}
	return art, nil
}

func (m *mockArtifactRepo) GetArtifactByObjectKey(ctx context.Context, bucket, objectKey string) (*domain.Artifact, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, art := range m.artifacts {
		if art.Bucket != nil && *art.Bucket == bucket && art.ObjectKey != nil && *art.ObjectKey == objectKey {
			return art, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (m *mockArtifactRepo) ListArtifacts(ctx context.Context, userID uuid.UUID, taskID *uuid.UUID) ([]*domain.Artifact, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var res []*domain.Artifact
	for _, art := range m.artifacts {
		if art.UserID == userID {
			if taskID == nil || (art.TaskID != nil && *art.TaskID == *taskID) {
				res = append(res, art)
			}
		}
	}
	return res, nil
}

func (m *mockArtifactRepo) DeleteArtifact(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	if m.FailDelete != nil {
		return m.FailDelete
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	art, ok := m.artifacts[id]
	if !ok || (userID != uuid.Nil && art.UserID != userID) {
		return domain.ErrNotFound
	}
	delete(m.artifacts, id)
	return nil
}

func TestStorageService_SaveArtifact_TaskScoped(t *testing.T) {
	ctx := context.Background()
	provider := storage.NewMockStorageProvider()
	repo := newMockArtifactRepo()
	bucket := "griffin-artifacts"

	svc := storage.NewStorageService(provider, repo, bucket, slog.Default())

	userID := uuid.New()
	taskID := uuid.New()
	filename := "report.json"
	content := []byte(`{"result":"task success","items":[1,2,3]}`)

	hasher := sha256.New()
	hasher.Write(content)
	expectedChecksum := hex.EncodeToString(hasher.Sum(nil))

	art, err := svc.SaveArtifact(ctx, storage.SaveArtifactInput{
		UserID:       userID,
		TaskID:       &taskID,
		Title:        "Task Output Report",
		Filename:     filename,
		ArtifactType: "JSON",
		Body:         bytes.NewReader(content),
		Metadata:     json.RawMessage(`{"env":"test"}`),
	})
	if err != nil {
		t.Fatalf("unexpected save error: %v", err)
	}

	// Verify task-scoped key format: tasks/{task_id}/outputs/{uuid}/{filename}
	expectedPrefix := storage.UserKeyPrefix(userID) + fmt.Sprintf("tasks/%s/outputs/", taskID.String())
	if art.ObjectKey == nil || !strings.HasPrefix(*art.ObjectKey, expectedPrefix) || !strings.HasSuffix(*art.ObjectKey, "/"+filename) {
		t.Fatalf("expected key under %s ending /%s, got %v", expectedPrefix, filename, art.ObjectKey)
	}
	if art.Bucket == nil || *art.Bucket != bucket {
		t.Fatalf("expected bucket %s, got %v", bucket, art.Bucket)
	}
	if art.Checksum == nil || *art.Checksum != expectedChecksum {
		t.Fatalf("expected checksum %s, got %v", expectedChecksum, art.Checksum)
	}
	if art.MimeType == nil || *art.MimeType != "application/json" {
		t.Fatalf("expected MIME application/json, got %v", art.MimeType)
	}
	if art.FileSizeBytes == nil || *art.FileSizeBytes != int64(len(content)) {
		t.Fatalf("expected size %d, got %v", len(content), art.FileSizeBytes)
	}
}

func TestStorageService_CompensatingDeleteOnDBFailure(t *testing.T) {
	ctx := context.Background()
	provider := storage.NewMockStorageProvider()
	repo := newMockArtifactRepo()
	bucket := "griffin-artifacts"

	// Simulate database failure
	dbErr := errors.New("database connection refused")
	repo.FailCreate = dbErr

	svc := storage.NewStorageService(provider, repo, bucket, slog.Default())

	userID := uuid.New()
	taskID := uuid.New()
	filename := "failed_record.txt"
	content := []byte("this content will be uploaded then deleted")

	_, err := svc.SaveArtifact(ctx, storage.SaveArtifactInput{
		UserID:       userID,
		TaskID:       &taskID,
		Title:        "Failed Save",
		Filename:     filename,
		ArtifactType: "FILE",
		Body:         bytes.NewReader(content),
	})
	if err == nil {
		t.Fatalf("expected error from SaveArtifact when DB fails, got nil")
	}

	// Verify that the object was cleaned up from storage via compensating delete
	listed, listErr := provider.List(ctx, storage.ListInput{Bucket: bucket, Prefix: storage.UserKeyPrefix(userID)})
	if listErr != nil {
		t.Fatalf("list: %v", listErr)
	}
	if len(listed.Objects) != 0 {
		t.Fatalf("expected no orphaned objects after compensating delete, got %d", len(listed.Objects))
	}
}

func TestStorageService_DownloadAndDelete(t *testing.T) {
	ctx := context.Background()
	provider := storage.NewMockStorageProvider()
	repo := newMockArtifactRepo()
	bucket := "griffin-artifacts"

	svc := storage.NewStorageService(provider, repo, bucket, slog.Default())

	userID := uuid.New()
	taskID := uuid.New()
	content := []byte("download and delete test")

	art, err := svc.SaveArtifact(ctx, storage.SaveArtifactInput{
		UserID:       userID,
		TaskID:       &taskID,
		Title:        "Artifact to Delete",
		Filename:     "file.txt",
		ArtifactType: "FILE",
		Body:         bytes.NewReader(content),
	})
	if err != nil {
		t.Fatalf("unexpected save error: %v", err)
	}

	// Download
	_, downOut, err := svc.DownloadArtifact(ctx, art.ID, userID)
	if err != nil {
		t.Fatalf("unexpected download error: %v", err)
	}
	defer downOut.Body.Close()

	readData, err := io.ReadAll(downOut.Body)
	if err != nil {
		t.Fatalf("reading download stream: %v", err)
	}
	if !bytes.Equal(readData, content) {
		t.Fatalf("download content mismatch")
	}

	// Delete
	if err := svc.DeleteArtifact(ctx, art.ID, userID); err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}

	// Verify DB record is gone
	_, err = repo.GetArtifactByID(ctx, art.ID)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound from repo, got %v", err)
	}

	// Verify storage object is gone
	_, err = provider.Head(ctx, storage.HeadInput{
		Bucket: bucket,
		Key:    *art.ObjectKey,
	})
	if !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatalf("expected ErrObjectNotFound from storage, got %v", err)
	}
}

func TestStorageService_PresignedURL(t *testing.T) {
	ctx := context.Background()
	provider := storage.NewMockStorageProvider()
	repo := newMockArtifactRepo()
	bucket := "griffin-artifacts"

	svc := storage.NewStorageService(provider, repo, bucket, slog.Default())

	userID := uuid.New()
	art, err := svc.SaveArtifact(ctx, storage.SaveArtifactInput{
		UserID:       userID,
		Title:        "Presign Test",
		Filename:     "doc.pdf",
		ArtifactType: "PDF",
		Body:         bytes.NewReader([]byte("%PDF-1.4...")),
	})
	if err != nil {
		t.Fatalf("unexpected save error: %v", err)
	}

	presigned, err := svc.GetPresignedDownloadURL(ctx, art.ID, userID, 30*time.Minute)
	if err != nil {
		t.Fatalf("unexpected presign error: %v", err)
	}
	if !strings.Contains(presigned.URL, bucket) {
		t.Fatalf("presigned URL missing bucket: %s", presigned.URL)
	}
	if !presigned.ExpiresAt.After(time.Now().UTC()) {
		t.Fatalf("invalid expiry timestamp: %v", presigned.ExpiresAt)
	}
}

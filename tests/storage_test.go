package tests

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/storage"
)

func TestStorageIntegration_FullLifecycle(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	cleanTables(t, env.DB)

	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "storage-lifecycle@example.com")

	// 1. Create a task to scope the artifact
	task, err := env.TaskRepo.CreateTask(ctx, &domain.Task{
		UserID:        userID,
		Title:         "Data Processing Task",
		Goal:          "Process and export dataset",
		ManagerDomain: "JJ",
		Status:        domain.TaskStatusRunning,
		Priority:      domain.TaskPriorityNormalP2,
		InputPayload:  json.RawMessage(`{"job":"extract"}`),
	})
	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	content := []byte("Griffin Phase 4 S3 Object Storage Integration Test Payload")
	hasher := sha256.New()
	hasher.Write(content)
	expectedChecksum := hex.EncodeToString(hasher.Sum(nil))

	filename := "dataset_export.csv"

	// 2. Save Artifact via StorageService
	art, err := env.StorageService.SaveArtifact(ctx, storage.SaveArtifactInput{
		UserID:       userID,
		TaskID:       &task.ID,
		Title:        "Dataset CSV Export",
		Filename:     filename,
		ArtifactType: "FILE",
		ContentType:  "text/csv",
		Body:         bytes.NewReader(content),
		Metadata:     json.RawMessage(`{"records":100}`),
	})
	if err != nil {
		t.Fatalf("failed to save artifact: %v", err)
	}

	// 3. Verify Database Metadata
	if art.ID == uuid.Nil {
		t.Fatalf("artifact ID should not be nil")
	}
	expectedObjectPrefix := storage.UserKeyPrefix(userID) + fmt.Sprintf("tasks/%s/outputs/", task.ID.String())
	if art.ObjectKey == nil || !strings.HasPrefix(*art.ObjectKey, expectedObjectPrefix) || !strings.HasSuffix(*art.ObjectKey, "/"+filename) {
		t.Fatalf("expected object key under %s ending /%s, got %v", expectedObjectPrefix, filename, art.ObjectKey)
	}
	if art.Checksum == nil || *art.Checksum != expectedChecksum {
		t.Fatalf("expected checksum %s, got %v", expectedChecksum, art.Checksum)
	}
	if art.FileSizeBytes == nil || *art.FileSizeBytes != int64(len(content)) {
		t.Fatalf("expected file size %d, got %v", len(content), art.FileSizeBytes)
	}

	expectedObjectKey := *art.ObjectKey

	// Query from DB repo directly
	dbArt, err := env.ArtifactRepo.GetArtifactByID(ctx, art.ID)
	if err != nil {
		t.Fatalf("failed to get artifact from DB: %v", err)
	}
	if dbArt.Title != "Dataset CSV Export" || dbArt.StorageProvider != "mock" {
		t.Fatalf("mismatched DB artifact: %+v", dbArt)
	}

	// Query by object key
	keyArt, err := env.ArtifactRepo.GetArtifactByObjectKey(ctx, env.StorageService.Bucket(), expectedObjectKey)
	if err != nil {
		t.Fatalf("failed to query artifact by object key: %v", err)
	}
	if keyArt.ID != art.ID {
		t.Fatalf("expected artifact ID %s, got %s", art.ID, keyArt.ID)
	}

	// 4. Download Stream
	_, downOut, err := env.StorageService.DownloadArtifact(ctx, art.ID, userID)
	if err != nil {
		t.Fatalf("failed to download artifact stream: %v", err)
	}
	defer downOut.Body.Close()

	downloadedBytes, err := io.ReadAll(downOut.Body)
	if err != nil {
		t.Fatalf("failed reading download stream: %v", err)
	}
	if !bytes.Equal(downloadedBytes, content) {
		t.Fatalf("downloaded content mismatch")
	}

	// 5. Presigned URL
	presigned, err := env.StorageService.GetPresignedDownloadURL(ctx, art.ID, userID, 15*time.Minute)
	if err != nil {
		t.Fatalf("failed to get presigned URL: %v", err)
	}
	if presigned.URL == "" || !presigned.ExpiresAt.After(time.Now().UTC()) {
		t.Fatalf("invalid presigned response: %+v", presigned)
	}

	// 6. Delete Artifact
	if err := env.StorageService.DeleteArtifact(ctx, art.ID, userID); err != nil {
		t.Fatalf("failed to delete artifact: %v", err)
	}

	// Verify DB record is gone
	_, err = env.ArtifactRepo.GetArtifactByID(ctx, art.ID)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound from DB, got %v", err)
	}

	// Verify storage provider object is gone
	_, err = env.StorageProvider.Head(ctx, storage.HeadInput{
		Bucket: env.StorageService.Bucket(),
		Key:    expectedObjectKey,
	})
	if !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatalf("expected ErrObjectNotFound from storage provider, got %v", err)
	}
}

func TestStorageIntegration_TenantIsolation(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	cleanTables(t, env.DB)

	ctx := context.Background()
	userA := CreateTestUser(t, env.DB, "userA@example.com")
	userB := CreateTestUser(t, env.DB, "userB@example.com")

	art, err := env.StorageService.SaveArtifact(ctx, storage.SaveArtifactInput{
		UserID:       userA,
		Title:        "Confidential Strategy",
		Filename:     "strategy.pdf",
		ArtifactType: "PDF",
		Body:         bytes.NewReader([]byte("%PDF-confidential")),
	})
	if err != nil {
		t.Fatalf("userA save failed: %v", err)
	}

	// User B attempts to access User A's artifact
	_, _, err = env.StorageService.DownloadArtifact(ctx, art.ID, userB)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound when userB downloads userA artifact, got %v", err)
	}

	_, err = env.StorageService.GetPresignedDownloadURL(ctx, art.ID, userB, 10*time.Minute)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound when userB presigns userA artifact, got %v", err)
	}

	err = env.StorageService.DeleteArtifact(ctx, art.ID, userB)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound when userB deletes userA artifact, got %v", err)
	}

	// Verify userA still has the artifact
	dbArt, err := env.ArtifactRepo.GetArtifactByIDAndUser(ctx, art.ID, userA)
	if err != nil || dbArt == nil {
		t.Fatalf("userA artifact should still exist: %v", err)
	}
}

func TestStorageIntegration_HTTPAPI(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	cleanTables(t, env.DB)

	email := "api-storage-user@example.com"
	userID := CreateTestUser(t, env.DB, email)
	authHeader := GenerateTestJWT(userID, email)

	server := api.NewServer(api.ServerDependencies{
		Config:          env.Config,
		Logger:          nil,
		DB:              env.DB,
		Engine:          env.Engine,
		TaskRepo:        env.TaskRepo,
		ApprovalRepo:    env.ApprovalRepo,
		AgentRepo:       env.AgentRepo,
		ArtifactRepo:    env.ArtifactRepo,
		StorageService:  env.StorageService,
		WorkerDefRepo:   env.WorkerDefRepo,
		OrchRepo:        env.OrchRepo,
		Orchestrator:    env.Orchestrator,
		Factory:         env.AgentFactory,
		ManagerRegistry: env.ManagerReg,
	})

	handler := server.Handler()

	// 1. Multipart Form Upload: POST /api/v1/artifacts/upload
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("title", "HTTP Uploaded Log")
	_ = writer.WriteField("artifact_type", "FILE")

	fileContent := []byte("2026-11-02 12:00:00 [INFO] System started cleanly")
	part, err := writer.CreateFormFile("file", "system.log")
	if err != nil {
		t.Fatalf("failed creating form file: %v", err)
	}
	_, _ = part.Write(fileContent)
	_ = writer.Close()

	req := httptest.NewRequest("POST", "/api/v1/artifacts/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", authHeader)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created from upload, got %d: %s", rr.Code, rr.Body.String())
	}

	var uploadResp struct {
		Success bool            `json:"success"`
		Data    domain.Artifact `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&uploadResp); err != nil {
		t.Fatalf("failed parsing upload response: %v", err)
	}
	createdID := uploadResp.Data.ID
	if createdID == uuid.Nil {
		t.Fatalf("missing artifact ID in upload response")
	}

	// 2. Metadata: GET /api/v1/artifacts/{id}
	req = httptest.NewRequest("GET", fmt.Sprintf("/api/v1/artifacts/%s", createdID.String()), nil)
	req.Header.Set("Authorization", authHeader)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from get artifact, got %d: %s", rr.Code, rr.Body.String())
	}

	// 3. Download: GET /api/v1/artifacts/{id}/download
	req = httptest.NewRequest("GET", fmt.Sprintf("/api/v1/artifacts/%s/download", createdID.String()), nil)
	req.Header.Set("Authorization", authHeader)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from download, got %d: %s", rr.Code, rr.Body.String())
	}
	if !bytes.Equal(rr.Body.Bytes(), fileContent) {
		t.Fatalf("downloaded body mismatch: expected %q, got %q", fileContent, rr.Body.String())
	}

	// 4. Presign: GET /api/v1/artifacts/{id}/presign
	req = httptest.NewRequest("GET", fmt.Sprintf("/api/v1/artifacts/%s/presign?expires_in=20m", createdID.String()), nil)
	req.Header.Set("Authorization", authHeader)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from presign, got %d: %s", rr.Code, rr.Body.String())
	}

	var presignResp struct {
		Success bool                  `json:"success"`
		Data    storage.PresignOutput `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&presignResp); err != nil {
		t.Fatalf("failed decoding presign response: %v", err)
	}
	if presignResp.Data.URL == "" {
		t.Fatalf("missing presigned URL in response")
	}

	// 5. Delete: DELETE /api/v1/artifacts/{id}
	req = httptest.NewRequest("DELETE", fmt.Sprintf("/api/v1/artifacts/%s", createdID.String()), nil)
	req.Header.Set("Authorization", authHeader)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204 No Content from delete, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify 404 after delete
	req = httptest.NewRequest("GET", fmt.Sprintf("/api/v1/artifacts/%s", createdID.String()), nil)
	req.Header.Set("Authorization", authHeader)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found after delete, got %d", rr.Code)
	}
}

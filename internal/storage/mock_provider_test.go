package storage_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/griffin/core/internal/storage"
)

func TestMockStorageProvider_CRUD(t *testing.T) {
	ctx := context.Background()
	provider := storage.NewMockStorageProvider()

	if provider.Name() != "mock" {
		t.Fatalf("expected provider name 'mock', got '%s'", provider.Name())
	}

	bucket := "test-bucket"
	key := "path/to/test.txt"
	content := []byte("Hello, Griffin Storage!")

	// 1. Upload
	upOut, err := provider.Upload(ctx, storage.UploadInput{
		Bucket:      bucket,
		Key:         key,
		Body:        bytes.NewReader(content),
		ContentType: "text/plain",
		Size:        int64(len(content)),
	})
	if err != nil {
		t.Fatalf("unexpected upload error: %v", err)
	}
	if upOut.Bucket != bucket || upOut.Key != key {
		t.Fatalf("unexpected upload output: %+v", upOut)
	}
	if upOut.Size != int64(len(content)) {
		t.Fatalf("expected size %d, got %d", len(content), upOut.Size)
	}

	// 2. Head
	headOut, err := provider.Head(ctx, storage.HeadInput{
		Bucket: bucket,
		Key:    key,
	})
	if err != nil {
		t.Fatalf("unexpected head error: %v", err)
	}
	if headOut.Size != int64(len(content)) {
		t.Fatalf("expected size %d, got %d", len(content), headOut.Size)
	}
	if headOut.ContentType != "text/plain" {
		t.Fatalf("expected content type text/plain, got %s", headOut.ContentType)
	}

	// 3. Download
	downOut, err := provider.Download(ctx, storage.DownloadInput{
		Bucket: bucket,
		Key:    key,
	})
	if err != nil {
		t.Fatalf("unexpected download error: %v", err)
	}
	defer downOut.Body.Close()

	readData, err := io.ReadAll(downOut.Body)
	if err != nil {
		t.Fatalf("failed reading download body: %v", err)
	}
	if !bytes.Equal(readData, content) {
		t.Fatalf("content mismatch: expected %q, got %q", content, readData)
	}

	// 4. List
	listOut, err := provider.List(ctx, storage.ListInput{
		Bucket: bucket,
		Prefix: "path/",
	})
	if err != nil {
		t.Fatalf("unexpected list error: %v", err)
	}
	if len(listOut.Objects) != 1 {
		t.Fatalf("expected 1 object in list, got %d", len(listOut.Objects))
	}
	if listOut.Objects[0].Key != key {
		t.Fatalf("expected listed key %s, got %s", key, listOut.Objects[0].Key)
	}

	// 5. Presign
	presignOut, err := provider.PresignDownloadURL(ctx, storage.PresignInput{
		Bucket:    bucket,
		Key:       key,
		ExpiresIn: 10 * time.Minute,
	})
	if err != nil {
		t.Fatalf("unexpected presign error: %v", err)
	}
	if !strings.Contains(presignOut.URL, bucket) || !strings.Contains(presignOut.URL, key) {
		t.Fatalf("invalid presigned URL: %s", presignOut.URL)
	}

	// 6. Delete
	if err := provider.Delete(ctx, storage.DeleteInput{
		Bucket: bucket,
		Key:    key,
	}); err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}

	// 7. Verify Deleted
	_, err = provider.Download(ctx, storage.DownloadInput{
		Bucket: bucket,
		Key:    key,
	})
	if !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatalf("expected ErrObjectNotFound after delete, got %v", err)
	}
}

func TestMockStorageProvider_FailureSimulation(t *testing.T) {
	ctx := context.Background()
	provider := storage.NewMockStorageProvider()

	simErr := errors.New("simulated network failure")
	provider.FailUpload = simErr

	_, err := provider.Upload(ctx, storage.UploadInput{
		Bucket: "b",
		Key:    "k",
		Body:   bytes.NewReader([]byte("data")),
	})
	if !errors.Is(err, simErr) {
		t.Fatalf("expected simulated error, got %v", err)
	}
}

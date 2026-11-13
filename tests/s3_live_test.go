package tests

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/griffin/core/internal/storage"
)

// TestS3_LiveAWSVerification performs end-to-end live testing against AWS S3
// in ap-south-1 using real AWS credentials from griffin-bootstrap profile.
func TestS3_LiveAWSVerification(t *testing.T) {
	if os.Getenv("GRIFFIN_ENABLE_LIVE_S3_TEST") != "true" {
		t.Skip("skipping live AWS S3 test; set GRIFFIN_ENABLE_LIVE_S3_TEST=true to run")
	}

	bucket := os.Getenv("GRIFFIN_S3_BUCKET")
	if bucket == "" {
		t.Skip("set GRIFFIN_S3_BUCKET to run the live S3 test")
	}

	region := os.Getenv("GRIFFIN_S3_REGION")
	if region == "" {
		region = "ap-south-1"
	}

	profile := os.Getenv("AWS_PROFILE")
	if profile == "" {
		profile = "griffin-bootstrap"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. Initialize real S3 provider with AWS SDK v2
	provider, err := storage.NewS3StorageProvider(ctx, storage.S3ClientOptions{
		Region:     region,
		Bucket:     bucket,
		AWSProfile: profile,
	})
	if err != nil {
		t.Fatalf("failed to initialize live S3 provider: %v", err)
	}

	testPayload := []byte(fmt.Sprintf("Griffin Live AWS S3 Verification Payload - Timestamp: %s", time.Now().UTC().Format(time.RFC3339Nano)))
	hasher := sha256.New()
	hasher.Write(testPayload)
	expectedSHA256 := hex.EncodeToString(hasher.Sum(nil))

	objectKey := fmt.Sprintf("tasks/live-test-%d/outputs/verification-probe.txt", time.Now().UnixNano())

	// 2. Test Upload (PutObject)
	uploadRes, err := provider.Upload(ctx, storage.UploadInput{
		Bucket:      bucket,
		Key:         objectKey,
		Body:        bytes.NewReader(testPayload),
		Size:        int64(len(testPayload)),
		ContentType: "text/plain; charset=utf-8",
		Metadata: map[string]string{
			"griffin-test": "live-verification",
		},
	})
	if err != nil {
		t.Fatalf("live S3 Upload failed: %v", err)
	}
	if uploadRes.Key != objectKey {
		t.Errorf("expected key %q, got %q", objectKey, uploadRes.Key)
	}

	// Clean up after test
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = provider.Delete(cleanupCtx, storage.DeleteInput{
			Bucket: bucket,
			Key:    objectKey,
		})
	}()

	// 3. Test Head / Stat (HeadObject)
	info, err := provider.Head(ctx, storage.HeadInput{
		Bucket: bucket,
		Key:    objectKey,
	})
	if err != nil {
		t.Fatalf("live S3 Head failed: %v", err)
	}
	if info.Size != int64(len(testPayload)) {
		t.Errorf("expected size %d, got %d", len(testPayload), info.Size)
	}

	// 4. Test Download (GetObject)
	downloadRes, err := provider.Download(ctx, storage.DownloadInput{
		Bucket: bucket,
		Key:    objectKey,
	})
	if err != nil {
		t.Fatalf("live S3 Download failed: %v", err)
	}
	downloadedData, err := io.ReadAll(downloadRes.Body)
	_ = downloadRes.Body.Close()
	if err != nil {
		t.Fatalf("failed to read downloaded body: %v", err)
	}

	if !bytes.Equal(downloadedData, testPayload) {
		t.Fatalf("downloaded data mismatch: got %q, want %q", string(downloadedData), string(testPayload))
	}

	// Check SHA-256
	h2 := sha256.New()
	h2.Write(downloadedData)
	actualSHA256 := hex.EncodeToString(h2.Sum(nil))
	if actualSHA256 != expectedSHA256 {
		t.Errorf("SHA-256 checksum mismatch: got %s, want %s", actualSHA256, expectedSHA256)
	}

	// 5. Test Presigned Download URL
	presignRes, err := provider.PresignDownloadURL(ctx, storage.PresignInput{
		Bucket:    bucket,
		Key:       objectKey,
		ExpiresIn: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("live S3 PresignDownloadURL failed: %v", err)
	}
	if presignRes.URL == "" {
		t.Fatal("expected non-empty presigned URL")
	}

	// Verify presigned URL with real HTTP client
	httpResp, err := http.Get(presignRes.URL)
	if err != nil {
		t.Fatalf("HTTP GET to presigned S3 URL failed: %v", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		t.Fatalf("HTTP GET to presigned URL returned status %d, expected 200", httpResp.StatusCode)
	}
	httpData, err := io.ReadAll(httpResp.Body)
	if err != nil {
		t.Fatalf("failed reading presigned URL response: %v", err)
	}
	if !bytes.Equal(httpData, testPayload) {
		t.Fatalf("presigned URL downloaded data mismatch")
	}

	// 6. Test Delete
	if err := provider.Delete(ctx, storage.DeleteInput{
		Bucket: bucket,
		Key:    objectKey,
	}); err != nil {
		t.Fatalf("live S3 Delete failed: %v", err)
	}

	// 7. Verify Object is Gone
	_, err = provider.Head(ctx, storage.HeadInput{
		Bucket: bucket,
		Key:    objectKey,
	})
	if err == nil {
		t.Fatal("expected error after delete, got nil")
	}
	if !errors.Is(err, storage.ErrObjectNotFound) {
		t.Errorf("expected ErrObjectNotFound after delete, got %v", err)
	}

	t.Logf("LIVE S3 VERIFICATION SUCCESS: Successfully verified Upload, Download, Head, Presign URL, and Delete against AWS S3 (%s in %s)", bucket, region)
}

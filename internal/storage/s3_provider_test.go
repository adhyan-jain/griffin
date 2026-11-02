package storage_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"github.com/griffin/core/internal/storage"
)

type mockS3Client struct {
	getObjectFunc     func(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	deleteObjectFunc  func(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	headObjectFunc    func(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	listObjectsV2Func func(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	putObjectFunc     func(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

func (m *mockS3Client) GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	if m.getObjectFunc != nil {
		return m.getObjectFunc(ctx, params, optFns...)
	}
	return nil, errors.New("GetObject not implemented")
}

func (m *mockS3Client) DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	if m.deleteObjectFunc != nil {
		return m.deleteObjectFunc(ctx, params, optFns...)
	}
	return &s3.DeleteObjectOutput{}, nil
}

func (m *mockS3Client) HeadObject(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	if m.headObjectFunc != nil {
		return m.headObjectFunc(ctx, params, optFns...)
	}
	return nil, errors.New("HeadObject not implemented")
}

func (m *mockS3Client) ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	if m.listObjectsV2Func != nil {
		return m.listObjectsV2Func(ctx, params, optFns...)
	}
	return nil, errors.New("ListObjectsV2 not implemented")
}

func (m *mockS3Client) PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if m.putObjectFunc != nil {
		return m.putObjectFunc(ctx, params, optFns...)
	}
	return &s3.PutObjectOutput{}, nil
}

type mockS3Uploader struct {
	uploadFunc func(ctx context.Context, input *s3.PutObjectInput, options ...func(*manager.Uploader)) (*manager.UploadOutput, error)
}

func (m *mockS3Uploader) Upload(ctx context.Context, input *s3.PutObjectInput, options ...func(*manager.Uploader)) (*manager.UploadOutput, error) {
	if m.uploadFunc != nil {
		return m.uploadFunc(ctx, input, options...)
	}
	return &manager.UploadOutput{
		Location: "s3://" + *input.Bucket + "/" + *input.Key,
		ETag:     aws.String("\"test-etag\""),
	}, nil
}

type mockS3Presigner struct {
	presignGetObjectFunc func(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error)
}

func (m *mockS3Presigner) PresignGetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error) {
	if m.presignGetObjectFunc != nil {
		return m.presignGetObjectFunc(ctx, params, optFns...)
	}
	return &v4.PresignedHTTPRequest{
		URL:    "https://" + *params.Bucket + ".s3.amazonaws.com/" + *params.Key + "?signed=true",
		Method: "GET",
	}, nil
}

type genericAPIError struct {
	code    string
	message string
}

func (e *genericAPIError) Error() string                 { return e.code + ": " + e.message }
func (e *genericAPIError) ErrorCode() string             { return e.code }
func (e *genericAPIError) ErrorMessage() string          { return e.message }
func (e *genericAPIError) ErrorFault() smithy.ErrorFault { return smithy.FaultClient }

func TestS3StorageProvider_Upload(t *testing.T) {
	ctx := context.Background()
	bucket := "griffin-artifacts"
	key := "tasks/123/outputs/result.json"

	var capturedBucket, capturedKey, capturedContentType string
	uploader := &mockS3Uploader{
		uploadFunc: func(ctx context.Context, input *s3.PutObjectInput, options ...func(*manager.Uploader)) (*manager.UploadOutput, error) {
			capturedBucket = *input.Bucket
			capturedKey = *input.Key
			if input.ContentType != nil {
				capturedContentType = *input.ContentType
			}
			return &manager.UploadOutput{
				Location: "s3://" + *input.Bucket + "/" + *input.Key,
				ETag:     aws.String("\"etag-12345\""),
			}, nil
		},
	}

	p := storage.NewS3StorageProviderWithAPIs(
		&mockS3Client{},
		uploader,
		&mockS3Presigner{},
		bucket,
		"ap-south-1",
	)

	if p.Name() != "s3" {
		t.Fatalf("expected name 's3', got %s", p.Name())
	}

	out, err := p.Upload(ctx, storage.UploadInput{
		Bucket:      bucket,
		Key:         key,
		Body:        bytes.NewReader([]byte(`{"status":"ok"}`)),
		ContentType: "application/json",
		Size:        15,
	})
	if err != nil {
		t.Fatalf("unexpected upload error: %v", err)
	}

	if capturedBucket != bucket || capturedKey != key {
		t.Fatalf("expected bucket/key %s/%s, got %s/%s", bucket, key, capturedBucket, capturedKey)
	}
	if capturedContentType != "application/json" {
		t.Fatalf("expected content type application/json, got %s", capturedContentType)
	}
	if out.URI != "s3://"+bucket+"/"+key {
		t.Fatalf("expected URI s3://%s/%s, got %s", bucket, key, out.URI)
	}
	if out.ETag != "\"etag-12345\"" {
		t.Fatalf("expected etag \"etag-12345\", got %s", out.ETag)
	}
}

func TestS3StorageProvider_Download(t *testing.T) {
	ctx := context.Background()
	bucket := "griffin-artifacts"
	key := "tasks/123/outputs/result.json"
	payload := []byte("downloadable content")

	client := &mockS3Client{
		getObjectFunc: func(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
			if *params.Key == "missing.txt" {
				return nil, &types.NoSuchKey{Message: aws.String("key not found")}
			}
			return &s3.GetObjectOutput{
				Body:          io.NopCloser(bytes.NewReader(payload)),
				ContentLength: aws.Int64(int64(len(payload))),
				ContentType:   aws.String("text/plain"),
				ETag:          aws.String("\"download-etag\""),
				LastModified:  aws.Time(time.Now().UTC()),
			}, nil
		},
	}

	p := storage.NewS3StorageProviderWithAPIs(client, &mockS3Uploader{}, &mockS3Presigner{}, bucket, "ap-south-1")

	// Success case
	downOut, err := p.Download(ctx, storage.DownloadInput{Bucket: bucket, Key: key})
	if err != nil {
		t.Fatalf("unexpected download error: %v", err)
	}
	defer downOut.Body.Close()

	data, err := io.ReadAll(downOut.Body)
	if err != nil {
		t.Fatalf("failed reading download stream: %v", err)
	}
	if !bytes.Equal(data, payload) {
		t.Fatalf("payload mismatch")
	}

	// Not Found case
	_, err = p.Download(ctx, storage.DownloadInput{Bucket: bucket, Key: "missing.txt"})
	if !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatalf("expected ErrObjectNotFound, got %v", err)
	}
}

func TestS3StorageProvider_Head(t *testing.T) {
	ctx := context.Background()
	bucket := "griffin-artifacts"

	client := &mockS3Client{
		headObjectFunc: func(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
			if *params.Key == "missing" {
				return nil, &types.NotFound{Message: aws.String("not found")}
			}
			if *params.Key == "forbidden" {
				return nil, &genericAPIError{code: "AccessDenied", message: "forbidden"}
			}
			return &s3.HeadObjectOutput{
				ContentLength: aws.Int64(42),
				ContentType:   aws.String("application/json"),
				ETag:          aws.String("\"head-etag\""),
				LastModified:  aws.Time(time.Now().UTC()),
			}, nil
		},
	}

	p := storage.NewS3StorageProviderWithAPIs(client, &mockS3Uploader{}, &mockS3Presigner{}, bucket, "ap-south-1")

	info, err := p.Head(ctx, storage.HeadInput{Bucket: bucket, Key: "exists"})
	if err != nil {
		t.Fatalf("unexpected head error: %v", err)
	}
	if info.Size != 42 || info.ContentType != "application/json" {
		t.Fatalf("unexpected object info: %+v", info)
	}

	_, err = p.Head(ctx, storage.HeadInput{Bucket: bucket, Key: "missing"})
	if !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatalf("expected ErrObjectNotFound, got %v", err)
	}

	_, err = p.Head(ctx, storage.HeadInput{Bucket: bucket, Key: "forbidden"})
	if !errors.Is(err, storage.ErrPermissionDenied) {
		t.Fatalf("expected ErrPermissionDenied, got %v", err)
	}
}

func TestS3StorageProvider_Presign(t *testing.T) {
	ctx := context.Background()
	bucket := "griffin-artifacts"
	key := "tasks/123/outputs/report.pdf"

	presigner := &mockS3Presigner{
		presignGetObjectFunc: func(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error) {
			return &v4.PresignedHTTPRequest{
				URL:    "https://griffin-artifacts.s3.ap-south-1.amazonaws.com/" + *params.Key + "?X-Amz-Signature=test",
				Method: "GET",
			}, nil
		},
	}

	p := storage.NewS3StorageProviderWithAPIs(&mockS3Client{}, &mockS3Uploader{}, presigner, bucket, "ap-south-1")

	out, err := p.PresignDownloadURL(ctx, storage.PresignInput{
		Bucket:    bucket,
		Key:       key,
		ExpiresIn: 20 * time.Minute,
	})
	if err != nil {
		t.Fatalf("unexpected presign error: %v", err)
	}
	if out.URL == "" || !out.ExpiresAt.After(time.Now().UTC()) {
		t.Fatalf("invalid presign output: %+v", out)
	}
}

func TestS3StorageProvider_ErrorMapping(t *testing.T) {
	ctx := context.Background()
	bucket := "test-bucket"

	tests := []struct {
		name        string
		s3Err       error
		expectedErr error
	}{
		{
			name:        "NoSuchBucket error",
			s3Err:       &types.NoSuchBucket{Message: aws.String("bucket missing")},
			expectedErr: storage.ErrBucketNotFound,
		},
		{
			name:        "AccessDenied error",
			s3Err:       &genericAPIError{code: "AccessDenied", message: "access denied"},
			expectedErr: storage.ErrPermissionDenied,
		},
		{
			name:        "HTTP 404 response error",
			s3Err:       &awshttp.ResponseError{ResponseError: &smithyhttp.ResponseError{Response: &smithyhttp.Response{Response: &http.Response{StatusCode: 404}}}},
			expectedErr: storage.ErrObjectNotFound,
		},
		{
			name:        "HTTP 403 response error",
			s3Err:       &awshttp.ResponseError{ResponseError: &smithyhttp.ResponseError{Response: &smithyhttp.Response{Response: &http.Response{StatusCode: 403}}}},
			expectedErr: storage.ErrPermissionDenied,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := &mockS3Client{
				headObjectFunc: func(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
					return nil, tc.s3Err
				},
			}
			p := storage.NewS3StorageProviderWithAPIs(client, &mockS3Uploader{}, &mockS3Presigner{}, bucket, "ap-south-1")
			_, err := p.Head(ctx, storage.HeadInput{Bucket: bucket, Key: "any"})
			if !errors.Is(err, tc.expectedErr) {
				t.Fatalf("expected error %v, got %v", tc.expectedErr, err)
			}
		})
	}
}

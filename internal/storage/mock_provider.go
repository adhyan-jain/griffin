package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

type mockStoredObject struct {
	data         []byte
	contentType  string
	metadata     map[string]string
	lastModified time.Time
	etag         string
}

// MockStorageProvider provides a thread-safe, in-memory implementation of Provider.
type MockStorageProvider struct {
	mu      sync.RWMutex
	objects map[string]*mockStoredObject // key: "bucket/key"

	// Failure simulation hooks for testing
	FailUpload   error
	FailDownload error
	FailDelete   error
	FailHead     error
	FailList     error
	FailPresign  error
}

// NewMockStorageProvider initializes an empty MockStorageProvider.
func NewMockStorageProvider() *MockStorageProvider {
	return &MockStorageProvider{
		objects: make(map[string]*mockStoredObject),
	}
}

func (m *MockStorageProvider) objKey(bucket, key string) string {
	return fmt.Sprintf("%s/%s", bucket, strings.TrimPrefix(key, "/"))
}

func (m *MockStorageProvider) Name() string {
	return "mock"
}

func (m *MockStorageProvider) Upload(ctx context.Context, input UploadInput) (*UploadOutput, error) {
	if m.FailUpload != nil {
		return nil, m.FailUpload
	}
	if input.Bucket == "" {
		return nil, ErrBucketNotFound
	}
	if strings.TrimSpace(input.Key) == "" {
		return nil, ErrInvalidKey
	}

	data, err := io.ReadAll(input.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUploadFailed, err)
	}

	etag := fmt.Sprintf("\"mock-etag-%d\"", time.Now().UnixNano())
	ct := input.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}

	metaCopy := make(map[string]string)
	for k, v := range input.Metadata {
		metaCopy[k] = v
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.objects[m.objKey(input.Bucket, input.Key)] = &mockStoredObject{
		data:         data,
		contentType:  ct,
		metadata:     metaCopy,
		lastModified: time.Now().UTC(),
		etag:         etag,
	}

	return &UploadOutput{
		Bucket: input.Bucket,
		Key:    input.Key,
		URI:    fmt.Sprintf("s3://%s/%s", input.Bucket, input.Key),
		ETag:   etag,
		Size:   int64(len(data)),
	}, nil
}

func (m *MockStorageProvider) Download(ctx context.Context, input DownloadInput) (*DownloadOutput, error) {
	if m.FailDownload != nil {
		return nil, m.FailDownload
	}
	if input.Bucket == "" {
		return nil, ErrBucketNotFound
	}
	if strings.TrimSpace(input.Key) == "" {
		return nil, ErrInvalidKey
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	obj, ok := m.objects[m.objKey(input.Bucket, input.Key)]
	if !ok {
		return nil, ErrObjectNotFound
	}

	metaCopy := make(map[string]string)
	for k, v := range obj.metadata {
		metaCopy[k] = v
	}

	return &DownloadOutput{
		Body:          io.NopCloser(bytes.NewReader(obj.data)),
		ContentType:   obj.contentType,
		ContentLength: int64(len(obj.data)),
		ETag:          obj.etag,
		LastModified:  obj.lastModified,
		Metadata:      metaCopy,
	}, nil
}

func (m *MockStorageProvider) Delete(ctx context.Context, input DeleteInput) error {
	if m.FailDelete != nil {
		return m.FailDelete
	}
	if input.Bucket == "" {
		return ErrBucketNotFound
	}
	if strings.TrimSpace(input.Key) == "" {
		return ErrInvalidKey
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.objects, m.objKey(input.Bucket, input.Key))
	return nil
}

func (m *MockStorageProvider) Head(ctx context.Context, input HeadInput) (*ObjectInfo, error) {
	if m.FailHead != nil {
		return nil, m.FailHead
	}
	if input.Bucket == "" {
		return nil, ErrBucketNotFound
	}
	if strings.TrimSpace(input.Key) == "" {
		return nil, ErrInvalidKey
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	obj, ok := m.objects[m.objKey(input.Bucket, input.Key)]
	if !ok {
		return nil, ErrObjectNotFound
	}

	metaCopy := make(map[string]string)
	for k, v := range obj.metadata {
		metaCopy[k] = v
	}

	return &ObjectInfo{
		Bucket:       input.Bucket,
		Key:          input.Key,
		Size:         int64(len(obj.data)),
		ETag:         obj.etag,
		ContentType:  obj.contentType,
		LastModified: obj.lastModified,
		Metadata:     metaCopy,
	}, nil
}

func (m *MockStorageProvider) List(ctx context.Context, input ListInput) (*ListOutput, error) {
	if m.FailList != nil {
		return nil, m.FailList
	}
	if input.Bucket == "" {
		return nil, ErrBucketNotFound
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []ObjectInfo
	prefixMatch := input.Bucket + "/" + input.Prefix
	for compoundKey, obj := range m.objects {
		if strings.HasPrefix(compoundKey, prefixMatch) {
			rawKey := strings.TrimPrefix(compoundKey, input.Bucket+"/")
			metaCopy := make(map[string]string)
			for k, v := range obj.metadata {
				metaCopy[k] = v
			}
			result = append(result, ObjectInfo{
				Bucket:       input.Bucket,
				Key:          rawKey,
				Size:         int64(len(obj.data)),
				ETag:         obj.etag,
				ContentType:  obj.contentType,
				LastModified: obj.lastModified,
				Metadata:     metaCopy,
			})
			if input.MaxKeys > 0 && int32(len(result)) >= input.MaxKeys {
				break
			}
		}
	}

	return &ListOutput{
		Objects:     result,
		IsTruncated: false,
	}, nil
}

func (m *MockStorageProvider) PresignDownloadURL(ctx context.Context, input PresignInput) (*PresignOutput, error) {
	if m.FailPresign != nil {
		return nil, m.FailPresign
	}
	if input.Bucket == "" {
		return nil, ErrBucketNotFound
	}
	if strings.TrimSpace(input.Key) == "" {
		return nil, ErrInvalidKey
	}

	expiry := input.ExpiresIn
	if expiry <= 0 {
		expiry = 15 * time.Minute
	}
	expiresAt := time.Now().UTC().Add(expiry)
	url := fmt.Sprintf("https://mock-s3.local/%s/%s?X-Amz-Expires=%d&X-Amz-Signature=mock-signature",
		input.Bucket, input.Key, int(expiry.Seconds()))

	return &PresignOutput{
		URL:       url,
		ExpiresAt: expiresAt,
	}, nil
}

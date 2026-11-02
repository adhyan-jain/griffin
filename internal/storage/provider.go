package storage

import (
	"context"
	"io"
	"time"
)

// ObjectInfo contains metadata for an object in storage.
type ObjectInfo struct {
	Bucket       string            `json:"bucket"`
	Key          string            `json:"key"`
	Size         int64             `json:"size"`
	ETag         string            `json:"etag,omitempty"`
	ContentType  string            `json:"content_type,omitempty"`
	LastModified time.Time         `json:"last_modified"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// UploadInput defines parameters for uploading an object.
type UploadInput struct {
	Bucket      string
	Key         string
	Body        io.Reader
	ContentType string
	Metadata    map[string]string
	Size        int64 // Optional, set <= 0 if unknown
}

// UploadOutput contains the result of an upload operation.
type UploadOutput struct {
	Bucket    string `json:"bucket"`
	Key       string `json:"key"`
	URI       string `json:"uri"`
	ETag      string `json:"etag,omitempty"`
	Size      int64  `json:"size"`
	VersionID string `json:"version_id,omitempty"`
}

// DownloadInput defines parameters for downloading an object.
type DownloadInput struct {
	Bucket string
	Key    string
}

// DownloadOutput contains the object content stream and metadata.
// Caller is responsible for closing Body.
type DownloadOutput struct {
	Body          io.ReadCloser
	ContentType   string
	ContentLength int64
	ETag          string
	LastModified  time.Time
	Metadata      map[string]string
}

// DeleteInput defines parameters for deleting an object.
type DeleteInput struct {
	Bucket string
	Key    string
}

// HeadInput defines parameters for checking an object's metadata.
type HeadInput struct {
	Bucket string
	Key    string
}

// ListInput defines parameters for listing objects in a bucket/prefix.
type ListInput struct {
	Bucket    string
	Prefix    string
	Delimiter string
	MaxKeys   int32
}

// ListOutput contains listed objects and pagination metadata.
type ListOutput struct {
	Objects     []ObjectInfo `json:"objects"`
	IsTruncated bool         `json:"is_truncated"`
	NextToken   string       `json:"next_token,omitempty"`
}

// PresignInput defines parameters for generating a presigned URL.
type PresignInput struct {
	Bucket    string
	Key       string
	ExpiresIn time.Duration
}

// PresignOutput contains the generated presigned URL and expiry.
type PresignOutput struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Provider defines the neutral storage contract for Griffin object storage backends.
type Provider interface {
	Upload(ctx context.Context, input UploadInput) (*UploadOutput, error)
	Download(ctx context.Context, input DownloadInput) (*DownloadOutput, error)
	Delete(ctx context.Context, input DeleteInput) error
	Head(ctx context.Context, input HeadInput) (*ObjectInfo, error)
	List(ctx context.Context, input ListInput) (*ListOutput, error)
	PresignDownloadURL(ctx context.Context, input PresignInput) (*PresignOutput, error)
	Name() string
}

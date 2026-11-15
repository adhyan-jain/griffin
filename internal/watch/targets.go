package watch

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/griffin/core/internal/storage"
)

const (
	DefaultS3Key = "oss/targets.json"
)

// DefaultTargets is a curated list of top Go and Python repositories when no targets file exists.
var DefaultTargets = []TargetRepo{
	{Repo: "golang/go", Language: "Go"},
	{Repo: "kubernetes/kubernetes", Language: "Go"},
	{Repo: "gin-gonic/gin", Language: "Go"},
	{Repo: "spf13/cobra", Language: "Go"},
	{Repo: "tiangolo/fastapi", Language: "Python"},
	{Repo: "psf/requests", Language: "Python"},
	{Repo: "pallets/flask", Language: "Python"},
	{Repo: "django/django", Language: "Python"},
}

// DefaultLocalTargetsPath returns ~/.griffin-host/oss/targets.json
func DefaultLocalTargetsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "griffin-host", "oss", "targets.json")
	}
	return filepath.Join(home, ".griffin-host", "oss", "targets.json")
}

// LoadTargetsFromFile reads targets from a local file path.
func LoadTargetsFromFile(path string) ([]TargetRepo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseTargets(data)
}

// SaveTargetsToFile writes targets JSON to a local file.
func SaveTargetsToFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}
	return os.WriteFile(path, data, 0o600)
}

// LoadTargetsFromS3 loads targets JSON from an S3 bucket and key.
func LoadTargetsFromS3(ctx context.Context, bucket, key string, opts storage.S3ClientOptions) ([]TargetRepo, error) {
	cfg, err := storage.LoadAWSConfig(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	client := s3.NewFromConfig(cfg)
	resp, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("s3 get %s/%s: %w", bucket, key, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if err != nil {
		return nil, fmt.Errorf("read s3 body: %w", err)
	}
	return ParseTargets(data)
}

// UploadTargetsToS3 uploads targets JSON to S3.
func UploadTargetsToS3(ctx context.Context, bucket, key string, data []byte, opts storage.S3ClientOptions) error {
	cfg, err := storage.LoadAWSConfig(ctx, opts)
	if err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}

	client := s3.NewFromConfig(cfg)
	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return fmt.Errorf("s3 put %s/%s: %w", bucket, key, err)
	}
	return nil
}

// ResolveTargets resolves target repositories from S3 if configured, or from local file,
// or defaults if neither exists.
func ResolveTargets(ctx context.Context, bucket, key, localPath string, s3Opts storage.S3ClientOptions) ([]TargetRepo, string, error) {
	// 1. Try S3 if bucket is configured
	if bucket != "" {
		if key == "" {
			key = DefaultS3Key
		}
		targets, err := LoadTargetsFromS3(ctx, bucket, key, s3Opts)
		if err == nil && len(targets) > 0 {
			return targets, fmt.Sprintf("s3://%s/%s", bucket, key), nil
		}
	}

	// 2. Try local file
	if localPath == "" {
		localPath = DefaultLocalTargetsPath()
	}
	targets, err := LoadTargetsFromFile(localPath)
	if err == nil && len(targets) > 0 {
		return targets, localPath, nil
	}

	// 3. Fallback to default curated list
	return DefaultTargets, "built-in curated defaults", nil
}

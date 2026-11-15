package watch

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/griffin/core/internal/storage"
)

// Default paths for dual-copy storage
const (
	DefaultS3Bucket = "griffin-artifacts-220115213279-ap-south-1"
	DefaultS3Prefix = "documents"
	DefaultProfile  = "griffin-bootstrap"
	DefaultRegion   = "ap-south-1"
)

// DualSyncConfig configures the dual-storage sync engine.
type DualSyncConfig struct {
	LocalRoot  string // /home/adhyan/Documents/Griffin
	S3Bucket   string
	S3Prefix   string
	AWSProfile string
	AWSRegion  string
	Logger     *slog.Logger
}

// DualStorageSync maintains 2 identical copies of all pipeline files:
// 1 on the laptop in /home/adhyan/Documents/Griffin/
// 1 on AWS S3 in s3://griffin-artifacts-220115213279-ap-south-1/documents/
type DualStorageSync struct {
	cfg    DualSyncConfig
	logger *slog.Logger
	s3Opts storage.S3ClientOptions
}

// NewDualStorageSync creates a DualStorageSync engine.
func NewDualStorageSync(cfg DualSyncConfig) *DualStorageSync {
	if cfg.LocalRoot == "" {
		home, _ := os.UserHomeDir()
		cfg.LocalRoot = filepath.Join(home, "Documents", "Griffin")
	}
	if cfg.S3Bucket == "" {
		cfg.S3Bucket = DefaultS3Bucket
	}
	if cfg.S3Prefix == "" {
		cfg.S3Prefix = DefaultS3Prefix
	}
	if cfg.AWSProfile == "" {
		cfg.AWSProfile = DefaultProfile
	}
	if cfg.AWSRegion == "" {
		cfg.AWSRegion = DefaultRegion
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	_ = os.MkdirAll(cfg.LocalRoot, 0o700)

	opts := storage.S3ClientOptions{
		Bucket:     cfg.S3Bucket,
		AWSProfile: cfg.AWSProfile,
		Region:     cfg.AWSRegion,
	}

	return &DualStorageSync{
		cfg:    cfg,
		logger: cfg.Logger,
		s3Opts: opts,
	}
}

// LocalPath returns the absolute local path for a relative pipeline path.
func (d *DualStorageSync) LocalPath(relPath string) string {
	return filepath.Join(d.cfg.LocalRoot, filepath.Clean(relPath))
}

// S3Key returns the full S3 key for a relative pipeline path.
func (d *DualStorageSync) S3Key(relPath string) string {
	clean := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(relPath)), "/")
	return fmt.Sprintf("%s/%s", strings.TrimSuffix(d.cfg.S3Prefix, "/"), clean)
}

// SaveDual writes data to the local laptop path and uploads to S3 simultaneously.
func (d *DualStorageSync) SaveDual(ctx context.Context, relPath string, data []byte) error {
	local := d.LocalPath(relPath)
	if err := os.MkdirAll(filepath.Dir(local), 0o700); err != nil {
		return fmt.Errorf("create local dir: %w", err)
	}
	if err := os.WriteFile(local, data, 0o644); err != nil {
		return fmt.Errorf("write local file %s: %w", local, err)
	}

	// S3 upload
	s3Key := d.S3Key(relPath)
	err := d.uploadS3(ctx, s3Key, data)
	if err != nil {
		d.logger.Warn("dual-sync: s3 upload failed (will reconcile on next online sync)", "key", s3Key, "error", err)
		return nil // Return nil so local pipeline continues if network is transiently down
	}

	d.logger.Info("dual-sync saved 2 copies", "local", local, "s3", fmt.Sprintf("s3://%s/%s", d.cfg.S3Bucket, s3Key))
	return nil
}

// ReadDual reads data from the local copy; if missing locally, fetches from S3 and caches locally.
func (d *DualStorageSync) ReadDual(ctx context.Context, relPath string) ([]byte, error) {
	local := d.LocalPath(relPath)
	data, err := os.ReadFile(local)
	if err == nil {
		return data, nil
	}

	// Missing locally: attempt fetch from S3
	s3Key := d.S3Key(relPath)
	s3Data, s3Err := d.downloadS3(ctx, s3Key)
	if s3Err != nil {
		return nil, fmt.Errorf("local file missing (%w) and s3 get failed (%v)", err, s3Err)
	}

	// Restore local copy
	_ = os.MkdirAll(filepath.Dir(local), 0o700)
	_ = os.WriteFile(local, s3Data, 0o644)
	d.logger.Info("dual-sync restored local file from s3", "local", local, "s3", s3Key)
	return s3Data, nil
}

// ReconcileAll runs on startup / reconnect to synchronize between S3 and the local folder.
func (d *DualStorageSync) ReconcileAll(ctx context.Context) error {
	d.logger.Info("dual-sync: starting startup reconciliation between laptop and s3", "local_root", d.cfg.LocalRoot, "bucket", d.cfg.S3Bucket)

	awsCfg, err := storage.LoadAWSConfig(ctx, d.s3Opts)
	if err != nil {
		d.logger.Warn("dual-sync reconcile: aws credentials not available (offline)", "error", err)
		return nil
	}

	client := s3.NewFromConfig(awsCfg)

	// 1. List all remote objects under S3Prefix
	prefix := strings.TrimSuffix(d.cfg.S3Prefix, "/") + "/"
	paginator := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{
		Bucket: aws.String(d.cfg.S3Bucket),
		Prefix: aws.String(prefix),
	})

	remoteKeys := make(map[string]int64)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			d.logger.Warn("dual-sync: list s3 objects failed", "error", err)
			break
		}
		for _, obj := range page.Contents {
			if obj.Key != nil && obj.Size != nil && *obj.Size > 0 {
				remoteKeys[*obj.Key] = *obj.Size
			}
		}
	}

	// Download any remote files missing locally
	for key := range remoteKeys {
		rel := strings.TrimPrefix(key, prefix)
		localFile := d.LocalPath(rel)
		if _, err := os.Stat(localFile); os.IsNotExist(err) {
			d.logger.Info("dual-sync: downloading missing local file from s3", "key", key, "local", localFile)
			if data, err := d.downloadS3(ctx, key); err == nil {
				_ = os.MkdirAll(filepath.Dir(localFile), 0o700)
				_ = os.WriteFile(localFile, data, 0o644)
			}
		}
	}

	// 2. Scan local files and upload any missing or modified files to S3
	_ = filepath.Walk(d.cfg.LocalRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(d.cfg.LocalRoot, path)
		if err != nil {
			return nil
		}
		s3Key := d.S3Key(rel)
		remSize, exists := remoteKeys[s3Key]
		if !exists || remSize != info.Size() {
			data, err := os.ReadFile(path)
			if err == nil {
				d.logger.Info("dual-sync: uploading local file to s3", "local", path, "key", s3Key)
				_ = d.uploadS3(ctx, s3Key, data)
			}
		}
		return nil
	})

	d.logger.Info("dual-sync: startup reconciliation completed successfully")
	return nil
}

func (d *DualStorageSync) uploadS3(ctx context.Context, key string, data []byte) error {
	awsCfg, err := storage.LoadAWSConfig(ctx, d.s3Opts)
	if err != nil {
		return err
	}
	client := s3.NewFromConfig(awsCfg)

	ctype := mime.TypeByExtension(filepath.Ext(key))
	if ctype == "" {
		if strings.HasSuffix(key, ".json") || strings.HasSuffix(key, ".jsonl") {
			ctype = "application/json"
		} else if strings.HasSuffix(key, ".md") || strings.HasSuffix(key, ".txt") {
			ctype = "text/plain; charset=utf-8"
		} else if strings.HasSuffix(key, ".pdf") {
			ctype = "application/pdf"
		} else {
			ctype = "application/octet-stream"
		}
	}

	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(d.cfg.S3Bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(ctype),
	})
	return err
}

func (d *DualStorageSync) downloadS3(ctx context.Context, key string) ([]byte, error) {
	awsCfg, err := storage.LoadAWSConfig(ctx, d.s3Opts)
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(awsCfg)

	resp, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(d.cfg.S3Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(io.LimitReader(resp.Body, 50<<20))
}

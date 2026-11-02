package config_test

import (
	"os"
	"testing"

	"github.com/griffin/core/internal/config"
)

func TestConfigLoadDefaults(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected valid default config, got error: %v", err)
	}

	if cfg.Port != 8080 {
		t.Errorf("expected port 8080, got %d", cfg.Port)
	}
	if cfg.MaxConcurrentWorkers != 2 {
		t.Errorf("expected max workers 2, got %d", cfg.MaxConcurrentWorkers)
	}
	if cfg.LeaseDurationSeconds != 60 {
		t.Errorf("expected lease duration 60, got %d", cfg.LeaseDurationSeconds)
	}
}

func TestConfigOverrideEnv(t *testing.T) {
	os.Setenv("PORT", "9090")
	os.Setenv("MAX_CONCURRENT_WORKERS", "4")
	defer func() {
		os.Unsetenv("PORT")
		os.Unsetenv("MAX_CONCURRENT_WORKERS")
	}()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}

	if cfg.Port != 9090 {
		t.Errorf("expected port 9090, got %d", cfg.Port)
	}
	if cfg.MaxConcurrentWorkers != 4 {
		t.Errorf("expected max workers 4, got %d", cfg.MaxConcurrentWorkers)
	}
}

func TestConfigStorageSettings(t *testing.T) {
	// Defaults
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("expected valid default config, got %v", err)
	}
	if cfg.StorageProvider != "s3" {
		t.Errorf("expected default storage provider s3, got %s", cfg.StorageProvider)
	}
	if cfg.S3Bucket != "griffin-artifacts" {
		t.Errorf("expected default bucket griffin-artifacts, got %s", cfg.S3Bucket)
	}
	if cfg.S3Region != "ap-south-1" {
		t.Errorf("expected default region ap-south-1, got %s", cfg.S3Region)
	}
	if cfg.S3PresignExpiryMinutes != 15 {
		t.Errorf("expected default presign expiry 15, got %d", cfg.S3PresignExpiryMinutes)
	}

	// Overrides
	os.Setenv("STORAGE_PROVIDER", "mock")
	os.Setenv("S3_BUCKET", "custom-bucket")
	os.Setenv("S3_REGION", "us-east-1")
	os.Setenv("S3_PRESIGN_EXPIRY_MINUTES", "30")
	defer func() {
		os.Unsetenv("STORAGE_PROVIDER")
		os.Unsetenv("S3_BUCKET")
		os.Unsetenv("S3_REGION")
		os.Unsetenv("S3_PRESIGN_EXPIRY_MINUTES")
	}()

	cfgOverride, err := config.Load()
	if err != nil {
		t.Fatalf("expected valid override config, got %v", err)
	}
	if cfgOverride.StorageProvider != "mock" {
		t.Errorf("expected storage provider mock, got %s", cfgOverride.StorageProvider)
	}
	if cfgOverride.S3Bucket != "custom-bucket" {
		t.Errorf("expected bucket custom-bucket, got %s", cfgOverride.S3Bucket)
	}
	if cfgOverride.S3Region != "us-east-1" {
		t.Errorf("expected region us-east-1, got %s", cfgOverride.S3Region)
	}
	if cfgOverride.S3PresignExpiryMinutes != 30 {
		t.Errorf("expected presign expiry 30, got %d", cfgOverride.S3PresignExpiryMinutes)
	}
}

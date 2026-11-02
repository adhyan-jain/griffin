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

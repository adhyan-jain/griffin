package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateAPIURL(t *testing.T) {
	ok := []string{"https://api.example.com", "http://localhost:8080", "http://127.0.0.1:8080"}
	bad := []string{"", "http://api.example.com", "ftp://x", "not a url", "https://"}
	for _, u := range ok {
		if err := validateAPIURL(u); err != nil {
			t.Errorf("%q should be accepted: %v", u, err)
		}
	}
	for _, u := range bad {
		if err := validateAPIURL(u); err == nil {
			t.Errorf("%q must be refused: the token is a bearer secret", u)
		}
	}
}

func TestParseCapabilitiesRefusesWhatThisBuildCannotBack(t *testing.T) {
	if caps, err := parseCapabilities("agent", false); err != nil || len(caps) != 1 {
		t.Fatalf("agent must be accepted: %v %v", caps, err)
	}
	for _, raw := range []string{"agent,browser", "shell", "phone_apps"} {
		if _, err := parseCapabilities(raw, false); err == nil || !strings.Contains(err.Error(), "no implementation") {
			t.Errorf("%q must be refused (a false completion otherwise), got %v", raw, err)
		}
	}
	if _, err := parseCapabilities("telepathy", false); err == nil {
		t.Error("unknown capabilities must be refused")
	}
	if _, err := parseCapabilities(" , ", false); err == nil {
		t.Error("empty must be refused")
	}
	if caps, err := parseCapabilities("agent,browser", true); err != nil || len(caps) != 2 {
		t.Errorf("the mock executor may advertise anything for development: %v %v", caps, err)
	}
}

func TestLoadTokenPrefersTheFileAndNeverReturnsEmpty(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	dir := t.TempDir()
	f := filepath.Join(dir, "token")
	if err := os.WriteFile(f, []byte("ghk_from_file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GRIFFIN_HOST_TOKEN_FILE", f)
	t.Setenv("GRIFFIN_HOST_TOKEN", "ghk_from_env")
	if got, err := loadToken(logger); err != nil || got != "ghk_from_file" {
		t.Fatalf("file must win and be trimmed, got %q %v", got, err)
	}
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(f, home) {
		rel, _ := filepath.Rel(home, f)
		t.Setenv("GRIFFIN_HOST_TOKEN_FILE", "~/"+rel)
		if got, err := loadToken(logger); err != nil || got != "ghk_from_file" {
			t.Fatalf("tilde expansion must work, got %q %v", got, err)
		}
	}
	empty := filepath.Join(dir, "empty")
	_ = os.WriteFile(empty, []byte("  \n"), 0o600)
	t.Setenv("GRIFFIN_HOST_TOKEN_FILE", empty)
	if _, err := loadToken(logger); err == nil {
		t.Fatal("an empty token file must be an error")
	}
	t.Setenv("GRIFFIN_HOST_TOKEN_FILE", "")
	if got, err := loadToken(logger); err != nil || got != "ghk_from_env" {
		t.Fatalf("env fallback, got %q %v", got, err)
	}
	t.Setenv("GRIFFIN_HOST_TOKEN", "")
	if _, err := loadToken(logger); err == nil {
		t.Fatal("no token anywhere must be an error")
	}
}

func TestSandboxStatusNeverClaimsSafetyWhenUnavailable(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no bwrap
	got := sandboxStatus(context.Background())
	if !strings.Contains(got, "NOT AVAILABLE") || strings.HasPrefix(got, "ok") {
		t.Fatalf("with no bubblewrap the status must say so, got %q", got)
	}
}

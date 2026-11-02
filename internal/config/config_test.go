package config_test

import (
	"strings"
	"testing"

	"github.com/griffin/core/internal/config"
)

const (
	strongJWT    = "k3Jx9fQ2mZp7Vd1sT8wLb4nHc6YgR0uEa5XoI2qWz9A="
	strongWorker = "Zt8Qm2vL5pRx1NcB7hYd3JfK9sWe4UaG6oTi0VbXn2M="
	prodUserURL  = "postgres://griffin_app.ref:p@aws-0-ap-south-1.pooler.supabase.com:6543/postgres?sslmode=require"
	prodDBURL    = "postgres://u:p@aws-0-ap-south-1.pooler.supabase.com:5432/postgres?sslmode=require"
)

// clearEnv blanks every variable Load reads so tests are hermetic.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"ENVIRONMENT", "PORT", "DATABASE_URL", "SUPABASE_JWT_SECRET", "WORKER_API_KEY", "GRIFFIN_MODE",
		"STORAGE_PROVIDER", "S3_BUCKET", "S3_REGION", "S3_ENDPOINT", "AWS_PROFILE", "AWS_REGION",
		"CORS_ALLOWED_ORIGINS", "ENABLE_DEV_LOGIN", "SUPABASE_JWKS_URL", "AI_PROVIDER", "SCALE_TO_ZERO_ENABLED",
		"EC2_INSTANCE_ID", "S3_PRESIGN_EXPIRY_MINUTES", "MAX_CONCURRENT_WORKERS", "MAX_UPLOAD_BYTES", "USER_DATABASE_URL",
	} {
		t.Setenv(k, "")
	}
}

// prodEnv sets a fully valid production environment (API mode).
func prodEnv(t *testing.T) {
	t.Helper()
	clearEnv(t)
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("DATABASE_URL", prodDBURL)
	t.Setenv("USER_DATABASE_URL", prodUserURL)
	t.Setenv("SUPABASE_JWT_SECRET", strongJWT)
	t.Setenv("WORKER_API_KEY", strongWorker)
	t.Setenv("S3_BUCKET", "griffin-artifacts-220115213279-ap-south-1")
	t.Setenv("GRIFFIN_MODE", "api")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example.com")
	t.Setenv("EC2_INSTANCE_ID", "i-0123456789abcdef0")
}

func devEnv(t *testing.T) {
	t.Helper()
	clearEnv(t)
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/griffin_dev?sslmode=disable")
	t.Setenv("SUPABASE_JWT_SECRET", "griffin-dev-secret-key-do-not-use-in-prod-minimum32chars")
	t.Setenv("WORKER_API_KEY", "griffin-worker-secret-key-dev")
}

func TestNoSecretFallbackInAnyEnvironment(t *testing.T) {
	for _, env := range []string{"", "development", "test", "production"} {
		clearEnv(t)
		t.Setenv("ENVIRONMENT", env)
		if _, err := config.Load(); err == nil {
			t.Fatalf("ENVIRONMENT=%q: Load succeeded with no DATABASE_URL/secrets; a dev fallback exists", env)
		}
	}
}

func TestUnsetEnvironmentFailsClosedToProduction(t *testing.T) {
	devEnv(t)
	t.Setenv("ENVIRONMENT", "") // forgot to set it
	if _, err := config.Load(); err == nil {
		t.Fatal("dev secrets + localhost DB must be rejected when ENVIRONMENT is unset (treated as production)")
	}
}

func TestInvalidEnvironmentRejected(t *testing.T) {
	devEnv(t)
	t.Setenv("ENVIRONMENT", "prod-ish")
	if _, err := config.Load(); err == nil {
		t.Fatal("unknown ENVIRONMENT must be rejected")
	}
}

func TestDevelopmentLoadsWithExplicitValues(t *testing.T) {
	devEnv(t)
	t.Setenv("ENABLE_DEV_LOGIN", "true")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("development config should load: %v", err)
	}
	if cfg.IsProduction() || !cfg.EnableDevLogin {
		t.Fatalf("unexpected cfg: prod=%v devlogin=%v", cfg.IsProduction(), cfg.EnableDevLogin)
	}
	if cfg.Port != 8080 || cfg.MaxConcurrentWorkers != 2 || cfg.LeaseDurationSeconds != 60 {
		t.Errorf("defaults changed: %+v", cfg)
	}
}

func TestProductionValidConfigLoads(t *testing.T) {
	prodEnv(t)
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("valid production config rejected: %v", err)
	}
	if !cfg.IsProduction() || cfg.EnableDevLogin {
		t.Fatal("expected production without dev login")
	}
	if len(cfg.CORSAllowedOrigins) != 1 || cfg.CORSAllowedOrigins[0] != "https://app.example.com" {
		t.Errorf("unexpected origins %v", cfg.CORSAllowedOrigins)
	}
}

func TestProductionRejections(t *testing.T) {
	cases := map[string]map[string]string{
		"dev jwt secret":         {"SUPABASE_JWT_SECRET": "griffin-dev-secret-key-do-not-use-in-prod-minimum32chars"},
		"test jwt secret":        {"SUPABASE_JWT_SECRET": "test-secret-key-32chars-minimum-length-for-hmac!"},
		"short jwt secret":       {"SUPABASE_JWT_SECRET": "short"},
		"dev worker key":         {"WORKER_API_KEY": "griffin-worker-secret-key-dev"},
		"short worker key":       {"WORKER_API_KEY": "abc"},
		"same secret both":       {"WORKER_API_KEY": strongJWT},
		"localhost db":           {"DATABASE_URL": "postgres://u:p@localhost:5432/x?sslmode=require"},
		"sslmode disable":        {"DATABASE_URL": "postgres://u:p@db.example.com:5432/x?sslmode=disable"},
		"no user db url":         {"USER_DATABASE_URL": ""},
		"user db same as system": {"USER_DATABASE_URL": prodDBURL},
		"user db localhost":      {"USER_DATABASE_URL": "postgres://u:p@localhost:5432/x?sslmode=require"},
		"user db sslmode off":    {"USER_DATABASE_URL": "postgres://u:p@db.example.com:6543/x?sslmode=disable"},
		"dev login enabled":      {"ENABLE_DEV_LOGIN": "true"},
		"mock storage":           {"STORAGE_PROVIDER": "mock"},
		"default bucket":         {"S3_BUCKET": "griffin-artifacts"},
		"custom s3 endpoint":     {"S3_ENDPOINT": "http://localhost:4566"},
		"aws profile":            {"AWS_PROFILE": "griffin-bootstrap"},
		"no cors origins":        {"CORS_ALLOWED_ORIGINS": ""},
		"wildcard cors":          {"CORS_ALLOWED_ORIGINS": "*"},
		"wildcard subdomain":     {"CORS_ALLOWED_ORIGINS": "https://*.example.com"},
		"cors with path":         {"CORS_ALLOWED_ORIGINS": "https://app.example.com/x"},
		"no instance id":         {"EC2_INSTANCE_ID": ""},
		"mock ai in worker":      {"GRIFFIN_MODE": "worker", "AI_PROVIDER": "mock"},
		"bad mode":               {"GRIFFIN_MODE": "everything"},
		"no jwt secret or jwks":  {"SUPABASE_JWT_SECRET": "", "SUPABASE_JWKS_URL": ""},
	}
	for name, override := range cases {
		t.Run(name, func(t *testing.T) {
			prodEnv(t)
			for k, v := range override {
				t.Setenv(k, v)
			}
			if _, err := config.Load(); err == nil {
				t.Fatalf("expected production config to be rejected")
			}
		})
	}
}

func TestProductionWorkerModeNeedsNoCORSOrInstanceID(t *testing.T) {
	prodEnv(t)
	t.Setenv("GRIFFIN_MODE", "worker")
	t.Setenv("AI_PROVIDER", "antigravity")
	t.Setenv("CORS_ALLOWED_ORIGINS", "")
	t.Setenv("EC2_INSTANCE_ID", "")
	t.Setenv("USER_DATABASE_URL", "")
	if _, err := config.Load(); err != nil {
		t.Fatalf("worker-mode production should not require CORS/instance id/user db url: %v", err)
	}
}

func TestProductionJWKSOnlyAllowed(t *testing.T) {
	prodEnv(t)
	t.Setenv("SUPABASE_JWT_SECRET", "")
	t.Setenv("SUPABASE_JWKS_URL", "https://abc.supabase.co/auth/v1/.well-known/jwks.json")
	if _, err := config.Load(); err != nil {
		t.Fatalf("JWKS-only production should load: %v", err)
	}
}

func TestStorageAndOverrides(t *testing.T) {
	devEnv(t)
	t.Setenv("PORT", "9090")
	t.Setenv("MAX_CONCURRENT_WORKERS", "4")
	t.Setenv("STORAGE_PROVIDER", "mock")
	t.Setenv("S3_BUCKET", "custom-bucket")
	t.Setenv("S3_REGION", "us-east-1")
	t.Setenv("S3_PRESIGN_EXPIRY_MINUTES", "30")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 9090 || cfg.MaxConcurrentWorkers != 4 || cfg.StorageProvider != "mock" ||
		cfg.S3Bucket != "custom-bucket" || cfg.S3Region != "us-east-1" || cfg.S3PresignExpiryMinutes != 30 {
		t.Fatalf("overrides not applied: %+v", cfg)
	}
}

func TestDefaultsInDev(t *testing.T) {
	devEnv(t)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StorageProvider != "s3" || cfg.S3Bucket != "griffin-artifacts" || cfg.S3Region != "ap-south-1" ||
		cfg.S3PresignExpiryMinutes != 15 || cfg.MaxUploadBytes != 5<<20 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if strings.Contains(strings.Join(cfg.CORSAllowedOrigins, ","), "*") {
		t.Fatal("no wildcard origin by default")
	}
}

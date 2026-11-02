package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds environment-driven settings for the Griffin engine and API server.
type Config struct {
	Port                     int
	DatabaseURL              string
	SupabaseJWTSecret        string
	WorkerAPIKey             string
	Environment              string
	LogLevel                 string
	LeaseDurationSeconds     int
	HeartbeatIntervalSeconds int
	ReaperIntervalSeconds    int
	MaxConcurrentWorkers     int

	// AI Execution Provider settings (Phase 2)
	AIProvider                 string // "antigravity" (default) or "mock"
	AntigravityBinary          string // path or command name, default "agy"
	MaxConcurrentAntigravity   int    // default 1, maximum 2
	ExecutionTimeoutSeconds    int    // default 300
	GracefulTerminationSeconds int    // default 5
	WorkspaceRoot              string // default "/tmp/griffin-workspaces"
	RetainWorkspacesOnFailure  bool   // default false

	// Scale-to-Zero and Runner settings
	GriffinMode                     string // "all" (default), "api", "worker"
	ScaleToZeroEnabled              bool   // default true
	IdleShutdownSeconds             int    // default 300
	PreShutdownCheckIntervalSeconds int    // default 5
	EC2InstanceID                   string // e.g. "i-0123456789abcdef0"
	AWSRegion                       string // default "ap-south-1"
	RunnerWakeupCooldownSeconds     int    // default 30

	// Object Storage settings (Phase 4)
	StorageProvider        string // "s3" (default) or "mock"
	S3Bucket               string // default "griffin-artifacts"
	S3Region               string // default "ap-south-1" (or AWS_REGION)
	S3Endpoint             string // optional custom endpoint (for LocalStack/MinIO)
	S3ForcePathStyle       bool   // default false
	AWSProfile             string // optional named AWS CLI profile for local dev
	S3PresignExpiryMinutes int    // default 15 minutes
}

// Load reads configuration from environment variables, applying secure defaults.
func Load() (*Config, error) {
	cfg := &Config{
		Port:                            getEnvAsInt("PORT", 8080),
		DatabaseURL:                     getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:54322/griffin_dev?sslmode=disable"),
		SupabaseJWTSecret:               getEnv("SUPABASE_JWT_SECRET", "griffin-dev-secret-key-do-not-use-in-prod-minimum32chars"),
		WorkerAPIKey:                    getEnv("WORKER_API_KEY", "griffin-worker-secret-key-dev"),
		Environment:                     getEnv("ENVIRONMENT", "development"),
		LogLevel:                        getEnv("LOG_LEVEL", "info"),
		LeaseDurationSeconds:            getEnvAsInt("LEASE_DURATION_SECONDS", 60),
		HeartbeatIntervalSeconds:        getEnvAsInt("HEARTBEAT_INTERVAL_SECONDS", 15),
		ReaperIntervalSeconds:           getEnvAsInt("REAPER_INTERVAL_SECONDS", 30),
		MaxConcurrentWorkers:            getEnvAsInt("MAX_CONCURRENT_WORKERS", 2),
		AIProvider:                      getEnv("AI_PROVIDER", "antigravity"),
		AntigravityBinary:               getEnv("ANTIGRAVITY_BINARY", "agy"),
		MaxConcurrentAntigravity:        getEnvAsInt("MAX_CONCURRENT_ANTIGRAVITY", 1),
		ExecutionTimeoutSeconds:         getEnvAsInt("EXECUTION_TIMEOUT_SECONDS", 300),
		GracefulTerminationSeconds:      getEnvAsInt("GRACEFUL_TERMINATION_SECONDS", 5),
		WorkspaceRoot:                   getEnv("WORKSPACE_ROOT", "/tmp/griffin-workspaces"),
		RetainWorkspacesOnFailure:       getEnvAsBool("RETAIN_WORKSPACES_ON_FAILURE", false),
		GriffinMode:                     getEnv("GRIFFIN_MODE", "all"),
		ScaleToZeroEnabled:              getEnvAsBool("SCALE_TO_ZERO_ENABLED", true),
		IdleShutdownSeconds:             getEnvAsInt("IDLE_SHUTDOWN_SECONDS", 300),
		PreShutdownCheckIntervalSeconds: getEnvAsInt("PRE_SHUTDOWN_CHECK_INTERVAL_SECONDS", 5),
		EC2InstanceID:                   getEnv("EC2_INSTANCE_ID", ""),
		AWSRegion:                       getEnv("AWS_REGION", "ap-south-1"),
		RunnerWakeupCooldownSeconds:     getEnvAsInt("RUNNER_WAKEUP_COOLDOWN_SECONDS", 30),
		StorageProvider:                 getEnv("STORAGE_PROVIDER", "s3"),
		S3Bucket:                        getEnv("S3_BUCKET", "griffin-artifacts"),
		S3Region:                        getEnv("S3_REGION", getEnv("AWS_REGION", "ap-south-1")),
		S3Endpoint:                      getEnv("S3_ENDPOINT", ""),
		S3ForcePathStyle:                getEnvAsBool("S3_FORCE_PATH_STYLE", false),
		AWSProfile:                      getEnv("AWS_PROFILE", ""),
		S3PresignExpiryMinutes:          getEnvAsInt("S3_PRESIGN_EXPIRY_MINUTES", 15),
	}

	if cfg.Port <= 0 || cfg.Port > 65535 {
		return nil, fmt.Errorf("invalid PORT: %d", cfg.Port)
	}
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.SupabaseJWTSecret == "" {
		return nil, fmt.Errorf("SUPABASE_JWT_SECRET is required")
	}
	if cfg.WorkerAPIKey == "" {
		return nil, fmt.Errorf("WORKER_API_KEY is required")
	}
	if cfg.S3PresignExpiryMinutes <= 0 {
		cfg.S3PresignExpiryMinutes = 15
	}
	if cfg.StorageProvider == "s3" && cfg.S3Bucket == "" {
		return nil, fmt.Errorf("S3_BUCKET is required when STORAGE_PROVIDER is s3")
	}
	if cfg.MaxConcurrentAntigravity <= 0 {
		cfg.MaxConcurrentAntigravity = 1
	}
	if cfg.MaxConcurrentAntigravity > 2 {
		cfg.MaxConcurrentAntigravity = 2 // Bounded maximum of 2 on EC2 host
	}
	if cfg.ExecutionTimeoutSeconds <= 0 {
		cfg.ExecutionTimeoutSeconds = 300
	}
	if cfg.GracefulTerminationSeconds <= 0 {
		cfg.GracefulTerminationSeconds = 5
	}

	return cfg, nil
}

func getEnv(key, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return defaultVal
}

func getEnvAsInt(key string, defaultVal int) int {
	valStr := getEnv(key, "")
	if valStr == "" {
		return defaultVal
	}
	val, err := strconv.Atoi(valStr)
	if err != nil {
		return defaultVal
	}
	return val
}

func getEnvAsBool(key string, defaultVal bool) bool {
	valStr := getEnv(key, "")
	if valStr == "" {
		return defaultVal
	}
	val, err := strconv.ParseBool(valStr)
	if err != nil {
		return defaultVal
	}
	return val
}

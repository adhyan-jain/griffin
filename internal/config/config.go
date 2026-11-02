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
}

// Load reads configuration from environment variables, applying secure defaults.
func Load() (*Config, error) {
	cfg := &Config{
		Port:                     getEnvAsInt("PORT", 8080),
		DatabaseURL:              getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:54322/griffin_dev?sslmode=disable"),
		SupabaseJWTSecret:        getEnv("SUPABASE_JWT_SECRET", "griffin-dev-secret-key-do-not-use-in-prod-minimum32chars"),
		WorkerAPIKey:             getEnv("WORKER_API_KEY", "griffin-worker-secret-key-dev"),
		Environment:              getEnv("ENVIRONMENT", "development"),
		LogLevel:                 getEnv("LOG_LEVEL", "info"),
		LeaseDurationSeconds:     getEnvAsInt("LEASE_DURATION_SECONDS", 60),
		HeartbeatIntervalSeconds: getEnvAsInt("HEARTBEAT_INTERVAL_SECONDS", 15),
		ReaperIntervalSeconds:    getEnvAsInt("REAPER_INTERVAL_SECONDS", 30),
		MaxConcurrentWorkers:     getEnvAsInt("MAX_CONCURRENT_WORKERS", 2),
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

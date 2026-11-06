package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/google/uuid"
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
	ClaudeEnabled              bool   // optional second provider for language-only steps; off by default
	ClaudeBinary               string
	MaxConcurrentClaude        int
	AgyToolsEnabled            bool     // read/act tool tiers for agy; off by default
	AgyReadArgs                []string // extra agy flags for the read tier
	AgyActArgs                 []string // extra agy flags for the act tier
	AgyEffort                  string   // optional --effort level
	TaskCompactAfterDays       int      // finished tasks older than this are trimmed; 0 disables
	AntigravityBinary          string   // path or command name, default "agy"
	MaxConcurrentAntigravity   int      // default 1, maximum 2
	ExecutionTimeoutSeconds    int      // default 300
	GracefulTerminationSeconds int      // default 5
	WorkspaceRoot              string   // default "/tmp/griffin-workspaces"
	RetainWorkspacesOnFailure  bool     // default false

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

	// Security hardening
	CORSAllowedOrigins []string // exact origins; wildcard is rejected
	EnableDevLogin     bool     // dev-only POST /api/v1/auth/login; refused in production
	SupabaseJWKSURL    string   // optional JWKS endpoint for ES256/RS256 Supabase signing keys
	SupabaseJWTIssuer  string   // optional expected "iss"
	MaxUploadBytes     int64    // artifact upload cap, default 5 MiB

	// Database pool (Supabase pooler friendly)
	DBMaxConns int // default 25 (use 2 on Lambda)
	DBMinConns int // default 2 (use 0 on Lambda)
	// UserDatabaseURL: RLS-enforced role (NOSUPERUSER, NOBYPASSRLS) used for user requests in API
	// mode. DATABASE_URL is then the system role, used only by worker-key/system paths.
	UserDatabaseURL string
	// AllowedUserEmails restricts API access to the operator account(s); required in production API mode.
	AllowedUserEmails []string
	// AllowedUserIDs optionally pins the operator to Supabase user UUID(s) (JWT sub) in addition to
	// the email allow-list. Empty = identified by email claim only.
	AllowedUserIDs []uuid.UUID
}

// Environment names. Anything unset or unknown is treated as production (fail closed).
const (
	EnvProduction  = "production"
	EnvDevelopment = "development"
	EnvTest        = "test"
)

// IsProduction reports whether production safety rules apply.
func (c *Config) IsProduction() bool { return c.Environment == EnvProduction }

// knownDevSecrets are placeholder values that must never be used in production.
var knownDevSecrets = map[string]bool{
	"griffin-dev-secret-key-do-not-use-in-prod-minimum32chars": true,
	"griffin-worker-secret-key-dev":                            true,
	"test-secret-key-32chars-minimum-length-for-hmac!":         true,
	"test-worker-api-key-secret-12345":                         true,
}

// Load reads configuration from environment variables, applying secure defaults.
func Load() (*Config, error) {
	env := strings.ToLower(strings.TrimSpace(getEnv("ENVIRONMENT", EnvProduction)))
	switch env {
	case EnvProduction, EnvDevelopment, EnvTest:
	default:
		return nil, fmt.Errorf("invalid ENVIRONMENT %q (want production, development or test)", env)
	}

	cfg := &Config{
		Port:                            getEnvAsInt("PORT", 8080),
		DatabaseURL:                     getEnv("DATABASE_URL", ""),
		SupabaseJWTSecret:               getEnv("SUPABASE_JWT_SECRET", ""),
		WorkerAPIKey:                    getEnv("WORKER_API_KEY", ""),
		Environment:                     env,
		LogLevel:                        getEnv("LOG_LEVEL", "info"),
		LeaseDurationSeconds:            getEnvAsInt("LEASE_DURATION_SECONDS", 60),
		HeartbeatIntervalSeconds:        getEnvAsInt("HEARTBEAT_INTERVAL_SECONDS", 15),
		ReaperIntervalSeconds:           getEnvAsInt("REAPER_INTERVAL_SECONDS", 30),
		MaxConcurrentWorkers:            getEnvAsInt("MAX_CONCURRENT_WORKERS", 2),
		AIProvider:                      getEnv("AI_PROVIDER", "antigravity"),
		ClaudeEnabled:                   getEnvAsBool("CLAUDE_ENABLED", false),
		ClaudeBinary:                    getEnv("CLAUDE_BINARY", "claude"),
		MaxConcurrentClaude:             getEnvAsInt("MAX_CONCURRENT_CLAUDE", 2),
		AgyToolsEnabled:                 getEnvAsBool("AGY_TOOLS_ENABLED", false),
		AgyReadArgs:                     strings.Fields(getEnv("AGY_READ_ARGS", "--mode plan --sandbox")),
		AgyActArgs:                      strings.Fields(getEnv("AGY_ACT_ARGS", "--mode accept-edits --sandbox")),
		AgyEffort:                       getEnv("AGY_EFFORT", ""),
		TaskCompactAfterDays:            getEnvAsInt("TASK_COMPACT_AFTER_DAYS", 7),
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
		CORSAllowedOrigins:              splitCSV(getEnv("CORS_ALLOWED_ORIGINS", "")),
		EnableDevLogin:                  getEnvAsBool("ENABLE_DEV_LOGIN", false),
		SupabaseJWKSURL:                 getEnv("SUPABASE_JWKS_URL", ""),
		SupabaseJWTIssuer:               getEnv("SUPABASE_JWT_ISSUER", ""),
		MaxUploadBytes:                  int64(getEnvAsInt("MAX_UPLOAD_BYTES", 5<<20)),
		DBMaxConns:                      getEnvAsInt("DB_MAX_CONNS", 25),
		DBMinConns:                      getEnvAsInt("DB_MIN_CONNS", 2),
		UserDatabaseURL:                 getEnv("USER_DATABASE_URL", ""),
		AllowedUserEmails:               splitCSV(getEnv("ALLOWED_USER_EMAILS", "")),
	}

	if cfg.Port <= 0 || cfg.Port > 65535 {
		return nil, fmt.Errorf("invalid PORT: %d", cfg.Port)
	}
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.SupabaseJWTSecret == "" && cfg.SupabaseJWKSURL == "" {
		return nil, fmt.Errorf("SUPABASE_JWT_SECRET (or SUPABASE_JWKS_URL) is required")
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
	if cfg.MaxUploadBytes <= 0 {
		cfg.MaxUploadBytes = 5 << 20
	}
	if cfg.DBMaxConns <= 0 {
		cfg.DBMaxConns = 25
	}
	if cfg.DBMinConns < 0 || cfg.DBMinConns > cfg.DBMaxConns {
		cfg.DBMinConns = 0
	}
	for _, o := range cfg.CORSAllowedOrigins {
		if err := validateOrigin(o); err != nil {
			return nil, err
		}
	}
	for _, s := range splitCSV(getEnv("ALLOWED_USER_IDS", "")) {
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("ALLOWED_USER_IDS contains an invalid UUID %q", s)
		}
		cfg.AllowedUserIDs = append(cfg.AllowedUserIDs, id)
	}
	if cfg.EnableDevLogin && cfg.IsProduction() {
		return nil, fmt.Errorf("ENABLE_DEV_LOGIN must not be set in production")
	}
	if cfg.IsProduction() {
		if err := cfg.validateProduction(); err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

// validateProduction enforces fail-closed rules so development shortcuts cannot reach production.
func (c *Config) validateProduction() error {
	if c.SupabaseJWTSecret != "" {
		if len(c.SupabaseJWTSecret) < 32 || knownDevSecrets[c.SupabaseJWTSecret] {
			return fmt.Errorf("SUPABASE_JWT_SECRET is a development placeholder or shorter than 32 characters")
		}
	}
	if len(c.WorkerAPIKey) < 32 || knownDevSecrets[c.WorkerAPIKey] {
		return fmt.Errorf("WORKER_API_KEY is a development placeholder or shorter than 32 characters")
	}
	if c.WorkerAPIKey == c.SupabaseJWTSecret {
		return fmt.Errorf("WORKER_API_KEY must differ from SUPABASE_JWT_SECRET")
	}
	u, err := url.Parse(c.DatabaseURL)
	if err != nil || u.Host == "" {
		return fmt.Errorf("DATABASE_URL is not a valid URL")
	}
	host := u.Hostname()
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return fmt.Errorf("DATABASE_URL must not point at localhost in production")
	}
	if m := u.Query().Get("sslmode"); m == "disable" || m == "allow" || m == "prefer" {
		return fmt.Errorf("DATABASE_URL sslmode %q is not allowed in production", m)
	}
	if mode := c.GriffinMode; mode == "all" || mode == "api" {
		if len(c.AllowedUserEmails) == 0 {
			return fmt.Errorf("ALLOWED_USER_EMAILS is required in production (operator allow-list; sign-ups may be open)")
		}
		if c.UserDatabaseURL == "" {
			return fmt.Errorf("USER_DATABASE_URL (RLS-enforced role) is required in production for API mode")
		}
		if c.UserDatabaseURL == c.DatabaseURL {
			return fmt.Errorf("USER_DATABASE_URL must use a different role than DATABASE_URL (the system role)")
		}
		uu, err := url.Parse(c.UserDatabaseURL)
		if err != nil || uu.Host == "" {
			return fmt.Errorf("USER_DATABASE_URL is not a valid URL")
		}
		if h := uu.Hostname(); h == "localhost" || h == "127.0.0.1" || h == "::1" {
			return fmt.Errorf("USER_DATABASE_URL must not point at localhost in production")
		}
		if m := uu.Query().Get("sslmode"); m == "disable" || m == "allow" || m == "prefer" {
			return fmt.Errorf("USER_DATABASE_URL sslmode %q is not allowed in production", m)
		}
	}
	if c.StorageProvider != "s3" {
		return fmt.Errorf("STORAGE_PROVIDER must be s3 in production")
	}
	if _, set := os.LookupEnv("S3_BUCKET"); !set || c.S3Bucket == "griffin-artifacts" {
		return fmt.Errorf("S3_BUCKET must be set explicitly in production")
	}
	if c.S3Endpoint != "" || c.AWSProfile != "" {
		return fmt.Errorf("S3_ENDPOINT and AWS_PROFILE must not be set in production (use the IAM role)")
	}
	mode := c.GriffinMode
	if mode != "all" && mode != "api" && mode != "worker" {
		return fmt.Errorf("invalid GRIFFIN_MODE %q", mode)
	}
	runsAPI := mode == "all" || mode == "api"
	runsWorker := mode == "all" || mode == "worker"
	if runsAPI && len(c.CORSAllowedOrigins) == 0 {
		return fmt.Errorf("CORS_ALLOWED_ORIGINS is required in production")
	}
	if runsAPI && c.ScaleToZeroEnabled && c.EC2InstanceID == "" {
		return fmt.Errorf("EC2_INSTANCE_ID is required in production when scale-to-zero is enabled")
	}
	if runsWorker && c.AIProvider != "antigravity" {
		return fmt.Errorf("AI_PROVIDER must be antigravity in production (mock executor is not allowed)")
	}
	return nil
}

func validateOrigin(o string) error {
	if o == "*" || strings.Contains(o, "*") {
		return fmt.Errorf("CORS_ALLOWED_ORIGINS must not contain wildcards: %q", o)
	}
	u, err := url.Parse(o)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.Path != "" || u.RawQuery != "" {
		return fmt.Errorf("invalid CORS origin %q (want scheme://host[:port])", o)
	}
	return nil
}

func splitCSV(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, strings.TrimRight(p, "/"))
		}
	}
	return out
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

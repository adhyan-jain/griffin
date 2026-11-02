package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Agent struct {
	ID                     uuid.UUID `json:"id"`
	UserID                 uuid.UUID `json:"user_id"`
	Name                   string    `json:"name"`
	RoleType               string    `json:"role_type"`      // 'ORCHESTRATOR', 'MANAGER', 'WORKER'
	ManagerDomain          string    `json:"manager_domain"` // 'JJ', 'CAREER', 'RESEARCH', etc.
	Description            string    `json:"description"`
	SystemPrompt           string    `json:"system_prompt"`
	ProviderPreference     string    `json:"provider_preference"`
	ModelPreference        string    `json:"model_preference"`
	MaxExecutionTimeoutSec int       `json:"max_execution_timeout_sec"`
	IsActive               bool      `json:"is_active"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

type Artifact struct {
	ID            uuid.UUID       `json:"id"`
	UserID        uuid.UUID       `json:"user_id"`
	TaskID        *uuid.UUID      `json:"task_id,omitempty"`
	Title         string          `json:"title"`
	ArtifactType  string          `json:"artifact_type"` // 'GOOGLE_DOC', 'PDF', 'MARKDOWN', 'CODE_DIFF', 'JSON'
	StorageURI    string          `json:"storage_uri"`
	FileSizeBytes *int64          `json:"file_size_bytes,omitempty"`
	MimeType      *string         `json:"mime_type,omitempty"`
	Metadata      json.RawMessage `json:"metadata"`
	CreatedAt     time.Time       `json:"created_at"`
}

type Notification struct {
	ID        uuid.UUID  `json:"id"`
	UserID    uuid.UUID  `json:"user_id"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	Channel   string     `json:"channel"`  // 'IN_APP', 'WEB_PUSH', 'EMAIL'
	Priority  string     `json:"priority"` // 'NORMAL', 'HIGH', 'URGENT'
	ReadAt    *time.Time `json:"read_at,omitempty"`
	ActionURL *string    `json:"action_url,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

type Schedule struct {
	ID               uuid.UUID       `json:"id"`
	UserID           uuid.UUID       `json:"user_id"`
	ManagerDomain    string          `json:"manager_domain"`
	Name             string          `json:"name"`
	CronExpression   string          `json:"cron_expression"`
	Timezone         string          `json:"timezone"`
	TaskSpecTemplate json.RawMessage `json:"task_spec_template"`
	IsPaused         bool            `json:"is_paused"`
	LastRunAt        *time.Time      `json:"last_run_at,omitempty"`
	NextRunAt        time.Time       `json:"next_run_at"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
}

type UserProfile struct {
	ID        uuid.UUID       `json:"id"`
	Email     string          `json:"email"`
	FullName  *string         `json:"full_name,omitempty"`
	Role      string          `json:"role"`
	Settings  json.RawMessage `json:"settings"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type Workspace struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"user_id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

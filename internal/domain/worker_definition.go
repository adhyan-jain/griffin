package domain

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var validWorkerKeyRegex = regexp.MustCompile(`^[a-z0-9_-]{3,64}$`)

// WorkerDefinition represents a reusable specialist worker specification with explicit versioning.
type WorkerDefinition struct {
	ID                   uuid.UUID       `json:"id"`
	UserID               uuid.UUID       `json:"user_id"`
	WorkerKey            string          `json:"worker_key"`
	Version              int             `json:"version"`
	ManagerID            string          `json:"manager_id"`
	Name                 string          `json:"name"`
	Description          string          `json:"description"`
	SystemPrompt         string          `json:"system_prompt"`
	Domain               string          `json:"domain"`
	InputSchema          json.RawMessage `json:"input_schema"`
	OutputSchema         json.RawMessage `json:"output_schema"`
	AllowedCapabilities  []string        `json:"allowed_capabilities"`
	AllowedTools         []string        `json:"allowed_tools"`
	ExecutionTimeoutSec  int             `json:"execution_timeout_sec"`
	MaxRetries           int             `json:"max_retries"`
	VerificationCriteria []string        `json:"verification_criteria"`
	IsActive             bool            `json:"is_active"`
	CreatedAt            time.Time       `json:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
}

// Validate checks worker definition constraints and ensures governance compliance.
func (w *WorkerDefinition) Validate() error {
	w.WorkerKey = strings.TrimSpace(strings.ToLower(w.WorkerKey))
	if !validWorkerKeyRegex.MatchString(w.WorkerKey) {
		return fmt.Errorf("%w: worker_key must be 3-64 alphanumeric characters, underscores, or hyphens", ErrValidation)
	}

	if strings.TrimSpace(w.Name) == "" {
		return fmt.Errorf("%w: name cannot be empty", ErrValidation)
	}
	if strings.TrimSpace(w.ManagerID) == "" {
		return fmt.Errorf("%w: manager_id cannot be empty", ErrValidation)
	}
	if strings.TrimSpace(w.Domain) == "" {
		w.Domain = w.ManagerID
	}
	if strings.TrimSpace(w.SystemPrompt) == "" {
		return fmt.Errorf("%w: system_prompt cannot be empty", ErrValidation)
	}

	if w.ExecutionTimeoutSec <= 0 {
		w.ExecutionTimeoutSec = 300 // 5 minutes default
	}
	if w.ExecutionTimeoutSec > 3600 {
		return fmt.Errorf("%w: execution_timeout_sec cannot exceed 3600 seconds", ErrValidation)
	}

	if w.MaxRetries < 0 || w.MaxRetries > 10 {
		return fmt.Errorf("%w: max_retries must be between 0 and 10", ErrValidation)
	}

	if w.AllowedCapabilities == nil {
		w.AllowedCapabilities = []string{}
	}
	if w.AllowedTools == nil {
		w.AllowedTools = []string{}
	}
	if w.VerificationCriteria == nil {
		w.VerificationCriteria = []string{}
	}

	// Validate capabilities
	for _, cap := range w.AllowedCapabilities {
		if !IsKnownCapability(cap) {
			return fmt.Errorf("%w: unknown capability '%s'", ErrValidation, cap)
		}
	}

	// Ensure schemas are valid JSON if provided
	if len(w.InputSchema) > 0 && !json.Valid(w.InputSchema) {
		return fmt.Errorf("%w: input_schema must be valid JSON", ErrValidation)
	}
	if len(w.OutputSchema) > 0 && !json.Valid(w.OutputSchema) {
		return fmt.Errorf("%w: output_schema must be valid JSON", ErrValidation)
	}

	if len(w.InputSchema) == 0 {
		w.InputSchema = json.RawMessage("{}")
	}
	if len(w.OutputSchema) == 0 {
		w.OutputSchema = json.RawMessage("{}")
	}

	return nil
}

// WorkerDefinitionUpdate represents update parameters for versioning a worker definition.
type WorkerDefinitionUpdate struct {
	Name                 *string          `json:"name,omitempty"`
	Description          *string          `json:"description,omitempty"`
	SystemPrompt         *string          `json:"system_prompt,omitempty"`
	InputSchema          *json.RawMessage `json:"input_schema,omitempty"`
	OutputSchema         *json.RawMessage `json:"output_schema,omitempty"`
	AllowedCapabilities  *[]string        `json:"allowed_capabilities,omitempty"`
	AllowedTools         *[]string        `json:"allowed_tools,omitempty"`
	ExecutionTimeoutSec  *int             `json:"execution_timeout_sec,omitempty"`
	MaxRetries           *int             `json:"max_retries,omitempty"`
	VerificationCriteria *[]string        `json:"verification_criteria,omitempty"`
}

// AgentFactoryAudit records changes to worker definitions.
type AgentFactoryAudit struct {
	ID                 int64           `json:"id"`
	UserID             uuid.UUID       `json:"user_id"`
	WorkerDefinitionID *uuid.UUID      `json:"worker_definition_id,omitempty"`
	WorkerKey          string          `json:"worker_key"`
	Version            int             `json:"version"`
	Action             string          `json:"action"` // 'CREATED', 'UPDATED', 'TOGGLED'
	Actor              string          `json:"actor"`  // 'USER', 'JJ', 'SYSTEM'
	Changes            json.RawMessage `json:"changes"`
	CreatedAt          time.Time       `json:"created_at"`
}

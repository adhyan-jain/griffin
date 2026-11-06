package worker

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
)

// UsageMetadata captures token counts and inference telemetry when reported by the provider.
type UsageMetadata struct {
	InputTokens     int `json:"input_tokens,omitempty"`
	OutputTokens    int `json:"output_tokens,omitempty"`
	ThinkingTokens  int `json:"thinking_tokens,omitempty"`
	CacheReadTokens int `json:"cache_read_tokens,omitempty"`
	TotalTokens     int `json:"total_tokens,omitempty"`
}

// ExecutionResult represents the provider-neutral outcome of a task execution attempt.
type ExecutionResult struct {
	ExecutionID      uuid.UUID         `json:"execution_id"`
	ProviderName     string            `json:"provider_name"`
	ConversationID   *string           `json:"conversation_id,omitempty"`
	Status           string            `json:"status"` // "SUCCESS", "FAILED", "AWAITING_APPROVAL"
	Output           json.RawMessage   `json:"output,omitempty"`
	TextOutput       string            `json:"text_output,omitempty"`
	RequiresApproval *domain.Approval  `json:"requires_approval,omitempty"`
	Error            error             `json:"error,omitempty"`
	Retryable        bool              `json:"retryable"`
	Duration         time.Duration     `json:"duration"`
	Usage            *UsageMetadata    `json:"usage,omitempty"`
	Artifacts        []domain.Artifact `json:"artifacts,omitempty"`

	// Token counters for attempt tracking compatibility
	PromptTokens     int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
}

// TaskExecutor defines the contract for task execution providers (e.g. Mock, Antigravity, Claude Cowork).
type TaskExecutor interface {
	Execute(ctx context.Context, task *domain.Task) (*ExecutionResult, error)
	Name() string
	IsHealthy(ctx context.Context) (bool, error)
}

// RuntimeFacts describes the provider runtime (grounded facts the model may quote).
type RuntimeFacts struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Model   string `json:"model"`
}

// RuntimeFactsProvider is an optional interface executors implement to expose runtime facts.
type RuntimeFactsProvider interface {
	RuntimeFacts() RuntimeFacts
}

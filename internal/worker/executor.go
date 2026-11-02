package worker

import (
	"context"
	"encoding/json"

	"github.com/griffin/core/internal/domain"
)

// ExecutionResult represents the outcome of an execution attempt by a TaskExecutor.
type ExecutionResult struct {
	Output           json.RawMessage
	RequiresApproval *domain.Approval
	Error            error
	Retryable        bool
	PromptTokens     int
	CompletionTokens int
}

// TaskExecutor defines the contract for task execution providers (e.g. Mock, Antigravity, Claude Cowork).
type TaskExecutor interface {
	Execute(ctx context.Context, task *domain.Task) (*ExecutionResult, error)
}

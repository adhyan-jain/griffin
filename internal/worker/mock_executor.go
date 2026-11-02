package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
)

// MockExecutor provides deterministic, reproducible task execution for testing the complete lifecycle.
type MockExecutor struct {
	mu                   sync.RWMutex
	FailTasks            map[uuid.UUID]bool
	RequireApprovalTasks map[uuid.UUID]*domain.Approval
	ExecutionDelay       time.Duration
	ExecutedTasks        []uuid.UUID
}

// NewMockExecutor initializes a MockExecutor.
func NewMockExecutor() *MockExecutor {
	return &MockExecutor{
		FailTasks:            make(map[uuid.UUID]bool),
		RequireApprovalTasks: make(map[uuid.UUID]*domain.Approval),
	}
}

func (m *MockExecutor) Execute(ctx context.Context, task *domain.Task) (*ExecutionResult, error) {
	m.mu.Lock()
	m.ExecutedTasks = append(m.ExecutedTasks, task.ID)
	shouldFail := m.FailTasks[task.ID]
	approval := m.RequireApprovalTasks[task.ID]
	delay := m.ExecutionDelay
	m.mu.Unlock()

	if delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}

	if approval != nil {
		return &ExecutionResult{
			RequiresApproval: approval,
		}, nil
	}

	if shouldFail {
		return &ExecutionResult{
			Error:     fmt.Errorf("simulated mock task failure for task %s", task.ID),
			Retryable: true,
		}, nil
	}

	outMap := map[string]any{
		"task_id":     task.ID.String(),
		"title":       task.Title,
		"status":      "mock_execution_success",
		"executed_at": time.Now().UTC().Format(time.RFC3339),
	}
	outBytes, _ := json.Marshal(outMap)

	return &ExecutionResult{
		Output:           outBytes,
		PromptTokens:     100,
		CompletionTokens: 50,
	}, nil
}

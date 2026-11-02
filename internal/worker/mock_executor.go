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
	Unhealthy            bool
}

// NewMockExecutor initializes a MockExecutor.
func NewMockExecutor() *MockExecutor {
	return &MockExecutor{
		FailTasks:            make(map[uuid.UUID]bool),
		RequireApprovalTasks: make(map[uuid.UUID]*domain.Approval),
	}
}

func (m *MockExecutor) Name() string {
	return "mock"
}

func (m *MockExecutor) IsHealthy(ctx context.Context) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.Unhealthy {
		return false, fmt.Errorf("mock executor simulated unhealthy status")
	}
	return true, nil
}

func (m *MockExecutor) Execute(ctx context.Context, task *domain.Task) (*ExecutionResult, error) {
	execID := uuid.New()
	start := time.Now()

	m.mu.Lock()
	m.ExecutedTasks = append(m.ExecutedTasks, task.ID)
	shouldFail := m.FailTasks[task.ID]
	approval := m.RequireApprovalTasks[task.ID]
	delay := m.ExecutionDelay
	m.mu.Unlock()

	if delay > 0 {
		select {
		case <-ctx.Done():
			return &ExecutionResult{
				ExecutionID:  execID,
				ProviderName: m.Name(),
				Status:       "FAILED",
				Error:        ctx.Err(),
				Duration:     time.Since(start),
			}, ctx.Err()
		case <-time.After(delay):
		}
	}

	if approval != nil {
		return &ExecutionResult{
			ExecutionID:      execID,
			ProviderName:     m.Name(),
			Status:           "AWAITING_APPROVAL",
			RequiresApproval: approval,
			Duration:         time.Since(start),
		}, nil
	}

	if shouldFail {
		err := fmt.Errorf("simulated mock task failure for task %s", task.ID)
		return &ExecutionResult{
			ExecutionID:  execID,
			ProviderName: m.Name(),
			Status:       "FAILED",
			Error:        err,
			Retryable:    true,
			Duration:     time.Since(start),
		}, nil
	}

	outMap := map[string]any{
		"task_id":     task.ID.String(),
		"title":       task.Title,
		"status":      "mock_execution_success",
		"executed_at": time.Now().UTC().Format(time.RFC3339),
	}
	outBytes, _ := json.Marshal(outMap)
	convID := fmt.Sprintf("mock-conv-%s", task.ID.String()[:8])

	return &ExecutionResult{
		ExecutionID:    execID,
		ProviderName:   m.Name(),
		ConversationID: &convID,
		Status:         "SUCCESS",
		Output:         outBytes,
		TextOutput:     fmt.Sprintf("Mock executed task %s successfully", task.Title),
		PromptTokens:   100,
		CompletionTokens: 50,
		Usage: &UsageMetadata{
			InputTokens:  100,
			OutputTokens: 50,
			TotalTokens:  150,
		},
		Duration: time.Since(start),
	}, nil
}

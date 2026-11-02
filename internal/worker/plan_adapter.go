package worker

import (
	"context"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/service"
)

// PlanExecutorAdapter adapts a worker.TaskExecutor into a service.PlanExecutor without circular dependencies.
type PlanExecutorAdapter struct {
	executor TaskExecutor
}

// NewPlanExecutorAdapter constructs a new PlanExecutorAdapter.
func NewPlanExecutorAdapter(executor TaskExecutor) *PlanExecutorAdapter {
	return &PlanExecutorAdapter{executor: executor}
}

// ExecuteTask satisfies service.PlanExecutor.
func (a *PlanExecutorAdapter) ExecuteTask(ctx context.Context, task *domain.Task) (*service.PlanExecutionResult, error) {
	res, err := a.executor.Execute(ctx, task)
	if err != nil {
		return nil, err
	}
	return &service.PlanExecutionResult{
		StructuredOutput: res.Output,
		TextOutput:       res.TextOutput,
	}, nil
}

// IsHealthy satisfies service.PlanExecutor.
func (a *PlanExecutorAdapter) IsHealthy(ctx context.Context) bool {
	if a.executor == nil {
		return false
	}
	healthy, err := a.executor.IsHealthy(ctx)
	return healthy && err == nil
}

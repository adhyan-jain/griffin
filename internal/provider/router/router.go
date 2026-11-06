// Package router sends each task to the AI provider that suits what the step needs, instead of one
// provider for everything (see domain.ClassifyProvider for how a step is judged: by capability, not
// by difficulty).
//
// Claude is optional and off by default. When it is not configured, or is not signed in, every step
// quietly runs on agy and the attempt records which provider actually ran it, so nothing is lost and
// nothing is billed to an API key.
package router

import (
	"context"
	"errors"
	"log/slog"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/worker"
)

// Router implements worker.TaskExecutor over a primary provider (agy) and an optional second one.
type Router struct {
	agy    worker.TaskExecutor
	claude worker.TaskExecutor // nil when Claude is not enabled
	logger *slog.Logger
}

// New builds a router. claude may be nil.
func New(agy, claude worker.TaskExecutor, logger *slog.Logger) *Router {
	if logger == nil {
		logger = slog.Default()
	}
	return &Router{agy: agy, claude: claude, logger: logger}
}

func (r *Router) Name() string { return "router" }

// IsHealthy follows the primary provider: the system is usable as long as agy is.
func (r *Router) IsHealthy(ctx context.Context) (bool, error) { return r.agy.IsHealthy(ctx) }

// RuntimeFacts forwards the primary provider's facts (chat quotes them).
func (r *Router) RuntimeFacts() worker.RuntimeFacts {
	if rp, ok := r.agy.(worker.RuntimeFactsProvider); ok {
		return rp.RuntimeFacts()
	}
	return worker.RuntimeFacts{}
}

// Execute runs the task on its planned provider, falling back to agy when Claude is off, unhealthy,
// or reports that it is not signed in.
func (r *Router) Execute(ctx context.Context, task *domain.Task) (*worker.ExecutionResult, error) {
	if domain.ProviderHintFor(task) != domain.ProviderClaude {
		return r.agy.Execute(ctx, task)
	}
	if r.claude == nil {
		r.logger.Debug("Step planned for claude; claude is not enabled, running on agy", "task_id", task.ID)
		return r.agy.Execute(ctx, task)
	}
	if ok, _ := r.claude.IsHealthy(ctx); !ok {
		r.logger.Warn("Claude is unavailable; running the step on agy", "task_id", task.ID)
		return r.agy.Execute(ctx, task)
	}

	res, err := r.claude.Execute(ctx, task)
	if errors.Is(err, domain.ErrAuthenticationRequired) || (res != nil && errors.Is(res.Error, domain.ErrAuthenticationRequired)) {
		r.logger.Warn("Claude is not signed in; running the step on agy. Sign in on the worker to enable it.", "task_id", task.ID)
		return r.agy.Execute(ctx, task)
	}
	return res, err
}

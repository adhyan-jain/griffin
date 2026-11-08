package worker

import (
	"context"

	"github.com/griffin/core/internal/repository"
)

type hostCtxKey int

const (
	approvedActionsKey hostCtxKey = iota
	dependencyOutputsKey
)

// WithApprovedActions records, for one execution, the approval action types the owner has already granted
// for the task. A host's executor uses it to tell that the exact action it paused on is now approved.
func WithApprovedActions(ctx context.Context, actions []string) context.Context {
	set := make(map[string]bool, len(actions))
	for _, a := range actions {
		set[a] = true
	}
	return context.WithValue(ctx, approvedActionsKey, set)
}

// ApprovedActionsFrom returns the approved action types recorded by WithApprovedActions (never nil).
func ApprovedActionsFrom(ctx context.Context) map[string]bool {
	if set, ok := ctx.Value(approvedActionsKey).(map[string]bool); ok {
		return set
	}
	return map[string]bool{}
}

// WithDependencyContext hands an executor the raw outputs of the step's completed prerequisites, for
// executors that act on a structured earlier result rather than reading it as prompt text.
func WithDependencyContext(ctx context.Context, deps []repository.DependencyOutput) context.Context {
	return context.WithValue(ctx, dependencyOutputsKey, deps)
}

// DependencyContextFrom returns the outputs recorded by WithDependencyContext.
func DependencyContextFrom(ctx context.Context) []repository.DependencyOutput {
	deps, _ := ctx.Value(dependencyOutputsKey).([]repository.DependencyOutput)
	return deps
}

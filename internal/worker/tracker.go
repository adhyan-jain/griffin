package worker

import (
	"context"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/service"
)

// trackers is the shared-list logic, backed by the same store as playbooks.
func (w *Worker) trackers() *service.TrackerLists {
	if w.playbooks == nil {
		return nil
	}
	return &service.TrackerLists{Store: w.playbooks, Tasks: w.taskRepo, Logger: w.logger}
}

// withTrackerList shows a task its tracker's shared list.
func (w *Worker) withTrackerList(ctx context.Context, task *domain.Task) *domain.Task {
	if t := w.trackers(); t != nil {
		return t.Prepare(ctx, task)
	}
	return task
}

// saveTracker writes the list a finished task handed back.
func (w *Worker) saveTracker(ctx context.Context, task *domain.Task, res *ExecutionResult) {
	body, ext, _, _, ok := resultFile(res)
	if t := w.trackers(); t != nil && ok && ext == "md" {
		t.Save(ctx, task, string(body))
	}
}

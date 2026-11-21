package worker

import (
	"context"
	"encoding/json"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/service"
)

// trackers is the shared-list logic, backed by the same store as playbooks.
func (w *Worker) trackers() *service.TrackerLists {
	if w.playbooks == nil {
		return nil
	}
	return &service.TrackerLists{Store: w.playbooks, Tasks: w.taskRepo, Logger: w.logger, ApprovedDrafts: w.engine.ApprovedDraftTasks}
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

// holdApplicationDraft hands a finished application draft to the owner's Approvals page (see the engine).
func (w *Worker) holdApplicationDraft(ctx context.Context, task *domain.Task, res *ExecutionResult) bool {
	if res == nil {
		return false
	}
	out := res.Output
	if t := chatResponseText(res); t != "" {
		out, _ = json.Marshal(map[string]string{"response": t})
	}
	return w.engine.HoldApplicationDraft(ctx, task, out)
}

// finishApprovedApplication completes an application task whose draft the owner approved.
func (w *Worker) finishApprovedApplication(ctx context.Context, task *domain.Task) bool {
	if !w.engine.FinishApprovedApplication(ctx, task, w.ID) {
		return false
	}
	w.notifyFinished(ctx, task.ID)
	return true
}

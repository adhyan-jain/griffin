package worker

import (
	"context"
	"encoding/json"
	"strings"

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

// holdApplicationDraft stops a finished application draft at the owner's Approvals page instead of completing the
// task. It returns true when the task was paused for approval; false means the task finishes as usual.
func (w *Worker) holdApplicationDraft(ctx context.Context, task *domain.Task, res *ExecutionResult) bool {
	if !domain.IsApplyTask(task.InputPayload) || res == nil {
		return false
	}
	draft := chatResponseText(res)
	if strings.TrimSpace(draft) == "" {
		return false
	}
	if err := w.engine.RequestApproval(ctx, task.ID, domain.ApplyDraftApproval(task, draft)); err != nil {
		w.logger.Error("Could not request approval for the application draft; finishing the task instead", "worker_id", w.ID, "task_id", task.ID, "error", err)
		return false
	}
	w.logger.Info("Application drafted; paused for the owner's review", "worker_id", w.ID, "task_id", task.ID)
	return true
}

// finishApprovedApplication completes an application task whose draft the owner approved, using the draft held on
// the approval. Nothing is submitted. It returns true when the task was finished here.
func (w *Worker) finishApprovedApplication(ctx context.Context, task *domain.Task) bool {
	if !domain.IsApplyTask(task.InputPayload) {
		return false
	}
	a, err := w.engine.ApprovedApproval(ctx, task, domain.ApplyDraftApprovalAction)
	if err != nil || a == nil {
		return false
	}
	var p struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(a.ActionPayload, &p) != nil || strings.TrimSpace(p.Text) == "" {
		return false
	}
	out, _ := json.Marshal(map[string]string{"response": p.Text})
	if err := w.engine.CompleteTask(ctx, task.ID, w.ID, out); err != nil {
		w.logger.Error("engine.CompleteTask failed for an approved application", "worker_id", w.ID, "task_id", task.ID, "error", err)
		return false
	}
	w.notifyFinished(ctx, task.ID)
	return true
}

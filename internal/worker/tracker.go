package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/storage"
)

const trackerReadLimit = 200 << 10

// trackerTypeFor is the tracker a task works on. A follow-up has the type of the task it continues,
// whatever its own wording is.
func (w *Worker) trackerTypeFor(ctx context.Context, task *domain.Task) string {
	if t := domain.TrackerTypeOf(task.Title, task.Goal, task.InputPayload); t != "" {
		return t
	}
	var p struct {
		ThreadID   string `json:"thread_id"`
		FollowUpOf string `json:"follow_up_of"`
	}
	if json.Unmarshal(task.InputPayload, &p) != nil || p.FollowUpOf == "" {
		return ""
	}
	id, err := uuid.Parse(strings.TrimSpace(p.ThreadID))
	if err != nil {
		if id, err = uuid.Parse(strings.TrimSpace(p.FollowUpOf)); err != nil {
			return ""
		}
	}
	root, err := w.taskRepo.GetTaskByID(ctx, id)
	if err != nil || root == nil {
		return ""
	}
	return domain.TrackerTypeOf(root.Title, root.Goal, root.InputPayload)
}

// trackerList finds the shared list of a type among the user's living lists.
func (w *Worker) trackerList(ctx context.Context, userID uuid.UUID, kind, trackerType string) *domain.Artifact {
	arts, err := w.playbooks.ListByKind(ctx, userID, kind)
	if err != nil {
		return nil
	}
	for _, a := range arts {
		var m struct {
			Tracker string `json:"tracker"`
		}
		if json.Unmarshal(a.Metadata, &m) == nil && m.Tracker == trackerType {
			return a
		}
	}
	return nil
}

// withTrackerList returns a copy of the task whose goal ends with its tracker's current list, so the
// task starts from the shared list instead of from nothing. Nothing is persisted on the task.
func (w *Worker) withTrackerList(ctx context.Context, task *domain.Task) *domain.Task {
	if w.playbooks == nil {
		return task
	}
	t := w.trackerTypeFor(ctx, task)
	if t == "" {
		return task
	}
	current := ""
	if list := w.trackerList(ctx, task.UserID, storage.ListKind, t); list != nil {
		if text, err := w.playbooks.ReadText(ctx, task.UserID, list.ID, trackerReadLimit); err == nil {
			current = text
		}
	}
	cp := *task
	cp.Goal = task.Goal + domain.TrackerPrompt(t, current)
	return &cp
}

// saveTracker writes the list a task handed back into its tracker's shared list. The list that was there
// is kept as one rolling backup first, so a task that gets it wrong never costs the data. With no list yet,
// the first table a tracker task produced starts it.
func (w *Worker) saveTracker(ctx context.Context, task *domain.Task, res *ExecutionResult) {
	if w.playbooks == nil {
		return
	}
	t := w.trackerTypeFor(ctx, task)
	if t == "" {
		return
	}
	body, ext, _, _, ok := resultFile(res)
	if !ok || ext != "md" {
		return
	}
	list := w.trackerList(ctx, task.UserID, storage.ListKind, t)
	table, found := domain.ExtractTrackerTable(string(body))
	if !found {
		if list != nil {
			return // the task did not change the list
		}
		if table, found = domain.FirstTable(string(body)); !found {
			return
		}
	}
	if !domain.PlaybookLooksSafe(table) {
		w.logger.Warn("Tracker list not saved: it looked like it contained a credential", "task_id", task.ID)
		return
	}
	title := domain.TrackerTypes[t] + " tracker"
	taskID := task.ID
	if list == nil {
		if _, err := w.playbooks.SaveArtifact(ctx, storage.SaveArtifactInput{
			UserID: task.UserID, TaskID: &taskID, Title: title, Filename: t + "-tracker.md",
			ArtifactType: "MARKDOWN", ContentType: "text/markdown; charset=utf-8",
			Body: bytes.NewReader([]byte(table + "\n")), Metadata: domain.TrackerListMetadata(storage.ListKind, t),
		}); err != nil {
			w.logger.Warn("Could not start the tracker list", "task_id", task.ID, "error", err)
		}
		return
	}
	w.backupTracker(ctx, task, t, list)
	if _, err := w.playbooks.UpdateText(ctx, list.ID, task.UserID, "# "+title+"\n\n"+table, false, trackerReadLimit); err != nil {
		w.logger.Warn("Could not update the tracker list", "task_id", task.ID, "error", err)
	}
}

func (w *Worker) backupTracker(ctx context.Context, task *domain.Task, t string, list *domain.Artifact) {
	old, err := w.playbooks.ReadText(ctx, task.UserID, list.ID, trackerReadLimit)
	if err != nil || strings.TrimSpace(old) == "" {
		return
	}
	if bak := w.trackerList(ctx, task.UserID, domain.TrackerBackupKind, t); bak != nil {
		_, _ = w.playbooks.UpdateText(ctx, bak.ID, task.UserID, old, false, trackerReadLimit)
		return
	}
	_, _ = w.playbooks.SaveArtifact(ctx, storage.SaveArtifactInput{
		UserID: task.UserID, Title: domain.TrackerTypes[t] + " tracker (previous)", Filename: t + "-tracker-previous.md",
		ArtifactType: "MARKDOWN", ContentType: "text/markdown; charset=utf-8",
		Body: bytes.NewReader([]byte(old)), Metadata: domain.TrackerListMetadata(domain.TrackerBackupKind, t),
	})
}

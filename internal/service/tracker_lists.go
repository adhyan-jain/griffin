package service

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/storage"
)

// TrackerStore is the storage a tracker's shared list lives in. *storage.StorageService implements it.
type TrackerStore interface {
	SaveArtifact(ctx context.Context, input storage.SaveArtifactInput) (*domain.Artifact, error)
	ListByKind(ctx context.Context, userID uuid.UUID, kind string) ([]*domain.Artifact, error)
	ReadText(ctx context.Context, userID, id uuid.UUID, maxBytes int64) (string, error)
	UpdateText(ctx context.Context, id, userID uuid.UUID, text string, appendMode bool, maxBytes int64) (*domain.Artifact, error)
}

// TrackerLists connects tasks to the shared list of their tracker. It is used by every path that runs
// tasks (the cloud worker and the user's own devices), so a task behaves the same wherever it runs.
type TrackerLists struct {
	Store  TrackerStore
	Tasks  repository.TaskRepository
	Logger *slog.Logger
	// ApprovedDrafts returns the tasks whose application draft the owner approved. When set, a finished application
	// that never went through approval (an older one) does not count as done, so it can be prepared again.
	ApprovedDrafts func(ctx context.Context, userID uuid.UUID) map[uuid.UUID]bool
}

const trackerReadLimit = 200 << 10

// trackerTypeFor is the tracker a task works on. A follow-up has the type of the task it continues,
// whatever its own wording is.
func (w *TrackerLists) typeFor(ctx context.Context, task *domain.Task) string {
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
	root, err := w.Tasks.GetTaskByID(ctx, id)
	if err != nil || root == nil {
		return ""
	}
	return domain.TrackerTypeOf(root.Title, root.Goal, root.InputPayload)
}

// trackerList finds the shared list of a type among the user's living lists.
func (w *TrackerLists) list(ctx context.Context, userID uuid.UUID, kind, trackerType string) *domain.Artifact {
	arts, err := w.Store.ListByKind(ctx, userID, kind)
	if err != nil {
		return nil
	}
	// If more than one list somehow exists for a type, the newest one is the list.
	var best *domain.Artifact
	for _, a := range arts {
		var m struct {
			Tracker string `json:"tracker"`
		}
		if json.Unmarshal(a.Metadata, &m) == nil && m.Tracker == trackerType && (best == nil || a.CreatedAt.After(best.CreatedAt)) {
			best = a
		}
	}
	if best != nil {
		return best
	}
	return nil
}

// withTrackerList returns a copy of the task whose goal ends with its tracker's current list, so the
// task starts from the shared list instead of from nothing. Nothing is persisted on the task.
// Prepare returns a copy of the task whose goal ends with its tracker's shared list (a task of no tracker
// comes back as it was). Nothing is persisted on the task.
func (w *TrackerLists) Prepare(ctx context.Context, task *domain.Task) *domain.Task {
	if w == nil || w.Store == nil {
		return task
	}
	if domain.IsApplyTask(task.InputPayload) {
		return w.withProfile(ctx, task)
	}
	t := w.typeFor(ctx, task)
	if t == "" {
		return task
	}
	list := w.list(ctx, task.UserID, storage.ListKind, t)
	if list == nil {
		list = w.seed(ctx, task, t)
	}
	current := ""
	if list != nil {
		if text, err := w.Store.ReadText(ctx, task.UserID, list.ID, trackerReadLimit); err == nil {
			current = text
		}
	}
	cp := *task
	cp.Goal = task.Goal + domain.TrackerPrompt(t, current, time.Now())
	return &cp
}

// Save writes the list a task handed back into its tracker's shared list. The list that was there is kept
// as one rolling backup first, so a task that gets it wrong never costs the data. With no list yet, the first
// table a tracker task produced starts it. An answer with no "## Tracker" section leaves the list alone.
func (w *TrackerLists) Save(ctx context.Context, task *domain.Task, answer string) {
	if w == nil || w.Store == nil || strings.TrimSpace(answer) == "" {
		return
	}
	t := w.typeFor(ctx, task)
	if t == "" {
		return
	}
	list := w.list(ctx, task.UserID, storage.ListKind, t)
	table, found := domain.ExtractTrackerTable(answer)
	if !found {
		if list != nil {
			return // the task did not change the list
		}
		if table, found = domain.FirstTable(answer); !found {
			return
		}
	}
	if !domain.PlaybookLooksSafe(table) {
		w.Logger.Warn("Tracker list not saved: it looked like it contained a credential", "task_id", task.ID)
		return
	}
	title := domain.TrackerTypes[t] + " tracker"
	taskID := task.ID
	if list == nil {
		if _, err := w.Store.SaveArtifact(ctx, storage.SaveArtifactInput{
			UserID: task.UserID, TaskID: &taskID, Title: title, Filename: t + "-tracker.md",
			ArtifactType: "MARKDOWN", ContentType: "text/markdown; charset=utf-8",
			Body: bytes.NewReader([]byte(table + "\n")), Metadata: domain.TrackerListMetadata(storage.ListKind, t),
		}); err != nil {
			w.Logger.Warn("Could not start the tracker list", "task_id", task.ID, "error", err)
		}
		return
	}
	w.backup(ctx, task, t, list)
	if _, err := w.Store.UpdateText(ctx, list.ID, task.UserID, "# "+title+"\n\n"+table, false, trackerReadLimit); err != nil {
		w.Logger.Warn("Could not update the tracker list", "task_id", task.ID, "error", err)
	}
}

func (w *TrackerLists) backup(ctx context.Context, task *domain.Task, t string, list *domain.Artifact) {
	old, err := w.Store.ReadText(ctx, task.UserID, list.ID, trackerReadLimit)
	if err != nil || strings.TrimSpace(old) == "" {
		return
	}
	if bak := w.list(ctx, task.UserID, domain.TrackerBackupKind, t); bak != nil {
		_, _ = w.Store.UpdateText(ctx, bak.ID, task.UserID, old, false, trackerReadLimit)
		return
	}
	_, _ = w.Store.SaveArtifact(ctx, storage.SaveArtifactInput{
		UserID: task.UserID, Title: domain.TrackerTypes[t] + " tracker (previous)", Filename: t + "-tracker-previous.md",
		ArtifactType: "MARKDOWN", ContentType: "text/markdown; charset=utf-8",
		Body: bytes.NewReader([]byte(old)), Metadata: domain.TrackerListMetadata(domain.TrackerBackupKind, t),
	})
}

// seedTracker starts a tracker's shared list from what the user already has: the table in the newest
// finished task of that type. Without it the first task to touch a list would find nothing to work on.
func (w *TrackerLists) seed(ctx context.Context, task *domain.Task, t string) *domain.Artifact {
	if w.Tasks == nil {
		return nil
	}
	done := domain.TaskStatus("COMPLETED")
	uid := task.UserID
	tasks, err := w.Tasks.ListTasks(ctx, repository.TaskFilter{UserID: &uid, Status: &done, Limit: 200})
	if err != nil {
		return nil
	}
	var best *domain.Task
	var table string
	for _, c := range tasks {
		if c.ID == task.ID || domain.TrackerTypeOf(c.Title, c.Goal, c.InputPayload) != t {
			continue
		}
		tb, ok := domain.FirstTable(domain.OutputText(c.OutputPayload))
		if !ok || !domain.PlaybookLooksSafe(tb) {
			continue
		}
		if best == nil || c.CreatedAt.After(best.CreatedAt) {
			best, table = c, tb
		}
	}
	if best == nil {
		return nil
	}
	title := domain.TrackerTypes[t] + " tracker"
	list, err := w.Store.SaveArtifact(ctx, storage.SaveArtifactInput{
		UserID: task.UserID, Title: title, Filename: t + "-tracker.md",
		ArtifactType: "MARKDOWN", ContentType: "text/markdown; charset=utf-8",
		Body: bytes.NewReader([]byte("# " + title + "\n\n" + table + "\n")), Metadata: domain.TrackerListMetadata(storage.ListKind, t),
	})
	if err != nil {
		w.Logger.Warn("Could not start the tracker list from earlier results", "task_id", task.ID, "error", err)
		return nil
	}
	return list
}

// withProfile shows an application task the owner's verified profile, so a draft is grounded in real facts and
// carries the real name and links instead of placeholders.
func (w *TrackerLists) withProfile(ctx context.Context, task *domain.Task) *domain.Task {
	arts, err := w.Store.ListByKind(ctx, task.UserID, domain.ProfileKind)
	if err != nil || len(arts) == 0 {
		return task
	}
	best := arts[0]
	for _, a := range arts {
		if a.CreatedAt.After(best.CreatedAt) {
			best = a
		}
	}
	text, err := w.Store.ReadText(ctx, task.UserID, best.ID, trackerReadLimit)
	if err != nil || strings.TrimSpace(text) == "" {
		return task
	}
	cp := *task
	cp.Goal = task.Goal + domain.ApplyProfilePrompt(text)
	return &cp
}

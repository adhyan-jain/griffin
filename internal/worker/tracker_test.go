package worker

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/storage"
)

// memStore is an in-memory PlaybookStore holding living lists.
type memStore struct {
	arts  map[uuid.UUID]*domain.Artifact
	texts map[uuid.UUID]string
}

func newMemStore() *memStore {
	return &memStore{arts: map[uuid.UUID]*domain.Artifact{}, texts: map[uuid.UUID]string{}}
}

func (m *memStore) SaveArtifact(_ context.Context, in storage.SaveArtifactInput) (*domain.Artifact, error) {
	b, _ := io.ReadAll(in.Body)
	a := &domain.Artifact{ID: uuid.New(), Metadata: in.Metadata}
	m.arts[a.ID], m.texts[a.ID] = a, string(b)
	return a, nil
}
func (m *memStore) ListByKind(_ context.Context, _ uuid.UUID, kind string) ([]*domain.Artifact, error) {
	var out []*domain.Artifact
	for _, a := range m.arts {
		var md struct {
			Kind string `json:"kind"`
		}
		if json.Unmarshal(a.Metadata, &md) == nil && md.Kind == kind {
			out = append(out, a)
		}
	}
	return out, nil
}
func (m *memStore) ReadText(_ context.Context, _, id uuid.UUID, _ int64) (string, error) {
	return m.texts[id], nil
}
func (m *memStore) UpdateText(_ context.Context, id, _ uuid.UUID, text string, _ bool, _ int64) (*domain.Artifact, error) {
	m.texts[id] = text
	return m.arts[id], nil
}

const (
	list3 = "| Company | Role | Tier |\n|---|---|---|\n| Acme | SDE Intern | Tier A |\n| Beta | ML Intern | Tier C |\n| Gamma | Ops Intern | Tier C |"
	list1 = "| Company | Role | Tier |\n|---|---|---|\n| Acme | SDE Intern | Tier A |"
)

func trackerWorker(store *memStore) *Worker {
	return &Worker{playbooks: store, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func answer(text string) *ExecutionResult {
	out, _ := json.Marshal(map[string]string{"response": text})
	return &ExecutionResult{TextOutput: text, Output: out}
}

func TestATaskStartsFromTheSharedListAndCanRemoveRows(t *testing.T) {
	store, user := newMemStore(), uuid.New()
	w := trackerWorker(store)
	first := &domain.Task{ID: uuid.New(), UserID: user, Title: "INTERNSHIP TRACKER", Goal: "list companies"}
	w.saveTracker(context.Background(), first, answer("Here you go\n\n"+list3))
	if len(store.arts) != 1 {
		t.Fatalf("the first table must start the list: %d artifacts", len(store.arts))
	}

	// A new task with different wording sees the whole list...
	op := &domain.Task{ID: uuid.New(), UserID: user, Title: "Clean up", Goal: "Remove every Tier C row from the internship list"}
	seen := w.withTrackerList(context.Background(), op)
	if !strings.Contains(seen.Goal, "Gamma") || !strings.Contains(seen.Goal, domain.TrackerSection) {
		t.Fatalf("the task must be shown the shared list: %q", seen.Goal)
	}
	if op.Goal == seen.Goal {
		t.Fatal("the task itself must not be changed")
	}

	// ...and what it hands back replaces the list, so the removal sticks.
	w.saveTracker(context.Background(), op, answer("Removed 2 rows.\n\n## Tracker\n\n"+list1))
	var cur, backup string
	for id, a := range store.arts {
		var m struct {
			Kind string `json:"kind"`
		}
		_ = json.Unmarshal(a.Metadata, &m)
		if m.Kind == storage.ListKind {
			cur = store.texts[id]
		} else {
			backup = store.texts[id]
		}
	}
	if strings.Contains(cur, "Gamma") || !strings.Contains(cur, "Acme") {
		t.Fatalf("the removal did not stick: %q", cur)
	}
	if !strings.Contains(backup, "Gamma") {
		t.Fatalf("the list before the change must be kept as a backup: %q", backup)
	}
}

func TestATaskThatDoesNotChangeTheListLeavesItAlone(t *testing.T) {
	store, user := newMemStore(), uuid.New()
	w := trackerWorker(store)
	w.saveTracker(context.Background(), &domain.Task{ID: uuid.New(), UserID: user, Title: "INTERNSHIP TRACKER", Goal: "x"}, answer(list3))
	w.saveTracker(context.Background(), &domain.Task{ID: uuid.New(), UserID: user, Title: "Summarise internships", Goal: "x"}, answer("Three listings, two are Tier C."))
	for _, text := range store.texts {
		if !strings.Contains(text, "Gamma") {
			t.Fatalf("an answer with no Tracker section must not touch the list: %q", text)
		}
	}
}

func TestOtherTasksAndApplyTasksNeverTouchTheList(t *testing.T) {
	store, user := newMemStore(), uuid.New()
	w := trackerWorker(store)
	other := &domain.Task{ID: uuid.New(), UserID: user, Title: "Weekly brief", Goal: "summarise my week"}
	if w.withTrackerList(context.Background(), other) != other {
		t.Fatal("a task about nothing tracked must be left as it is")
	}
	apply := &domain.Task{ID: uuid.New(), UserID: user, Title: "APPLY - Acme - SDE Intern", Goal: "Apply for the internship", InputPayload: []byte(`{"apply_for":{"company":"Acme"}}`)}
	w.saveTracker(context.Background(), apply, answer("## Tracker\n\n"+list1))
	if len(store.arts) != 0 {
		t.Fatal("an application task works on one posting, not on the list")
	}
}

type seedRepo struct {
	repository.TaskRepository
	tasks []*domain.Task
}

func (r seedRepo) ListTasks(context.Context, repository.TaskFilter) ([]*domain.Task, error) {
	return r.tasks, nil
}

// The first task to touch a tracker finds the user's existing data, not an empty list, and is told the date.
func TestTheListIsSeededFromEarlierResultsOnFirstUse(t *testing.T) {
	store, user := newMemStore(), uuid.New()
	out, _ := json.Marshal(map[string]string{"response": "Here:\n\n" + list3})
	old := &domain.Task{ID: uuid.New(), UserID: user, Title: "INTERNSHIP TRACKER (daily 08:00)", Goal: "x", OutputPayload: out, CreatedAt: time.Now().Add(-time.Hour)}
	w := trackerWorker(store)
	w.taskRepo = seedRepo{tasks: []*domain.Task{old}}

	op := &domain.Task{ID: uuid.New(), UserID: user, Title: "remove the passed internships, i havent applied on any of them btw", Goal: "remove the passed internships, i havent applied on any of them btw"}
	seen := w.withTrackerList(context.Background(), op)
	if !strings.Contains(seen.Goal, "Gamma") || !strings.Contains(seen.Goal, "Today is") {
		t.Fatalf("the task must see the seeded list and today's date: %q", seen.Goal)
	}
	if len(store.arts) != 1 {
		t.Fatalf("exactly one list must be created, got %d", len(store.arts))
	}
	w.withTrackerList(context.Background(), op)
	if len(store.arts) != 1 {
		t.Fatal("seeding twice must not create a second list")
	}
}

package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
)

// fakeCodeQueue mirrors the lease query's rules: due QUEUED/RETRYING, no requires, no lease, not excluded.
type fakeCodeQueue struct{ tasks []*domain.Task }

func (q *fakeCodeQueue) LeaseNextCodeTask(_ context.Context, workerID string, d time.Duration, exclude []uuid.UUID) (*domain.Task, error) {
	skip := map[uuid.UUID]bool{}
	for _, id := range exclude {
		skip[id] = true
	}
	for _, t := range q.tasks {
		if skip[t.ID] || t.LeasedBy != nil || (t.Status != domain.TaskStatusQueued && t.Status != domain.TaskStatusRetrying) {
			continue
		}
		var p struct {
			Requires []string `json:"requires"`
		}
		_ = json.Unmarshal(t.InputPayload, &p)
		if len(p.Requires) > 0 {
			continue
		}
		t.Status, t.LeasedBy = domain.TaskStatusRunning, &workerID
		c := *t
		return &c, nil
	}
	return nil, nil
}

func (q *fakeCodeQueue) ReleaseLeasedTask(_ context.Context, id uuid.UUID, workerID string) (bool, error) {
	for _, t := range q.tasks {
		if t.ID == id && t.LeasedBy != nil && *t.LeasedBy == workerID && t.Status == domain.TaskStatusRunning {
			t.Status, t.LeasedBy, t.LeaseExpiresAt = domain.TaskStatusQueued, nil, nil
			return true, nil
		}
	}
	return false, nil
}

type fakeCodeEngine struct {
	q       *fakeCodeQueue
	onDone  func()
	created int
}

func (e *fakeCodeEngine) CompleteTask(_ context.Context, id uuid.UUID, workerID string, _ json.RawMessage) error {
	for _, t := range e.q.tasks {
		if t.ID == id && t.LeasedBy != nil && *t.LeasedBy == workerID {
			t.Status, t.LeasedBy = domain.TaskStatusCompleted, nil
			if e.onDone != nil {
				e.onDone()
			}
			return nil
		}
	}
	return domain.ErrLeaseExpired
}
func (e *fakeCodeEngine) CancelTask(context.Context, uuid.UUID, string) error { return nil }
func (e *fakeCodeEngine) CreateTask(_ context.Context, t *domain.Task, _ []uuid.UUID) (*domain.Task, error) {
	e.created++
	return t, nil
}

type codeRig struct {
	q    *fakeCodeQueue
	eng  *fakeCodeEngine
	op   *CodeRoutines
	now  time.Time
	user uuid.UUID
}

func newCodeRig(t *testing.T) *codeRig {
	t.Helper()
	r := &codeRig{q: &fakeCodeQueue{}, now: time.Date(2026, 11, 28, 11, 0, 0, 0, domain.ISTZone), user: uuid.New()}
	r.eng = &fakeCodeEngine{q: r.q}
	rem := &Reminders{Tasks: &remStore{}, Outbox: &remOutbox{}, Now: func() time.Time { return r.now }}
	r.op = &CodeRoutines{Queue: r.q, Engine: r.eng, Trackers: &TrackerLists{Reminders: rem}, Now: func() time.Time { return r.now }}
	return r
}

func (r *codeRig) add(title, payload string) *domain.Task {
	if payload == "" {
		payload = "{}"
	}
	t := &domain.Task{ID: uuid.New(), UserID: r.user, Title: title, Goal: title, Status: domain.TaskStatusQueued,
		InputPayload: json.RawMessage(payload), MaxRetries: 3, RetryCount: 1}
	r.q.tasks = append(r.q.tasks, t)
	return t
}

func (r *codeRig) reminder(what string) *domain.Task {
	return r.add(domain.ReminderTitlePrefix+what, `{"kind":"reminder","what":"`+what+`","remind_at":"2026-11-28T10:00:00+05:30"}`)
}

func TestCodeRoutinesCompletesATaskCodeAnswers(t *testing.T) {
	r := newCodeRig(t)
	rem := r.reminder("call mom")
	if err := r.op.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if rem.Status != domain.TaskStatusCompleted || rem.LeasedBy != nil {
		t.Errorf("status %s lease %v", rem.Status, rem.LeasedBy)
	}
}

func TestCodeRoutinesReleasesWhatCodeDoesNotAnswerUntouched(t *testing.T) {
	r := newCodeRig(t)
	model := r.add("Write a cover letter", "")
	rem := r.reminder("after")
	if err := r.op.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if model.Status != domain.TaskStatusQueued || model.LeasedBy != nil || model.RetryCount != 1 {
		t.Errorf("model task not given back as it was: %s lease %v retries %d", model.Status, model.LeasedBy, model.RetryCount)
	}
	if rem.Status != domain.TaskStatusCompleted {
		t.Errorf("a code task behind a model task still ran: %s", rem.Status)
	}
}

func TestCodeRoutinesLeavesTasksThatNeedAHostOrAreLeasedByOne(t *testing.T) {
	r := newCodeRig(t)
	needs := r.reminder("needs hands")
	needs.InputPayload = json.RawMessage(`{"requires":["browser"]}`)
	owner := "host:laptop"
	leased := r.reminder("on a host")
	leased.Status, leased.LeasedBy = domain.TaskStatusRunning, &owner
	if err := r.op.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if needs.Status != domain.TaskStatusQueued || needs.LeasedBy != nil {
		t.Errorf("task with requires touched: %s", needs.Status)
	}
	if leased.Status != domain.TaskStatusRunning || leased.LeasedBy == nil || *leased.LeasedBy != owner {
		t.Errorf("host-leased task touched: %s", leased.Status)
	}
}

func TestCodeRoutinesStopsWhenTheBudgetIsLow(t *testing.T) {
	r := newCodeRig(t)
	r.op.Budget, r.op.MinLeft = 18*time.Second, 3*time.Second
	r.eng.onDone = func() { r.now = r.now.Add(10 * time.Second) } // each task takes 10s
	var all []*domain.Task
	for i := 0; i < 5; i++ {
		all = append(all, r.reminder("r"+string(rune('a'+i))))
	}
	if err := r.op.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	done := 0
	for _, tk := range all {
		if tk.Status == domain.TaskStatusCompleted {
			done++
		}
	}
	if done != 2 { // 18s: starts at 0s and at 10s (8s left), not at 20s
		t.Errorf("completed %d, want 2", done)
	}
}

func TestCodeRoutinesCapsTasksPerTick(t *testing.T) {
	r := newCodeRig(t)
	r.op.MaxTasks = 3
	var all []*domain.Task
	for i := 0; i < 6; i++ {
		all = append(all, r.reminder("r"+string(rune('a'+i))))
	}
	_ = r.op.Run(context.Background())
	done := 0
	for _, tk := range all {
		if tk.Status == domain.TaskStatusCompleted {
			done++
		}
	}
	if done != 3 {
		t.Errorf("completed %d, want 3", done)
	}
}

type fakeDueUsers struct{ ids []uuid.UUID }

func (f fakeDueUsers) UsersWithDueReminders(context.Context, time.Time, int) ([]uuid.UUID, error) {
	return f.ids, nil
}

func TestDeliverDueRemindersDeliversForEveryUserWithOne(t *testing.T) {
	now := time.Date(2026, 11, 28, 11, 0, 0, 0, domain.ISTZone)
	st, ob := &remStore{}, &remOutbox{}
	r := &Reminders{Tasks: st, Due: st, Outbox: ob, Now: func() time.Time { return now }}
	a, _ := r.Create(context.Background(), uuid.New(), "one", now.Add(-time.Minute))
	if err := DeliverDueReminders(context.Background(), r, fakeDueUsers{ids: []uuid.UUID{a.UserID}}); err != nil {
		t.Fatal(err)
	}
	if !st.done[a.ID] || !ob.seen["reminder:"+a.ID.String()] {
		t.Errorf("not delivered: done=%v outbox=%v", st.done, ob.seen)
	}
}

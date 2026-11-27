package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/notify"
)

type remStore struct {
	mu    sync.Mutex
	tasks []*domain.Task
	done  map[uuid.UUID]bool
	keys  map[string]bool
}

func (s *remStore) CreateTask(_ context.Context, t *domain.Task, _ []uuid.UUID) (*domain.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.keys == nil {
		s.keys, s.done = map[string]bool{}, map[uuid.UUID]bool{}
	}
	if s.keys[*t.IdempotencyKey] {
		return nil, domain.ErrIdempotentConflict
	}
	s.keys[*t.IdempotencyKey] = true
	c := *t
	s.tasks = append(s.tasks, &c)
	return &c, nil
}
func (s *remStore) DueReminders(_ context.Context, _ uuid.UUID, now time.Time, _ int) ([]*domain.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*domain.Task
	for _, t := range s.tasks {
		if !s.done[t.ID] && !t.ScheduledFor.After(now) {
			out = append(out, t)
		}
	}
	return out, nil
}
func (s *remStore) FinishReminder(_ context.Context, id uuid.UUID, _ json.RawMessage) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done[id] {
		return false, nil
	}
	s.done[id] = true
	return true, nil
}

type remOutbox struct{ seen map[string]bool }

func (o *remOutbox) Record(_ context.Context, n *domain.Notification) (bool, error) {
	if o.seen == nil {
		o.seen = map[string]bool{}
	}
	if o.seen[n.DedupeKey] {
		return false, nil
	}
	o.seen[n.DedupeKey] = true
	return true, nil
}

type remSender struct {
	msgs []notify.Message
	err  error
}

func (s *remSender) Send(_ context.Context, m notify.Message) error {
	s.msgs = append(s.msgs, m)
	return s.err
}

func TestReminderCreateSchedulesTheDelayedPush(t *testing.T) {
	now := time.Date(2026, 11, 28, 1, 19, 0, 0, domain.ISTZone)
	at := time.Date(2026, 11, 28, 12, 0, 0, 0, domain.ISTZone)
	st, snd, user := &remStore{}, &remSender{}, uuid.New()
	r := &Reminders{Tasks: st, Sender: snd, AppURL: "https://app", Now: func() time.Time { return now }}
	task, err := r.Create(context.Background(), user, "talk to Ankit mama", at)
	if err != nil {
		t.Fatal(err)
	}
	if task.Title != "REMINDER - talk to Ankit mama" || task.ManagerDomain != "personal_ops" || !task.ScheduledFor.Equal(at) {
		t.Errorf("task: %+v", task)
	}
	if len(snd.msgs) != 1 || !snd.msgs[0].At.Equal(at) || snd.msgs[0].ClickURL != "https://app/tasks/"+task.ID.String() {
		t.Errorf("push: %+v", snd.msgs)
	}
	if _, err := r.Create(context.Background(), user, "talk to Ankit mama", at); !errors.Is(err, domain.ErrIdempotentConflict) || len(st.tasks) != 1 || len(snd.msgs) != 1 {
		t.Errorf("a double submit makes one reminder and one push: %v %d %d", err, len(st.tasks), len(snd.msgs))
	}
}

func TestReminderFarAwayIsNotPushedYet(t *testing.T) {
	now := time.Date(2026, 11, 28, 1, 0, 0, 0, domain.ISTZone)
	snd := &remSender{}
	r := &Reminders{Tasks: &remStore{}, Sender: snd, Now: func() time.Time { return now }}
	if _, err := r.Create(context.Background(), uuid.New(), "x", now.AddDate(0, 0, 10)); err != nil || len(snd.msgs) != 0 {
		t.Errorf("err %v pushes %d", err, len(snd.msgs))
	}
}

func TestDeliverDueWritesOneOutboxRowAndCompletesOnce(t *testing.T) {
	now := time.Date(2026, 11, 28, 11, 0, 0, 0, domain.ISTZone)
	st, ob, snd, user := &remStore{}, &remOutbox{}, &remSender{}, uuid.New()
	r := &Reminders{Tasks: st, Due: st, Outbox: ob, Sender: snd, Now: func() time.Time { return now }}
	due, _ := r.Create(context.Background(), user, "due one", now.Add(-time.Minute)) // pushed=false path is not taken: Sender set, so pushed
	_ = due
	later, _ := r.Create(context.Background(), user, "later one", now.Add(time.Hour))
	r.DeliverDue(context.Background(), user)
	r.DeliverDue(context.Background(), user)
	if len(ob.seen) != 1 || !ob.seen["reminder:"+due.ID.String()] || ob.seen["reminder:"+later.ID.String()] {
		t.Errorf("outbox: %v", ob.seen)
	}
	if !st.done[due.ID] || st.done[later.ID] {
		t.Errorf("done: %v", st.done)
	}
}

func TestDeliverDueSendsWhenNoDelayedPushWasScheduled(t *testing.T) {
	now := time.Date(2026, 11, 28, 11, 0, 0, 0, domain.ISTZone)
	st, ob, user := &remStore{}, &remOutbox{}, uuid.New()
	// Created with no sender (as in production without ntfy), then delivered with a sender present.
	task, _ := (&Reminders{Tasks: st}).Create(context.Background(), user, "call mom", now.Add(-time.Second))
	snd := &remSender{}
	r := &Reminders{Due: st, Outbox: ob, Sender: snd, Now: func() time.Time { return now }}
	r.DeliverDue(context.Background(), user)
	r.DeliverDue(context.Background(), user)
	if len(snd.msgs) != 1 || !st.done[task.ID] {
		t.Errorf("pushes %d done %v", len(snd.msgs), st.done)
	}
}

func TestRemindHookAnswersAndSharesTheOutboxKey(t *testing.T) {
	st, ob, user := &remStore{}, &remOutbox{}, uuid.New()
	r := &Reminders{Tasks: st, Due: st, Outbox: ob, Now: time.Now}
	task, _ := r.Create(context.Background(), user, "call mom", time.Now().Add(time.Hour))
	w := &TrackerLists{Reminders: r}
	answer, how := w.RunInCode(context.Background(), task, nil, nil)
	if how != Answered || answer != "Reminded you: call mom." || !ob.seen["reminder:"+task.ID.String()] {
		t.Errorf("%q %v %v", answer, how, ob.seen)
	}
	if _, ok := w.Remind(context.Background(), &domain.Task{Title: "something else"}); ok {
		t.Error("only reminders")
	}
}

func TestCallDetectorIgnoresReminders(t *testing.T) {
	if wantsCall("remind me to call ankit mama tomorrow at noon") {
		t.Error("a reminder to call is not a call")
	}
	if !wantsCall("call ankit mama") {
		t.Error("a plain call is still a call")
	}
}

func TestReminderTaskFinishedIsNotNews(t *testing.T) {
	snd := &remSender{}
	n := &TaskNotifier{Sender: snd}
	n.TaskFinished(context.Background(), &domain.Task{Title: "REMINDER - x", Status: domain.TaskStatusCompleted})
	if len(snd.msgs) != 0 {
		t.Error("the reminder was the push")
	}
}

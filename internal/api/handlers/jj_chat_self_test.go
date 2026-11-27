package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/notify"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/service"
)

type selfFakeTasks struct{}

func (selfFakeTasks) ListTasks(context.Context, repository.TaskFilter) ([]*domain.Task, error) {
	return nil, nil
}
func (selfFakeTasks) CancelTask(context.Context, uuid.UUID, string) error { return nil }
func (selfFakeTasks) ResumeTask(context.Context, uuid.UUID) error         { return nil }

type selfFakeApprovals struct {
	list    []*domain.Approval
	decided []uuid.UUID
	err     error
}

func (f *selfFakeApprovals) ListApprovals(_ context.Context, user uuid.UUID, _ *domain.ApprovalStatus) ([]*domain.Approval, error) {
	if f.err != nil {
		return nil, f.err
	}
	var out []*domain.Approval
	for _, a := range f.list {
		if a.UserID == user {
			out = append(out, a)
		}
	}
	return out, nil
}
func (f *selfFakeApprovals) RespondApproval(_ context.Context, id, _ uuid.UUID, _ bool, _ string) (*domain.Approval, error) {
	f.decided = append(f.decided, id)
	return &domain.Approval{ID: id}, nil
}

func newSelfChat(t *testing.T) (*JJChatHandler, *fakeChatStore, *selfFakeApprovals, *chatWake, uuid.UUID) {
	t.Helper()
	user := uuid.New()
	ap := &selfFakeApprovals{list: []*domain.Approval{
		{ID: uuid.New(), UserID: user, Summary: "Message Coy", Status: domain.ApprovalStatusPending},
		{ID: uuid.New(), UserID: user, Summary: "Email Acme", Status: domain.ApprovalStatusPending},
		{ID: uuid.New(), UserID: uuid.New(), Summary: "Someone else's", Status: domain.ApprovalStatusPending},
	}}
	wake := &chatWake{}
	h, store, _ := newChat(wake, true)
	h.WithSelfCommands(&service.SelfCommands{Tasks: selfFakeTasks{}, Approvals: ap})
	return h, store, ap, wake, user
}

func TestChat_SelfCommandApproveAllRunsBeforeAnyTaskIsQueued(t *testing.T) {
	h, store, ap, wake, user := newSelfChat(t)
	code, env := sendChat(t, h, user, `{"message":"approve all pending approvals"}`, nil)
	require.Equal(t, 200, code)
	assert.Equal(t, "conversation", env.Data.Kind)
	assert.Contains(t, env.Data.Response, "Approved 2 pending approvals")
	assert.Contains(t, env.Data.Response, "Message Coy")
	assert.Len(t, ap.decided, 2, "only the owner's own approvals")
	assert.Empty(t, store.tasks, "no model task is queued for a self command")
	assert.Equal(t, 1, wake.calls, "the runner is woken so approved work continues")
}

func TestChat_SelfCommandNotRunForQuestionsOrOtherText(t *testing.T) {
	for _, msg := range []string{"should I approve all?", "what is approval", "tell me a joke"} {
		h, store, ap, _, user := newSelfChat(t)
		body, _ := json.Marshal(map[string]string{"message": msg})
		code, _ := sendChat(t, h, user, string(body), nil)
		assert.Less(t, code, 300, msg)
		assert.Empty(t, ap.decided, msg)
		assert.Len(t, store.tasks, 1, "%s falls through to the normal chat task", msg)
	}
}

func TestChat_SelfCommandSkippedWhenNewTaskForced(t *testing.T) {
	h, store, ap, _, user := newSelfChat(t)
	sendChat(t, h, user, `{"message":"approve all pending","new_task":true}`, nil)
	assert.Empty(t, ap.decided)
	assert.NotEmpty(t, store.tasks)
}

func TestChat_SelfCommandFailureChangesNothingAndSaysSo(t *testing.T) {
	h, store, ap, wake, user := newSelfChat(t)
	ap.err = errors.New("db down")
	code, env := sendChat(t, h, user, `{"message":"approve all pending"}`, nil)
	require.Equal(t, 200, code)
	assert.Contains(t, env.Data.Response, "nothing was changed")
	assert.Empty(t, ap.decided)
	assert.Empty(t, store.tasks)
	assert.Equal(t, 0, wake.calls)
}

type chatRemSender struct{ msgs []notify.Message }

func (s *chatRemSender) Send(_ context.Context, m notify.Message) error {
	s.msgs = append(s.msgs, m)
	return nil
}

func TestJJChat_ReminderIsCreatedInlineAtTheOwnersMorning(t *testing.T) {
	h, store, obj := newChat(nil, false)
	snd := &chatRemSender{}
	now := time.Date(2026, 11, 28, 1, 19, 0, 0, domain.ISTZone)
	h.now = func() time.Time { return now }
	h.WithReminders(&service.Reminders{Tasks: store, Sender: snd, Now: func() time.Time { return now }})
	user := uuid.New()
	code, env := sendChat(t, h, user, `{"message":"Give me a notification to remind me that I need to talk to Ankit mama tomorrow at 12 in the noon"}`, nil)
	require.Equal(t, 200, code)
	require.Len(t, store.tasks, 1)
	want := time.Date(2026, 11, 28, 12, 0, 0, 0, domain.ISTZone)
	assert.True(t, store.tasks[0].ScheduledFor.Equal(want))
	assert.Equal(t, "REMINDER - talk to Ankit mama", store.tasks[0].Title)
	assert.Equal(t, 0, obj.calls)
	assert.Len(t, snd.msgs, 1)
	assert.Contains(t, env.Data.Response, "Done. I'll remind you to talk to Ankit mama today at 12:00 pm.")
}

func TestJJChat_ReminderWithoutATimeAsks(t *testing.T) {
	h, store, _ := newChat(nil, false)
	h.WithReminders(&service.Reminders{Tasks: store})
	_, env := sendChat(t, h, uuid.New(), `{"message":"remind me to call mom tomorrow"}`, nil)
	assert.Empty(t, store.tasks)
	assert.Contains(t, env.Data.Response, "When should I remind you to call mom?")
}

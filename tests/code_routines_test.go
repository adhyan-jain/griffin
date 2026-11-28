package tests

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/api/handlers"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/runner"
	"github.com/griffin/core/internal/service"
)

type memOutbox struct{ n int }

func (o *memOutbox) Record(context.Context, *domain.Notification) (bool, error) {
	o.n++
	return true, nil
}

func TestLeaseNextCodeTask_SkipsRequiresHostLeasedAndFutureAndReleasesUntouched(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	user := CreateTestUser(t, env.DB, "code_lease@example.com")
	past, future := time.Now().Add(-time.Minute), time.Now().Add(time.Hour)

	mk := func(title, payload string, at *time.Time) *domain.Task {
		tk, err := env.Engine.CreateTask(ctx, &domain.Task{UserID: user, Title: title, Goal: title,
			InputPayload: json.RawMessage(payload), ScheduledFor: at}, nil)
		require.NoError(t, err)
		return tk
	}
	needs := mk("needs a browser", `{"requires":["browser"]}`, nil)
	later := mk("not yet", `{}`, &future)
	hostTask := mk("host has it", `{}`, nil)
	_, err := env.TaskRepo.ClaimNextTaskForHost(ctx, user, "host:laptop", []string{"agent"}, time.Minute)
	require.NoError(t, err) // claims the oldest code task: hostTask
	due := mk("due", `{}`, &past)

	got, err := codeRepo(env).LeaseNextCodeTask(ctx, "tick:code", time.Minute, nil)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, due.ID, got.ID, "only the due, requires-free, unleased task is leased")
	assert.Equal(t, domain.TaskStatusRunning, got.Status)

	none, err := codeRepo(env).LeaseNextCodeTask(ctx, "tick:code", time.Minute, nil)
	require.NoError(t, err)
	assert.Nil(t, none)

	ok, err := codeRepo(env).ReleaseLeasedTask(ctx, due.ID, "someone-else")
	require.NoError(t, err)
	assert.False(t, ok, "only the lease owner can release")
	ok, err = codeRepo(env).ReleaseLeasedTask(ctx, due.ID, "tick:code")
	require.NoError(t, err)
	assert.True(t, ok)

	back, err := env.TaskRepo.GetTaskByID(ctx, due.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskStatusQueued, back.Status)
	assert.Nil(t, back.LeasedBy)
	assert.Nil(t, back.LeaseExpiresAt)
	assert.Equal(t, due.RetryCount, back.RetryCount)
	assert.Nil(t, back.StartedAt)

	// Excluding it, nothing else is eligible.
	none, err = codeRepo(env).LeaseNextCodeTask(ctx, "tick:code", time.Minute, []uuid.UUID{due.ID})
	require.NoError(t, err)
	assert.Nil(t, none)

	for _, id := range []uuid.UUID{needs.ID, later.ID} {
		tk, _ := env.TaskRepo.GetTaskByID(ctx, id)
		assert.Equal(t, domain.TaskStatusQueued, tk.Status)
		assert.Nil(t, tk.LeasedBy)
	}
	ht, _ := env.TaskRepo.GetTaskByID(ctx, hostTask.ID)
	require.NotNil(t, ht.LeasedBy)
	assert.Equal(t, "host:laptop", *ht.LeasedBy)
}

func TestUsersWithDueReminders(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	a := CreateTestUser(t, env.DB, "rem_a@example.com")
	b := CreateTestUser(t, env.DB, "rem_b@example.com")
	past, future := time.Now().Add(-time.Minute), time.Now().Add(time.Hour)
	for _, c := range []struct {
		u     uuid.UUID
		title string
		at    time.Time
	}{{a, domain.ReminderTitlePrefix + "due", past}, {a, domain.ReminderTitlePrefix + "due too", past}, {b, domain.ReminderTitlePrefix + "later", future}, {b, "plain task", past}} {
		at := c.at
		_, err := env.Engine.CreateTask(ctx, &domain.Task{UserID: c.u, Title: c.title, Goal: c.title, ScheduledFor: &at}, nil)
		require.NoError(t, err)
	}
	ids, err := codeRepo(env).UsersWithDueReminders(ctx, time.Now(), 50)
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{a}, ids)
}

// A due reminder is finished by the tick's own ops, so the tick finds nothing runnable and does not wake EC2.
func TestTickRunsCodeRoutinesBeforeCountingRunnable(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	user := CreateTestUser(t, env.DB, "tick_code@example.com")
	ob := &memOutbox{}
	rem := &service.Reminders{Tasks: env.Engine, Due: env.TaskRepo.(service.ReminderDue), Outbox: ob}
	at := time.Now().Add(-time.Minute)
	payload, _ := json.Marshal(map[string]any{"kind": "reminder", "what": "stretch", "remind_at": at.Format(time.RFC3339)})
	_, err := env.Engine.CreateTask(ctx, &domain.Task{UserID: user, Title: domain.ReminderTitlePrefix + "stretch", Goal: "stretch",
		ManagerDomain: "personal_ops", InputPayload: payload, ScheduledFor: &at}, nil)
	require.NoError(t, err)

	routines := &service.CodeRoutines{Queue: codeRepo(env), Engine: env.Engine, Trackers: &service.TrackerLists{Reminders: rem}}
	mgr := runner.NewMockInstanceManager(runner.RunnerStateStopped)
	wake := runner.NewWakeupController(mgr, runner.WakeupConfig{}, nil)
	h := handlers.NewSchedulerHandler(env.Engine, env.TaskRepo, nil, wake, nil).WithProactive(
		handlers.ProactiveOp{Name: "code_routines", Run: routines.Run})
	code, resp := doTick(t, h)
	assert.Equal(t, 200, code)
	assert.Equal(t, "ok", resp.Operations["code_routines"])
	assert.Equal(t, 0, resp.RunnableTasks)
	assert.False(t, resp.WakeupTriggered)
	assert.Equal(t, 0, mgr.StartCalls)
	assert.Equal(t, 1, ob.n)
}

func codeRepo(env *TestEnv) interface {
	service.CodeQueue
	UsersWithDueReminders(ctx context.Context, now time.Time, limit int) ([]uuid.UUID, error)
} {
	return env.TaskRepo.(interface {
		service.CodeQueue
		UsersWithDueReminders(ctx context.Context, now time.Time, limit int) ([]uuid.UUID, error)
	})
}

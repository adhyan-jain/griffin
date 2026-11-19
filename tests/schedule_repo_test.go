package tests

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/api/handlers"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/runner"
)

func TestScheduleRepo_CRUD(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "sched_crud@example.com")

	repo := repository.NewPostgresScheduleRepository(env.DB.Pool)

	now := time.Now().UTC()
	s := &domain.Schedule{
		UserID:           userID,
		ManagerDomain:    "JJ",
		Name:             "Daily Morning Digest",
		CronExpression:   "daily 08:00 IST",
		Timezone:         "Asia/Kolkata",
		TaskSpecTemplate: json.RawMessage(`{"title":"Morning Briefing","goal":"Summarise inbox and calendar"}`),
		IsPaused:         false,
		NextRunAt:        now.Add(1 * time.Hour),
	}

	created, err := repo.CreateSchedule(ctx, s)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, created.ID)
	assert.Equal(t, s.Name, created.Name)
	assert.False(t, created.IsPaused)

	// Get
	got, err := repo.GetScheduleByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, got.ID)
	assert.Equal(t, "daily 08:00 IST", got.CronExpression)

	// List
	list, err := repo.ListSchedules(ctx, userID)
	require.NoError(t, err)
	assert.Len(t, list, 1)

	// Pause
	err = repo.SetPaused(ctx, created.ID, userID, true)
	require.NoError(t, err)
	got, err = repo.GetScheduleByID(ctx, created.ID)
	require.NoError(t, err)
	assert.True(t, got.IsPaused)

	// Delete
	err = repo.DeleteSchedule(ctx, created.ID, userID)
	require.NoError(t, err)
	_, err = repo.GetScheduleByID(ctx, created.ID)
	assert.ErrorIs(t, err, repository.ErrScheduleNotFound)
}

func TestSchedulerTick_MaterializesDueScheduleExactlyOnce(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "sched_tick@example.com")

	schedRepo := repository.NewPostgresScheduleRepository(env.DB.Pool)

	now := time.Now().UTC()
	pastDue := now.Add(-5 * time.Minute)

	s := &domain.Schedule{
		UserID:           userID,
		ManagerDomain:    "JJ",
		Name:             "Scout internships",
		CronExpression:   "daily 08:00 IST",
		Timezone:         "Asia/Kolkata",
		TaskSpecTemplate: json.RawMessage(`{"title":"Scout internships","goal":"Find active Tier A internships","input_payload":{"category":"internship"}}`),
		IsPaused:         false,
		NextRunAt:        pastDue,
	}
	created, err := schedRepo.CreateSchedule(ctx, s)
	require.NoError(t, err)

	mgr := runner.NewMockInstanceManager(runner.RunnerStateStopped)
	wake := runner.NewWakeupController(mgr, runner.WakeupConfig{}, nil)
	h := handlers.NewSchedulerHandler(env.Engine, env.TaskRepo, schedRepo, wake, nil)

	// 10 concurrent ticks race for the due schedule
	var wg sync.WaitGroup
	var totalMaterialized int
	var mu sync.Mutex

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, resp := doTick(t, h)
			assert.Equal(t, 200, code)
			mu.Lock()
			totalMaterialized += resp.MaterializedJobs
			mu.Unlock()
		}()
	}
	wg.Wait()

	// Exactly 1 task must have been materialized
	assert.Equal(t, 1, totalMaterialized, "exactly one task must be materialized across concurrent ticks")

	// Schedule's next_run_at must have been advanced to future
	updatedSched, err := schedRepo.GetScheduleByID(ctx, created.ID)
	require.NoError(t, err)
	assert.True(t, updatedSched.NextRunAt.After(now), "next_run_at must advance into the future")
	require.NotNil(t, updatedSched.LastRunAt)

	// Task must exist in queue with schedule_id linked
	tasks, err := env.TaskRepo.ListTasks(ctx, repository.TaskFilter{UserID: &userID})
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	assert.Equal(t, "Scout internships", tasks[0].Title)
	assert.Equal(t, domain.TaskStatusQueued, tasks[0].Status)
	assert.Contains(t, string(tasks[0].InputPayload), created.ID.String())
}

func TestSchedulerTick_PausedScheduleNotMaterialized(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "sched_paused@example.com")

	schedRepo := repository.NewPostgresScheduleRepository(env.DB.Pool)

	now := time.Now().UTC()
	pastDue := now.Add(-5 * time.Minute)

	s := &domain.Schedule{
		UserID:         userID,
		ManagerDomain:  "JJ",
		Name:           "Paused job",
		CronExpression: "daily 08:00 IST",
		Timezone:       "Asia/Kolkata",
		IsPaused:       true,
		NextRunAt:      pastDue,
	}
	_, err := schedRepo.CreateSchedule(ctx, s)
	require.NoError(t, err)

	mgr := runner.NewMockInstanceManager(runner.RunnerStateStopped)
	wake := runner.NewWakeupController(mgr, runner.WakeupConfig{}, nil)
	h := handlers.NewSchedulerHandler(env.Engine, env.TaskRepo, schedRepo, wake, nil)

	code, resp := doTick(t, h)
	assert.Equal(t, 200, code)
	assert.Equal(t, 0, resp.MaterializedJobs)
	assert.Equal(t, 0, resp.RunnableTasks)

	tasks, err := env.TaskRepo.ListTasks(ctx, repository.TaskFilter{UserID: &userID})
	require.NoError(t, err)
	assert.Empty(t, tasks)
}

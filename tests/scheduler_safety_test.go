package tests

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/api/handlers"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/runner"
	"github.com/griffin/core/internal/service"
)

func doTick(t *testing.T, h *handlers.SchedulerHandler) (int, handlers.TickResponse) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.Tick(rec, httptest.NewRequest(http.MethodPost, "/api/v1/scheduler/tick", nil))
	var env struct {
		Data handlers.TickResponse `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	return rec.Code, env.Data
}

// Concurrent ticks + duplicate wake-ups must start the runner exactly once (cooldown).
func TestSchedulerSafety_ConcurrentTicksStartRunnerOnce(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "tick_conc@example.com")
	_, err := env.Engine.CreateTask(ctx, &domain.Task{UserID: userID, Title: "t", Goal: "g"}, nil)
	require.NoError(t, err)

	mgr := runner.NewMockInstanceManager(runner.RunnerStateStopped)
	wake := runner.NewWakeupController(mgr, runner.WakeupConfig{CooldownWindow: time.Minute}, nil)
	h := handlers.NewSchedulerHandler(env.Engine, env.TaskRepo, wake)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, resp := doTick(t, h)
			assert.Equal(t, http.StatusOK, code)
			assert.Equal(t, 1, resp.RunnableTasks)
			assert.True(t, resp.WakeupTriggered)
		}()
	}
	wg.Wait()
	assert.Equal(t, 1, mgr.StartCalls, "20 concurrent ticks must produce a single StartInstances call")
}

// No runnable work -> tick must not wake the runner (scale-to-zero stays at zero).
func TestSchedulerSafety_EmptyQueueNoWake(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	mgr := runner.NewMockInstanceManager(runner.RunnerStateStopped)
	wake := runner.NewWakeupController(mgr, runner.WakeupConfig{}, nil)
	h := handlers.NewSchedulerHandler(env.Engine, env.TaskRepo, wake)
	code, resp := doTick(t, h)
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, 0, resp.RunnableTasks)
	assert.False(t, resp.WakeupTriggered)
	assert.Equal(t, 0, mgr.StartCalls)
}

// A failing wake-up must not be reported as triggered (was swallowed before).
func TestSchedulerSafety_WakeupFailureSurfaced(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "tick_fail@example.com")
	_, err := env.Engine.CreateTask(ctx, &domain.Task{UserID: userID, Title: "t", Goal: "g"}, nil)
	require.NoError(t, err)

	mgr := runner.NewMockInstanceManager(runner.RunnerStateStopped)
	mgr.FailOnStart = true
	h := handlers.NewSchedulerHandler(env.Engine, env.TaskRepo, runner.NewWakeupController(mgr, runner.WakeupConfig{}, nil))
	code, _ := doTick(t, h)
	assert.GreaterOrEqual(t, code, 500)
}

// scheduled_for state is DB-backed: a task scheduled in the future survives a
// "restart" (fresh repo/engine/handler), does not wake the runner early, and is
// picked up and woken for once due.
func TestSchedulerSafety_ScheduledTaskSurvivesRestart(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "sched_restart@example.com")

	due := time.Now().UTC().Add(2 * time.Second)
	created, err := env.Engine.CreateTask(ctx, &domain.Task{UserID: userID, Title: "later", Goal: "g", ScheduledFor: &due}, nil)
	require.NoError(t, err)

	// Simulate process restart: brand new objects over the same database.
	repo2 := repository.NewPostgresTaskRepository(env.DB.Pool)
	engine2 := service.NewTaskEngine(repo2, env.ApprovalRepo, nil)
	mgr := runner.NewMockInstanceManager(runner.RunnerStateStopped)
	h := handlers.NewSchedulerHandler(engine2, repo2, runner.NewWakeupController(mgr, runner.WakeupConfig{CooldownWindow: time.Millisecond}, nil))

	_, resp := doTick(t, h)
	assert.Equal(t, 0, resp.RunnableTasks, "future-scheduled task must not count as runnable (would block scale-to-zero)")
	assert.False(t, resp.WakeupTriggered)
	claimed, err := engine2.ClaimTask(ctx, "w1", time.Minute)
	require.NoError(t, err)
	assert.Nil(t, claimed, "must not be claimable before scheduled_for")

	time.Sleep(2200 * time.Millisecond)

	_, resp = doTick(t, h)
	assert.Equal(t, 1, resp.RunnableTasks)
	assert.True(t, resp.WakeupTriggered)
	assert.Equal(t, 1, mgr.StartCalls)
	claimed, err = engine2.ClaimTask(ctx, "w1", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	assert.Equal(t, created.ID, claimed.ID)
}

// Runner shut down mid-lease: the lease expires, the reaper reclaims, and the task
// is re-run by exactly one worker (not lost, not double-run); the stale worker
// cannot complete it afterwards.
func TestSchedulerSafety_MidLeaseShutdownReclaimedOnce(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "midlease@example.com")
	created, err := env.Engine.CreateTask(ctx, &domain.Task{UserID: userID, Title: "t", Goal: "g", MaxRetries: 3}, nil)
	require.NoError(t, err)

	first, err := env.Engine.ClaimTask(ctx, "worker-dead", 200*time.Millisecond)
	require.NoError(t, err)
	require.NotNil(t, first)

	// While the lease is live nobody else can take it, and the reaper leaves it alone.
	other, err := env.Engine.ClaimTask(ctx, "worker-2", time.Minute)
	require.NoError(t, err)
	assert.Nil(t, other)
	n, err := env.Engine.ReapExpiredLeases(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), n)

	// Runner dies: no heartbeats. A tick sees in-flight work and wakes the runner.
	mgr := runner.NewMockInstanceManager(runner.RunnerStateStopped)
	h := handlers.NewSchedulerHandler(env.Engine, env.TaskRepo, runner.NewWakeupController(mgr, runner.WakeupConfig{}, nil))
	time.Sleep(400 * time.Millisecond)
	_, resp := doTick(t, h)
	assert.True(t, resp.WakeupTriggered, "expired-lease RUNNING task must wake the runner")

	// Reaper runs twice (duplicate): only the first reclaims.
	n1, err := env.Engine.ReapExpiredLeases(ctx)
	require.NoError(t, err)
	n2, err := env.Engine.ReapExpiredLeases(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), n1)
	assert.Equal(t, int64(0), n2)

	// Many workers race for the reclaimed task: exactly one wins.
	var wins int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			c, err := env.Engine.ClaimTask(ctx, "worker-new", time.Minute)
			if err == nil && c != nil {
				atomic.AddInt32(&wins, 1)
			}
		}(i)
	}
	wg.Wait()
	assert.Equal(t, int32(1), wins, "reclaimed task must be run by exactly one worker")

	// The dead worker's late completion must be rejected.
	err = env.Engine.CompleteTask(ctx, created.ID, "worker-dead", nil)
	assert.Error(t, err)

	got, err := env.Engine.GetTask(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskStatusRunning, got.Status)
	assert.Equal(t, 1, got.RetryCount)
}

// Retries are exhausted -> FAILED, never silently re-queued forever.
func TestSchedulerSafety_ReaperExhaustsRetries(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "exhaust@example.com")
	created, err := env.Engine.CreateTask(ctx, &domain.Task{UserID: userID, Title: "t", Goal: "g", MaxRetries: 1}, nil)
	require.NoError(t, err)
	c, err := env.Engine.ClaimTask(ctx, "w", 50*time.Millisecond)
	require.NoError(t, err)
	require.NotNil(t, c)
	time.Sleep(150 * time.Millisecond)
	_, err = env.Engine.ReapExpiredLeases(ctx)
	require.NoError(t, err)
	got, err := env.Engine.GetTask(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskStatusFailed, got.Status)
	again, err := env.Engine.ClaimTask(ctx, "w", time.Minute)
	require.NoError(t, err)
	assert.Nil(t, again)
}

// Regression: a retryable failure (RETRYING) must become claimable again after
// its backoff; previously only QUEUED tasks were ever claimed, so retries were lost.
func TestSchedulerSafety_RetryingTaskIsReclaimedAfterBackoff(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "retrying@example.com")
	created, err := env.Engine.CreateTask(ctx, &domain.Task{UserID: userID, Title: "t", Goal: "g", MaxRetries: 3}, nil)
	require.NoError(t, err)
	c, err := env.Engine.ClaimTask(ctx, "w1", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, c)
	require.NoError(t, env.TaskRepo.FailTask(ctx, created.ID, "w1", "boom", true, 300*time.Millisecond))

	early, err := env.Engine.ClaimTask(ctx, "w2", time.Minute)
	require.NoError(t, err)
	assert.Nil(t, early, "must respect backoff")
	time.Sleep(450 * time.Millisecond)
	again, err := env.Engine.ClaimTask(ctx, "w2", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, again)
	assert.Equal(t, created.ID, again.ID)
}

// Idempotency key holds under concurrent submissions: exactly one task is created.
func TestSchedulerSafety_ConcurrentIdempotentCreate(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "idem_conc@example.com")
	key := "idem-conc-key"

	var ok, conflict int32
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := env.Engine.CreateTask(ctx, &domain.Task{UserID: userID, Title: "t", Goal: "g", IdempotencyKey: &key}, nil)
			switch {
			case err == nil:
				atomic.AddInt32(&ok, 1)
			case errors.Is(err, domain.ErrIdempotentConflict):
				atomic.AddInt32(&conflict, 1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	assert.Equal(t, int32(1), ok)
	assert.Equal(t, int32(9), conflict)
}

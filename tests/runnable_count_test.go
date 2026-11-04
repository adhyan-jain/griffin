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
)

func createJJRoot(t *testing.T, env *TestEnv, userID uuid.UUID) *domain.Task {
	t.Helper()
	leased := "JJ_ORCHESTRATOR"
	root, err := env.Engine.CreateTask(context.Background(), &domain.Task{
		UserID: userID, Title: "root", Goal: "g", ManagerDomain: "JJ",
		Status: domain.TaskStatusRunning, LeasedBy: &leased,
	}, nil)
	require.NoError(t, err)
	return root
}

func createChild(t *testing.T, env *TestEnv, userID uuid.UUID, root *domain.Task) *domain.Task {
	t.Helper()
	c, err := env.Engine.CreateTask(context.Background(), &domain.Task{
		UserID: userID, Title: "child", Goal: "g", ParentID: &root.ID, RootID: &root.ID,
	}, nil)
	require.NoError(t, err)
	return c
}

func completeChild(t *testing.T, env *TestEnv, workerID string) {
	t.Helper()
	ctx := context.Background()
	claimed, err := env.Engine.ClaimTask(ctx, workerID, time.Minute)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.NoError(t, env.Engine.CompleteTask(ctx, claimed.ID, workerID, json.RawMessage(`{"response":"x"}`)))
}

func countRunnable(t *testing.T, env *TestEnv) int64 {
	t.Helper()
	n, err := env.TaskRepo.CountRunnableTasks(context.Background())
	require.NoError(t, err)
	return n
}

func TestCountRunnable_FinishedOrchestrationRootDoesNotCount(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	userID := CreateTestUser(t, env.DB, "count_a@example.com")
	root := createJJRoot(t, env, userID)
	createChild(t, env, userID, root)
	createChild(t, env, userID, root)
	completeChild(t, env, "griffin-worker-1")
	completeChild(t, env, "griffin-worker-1")

	assert.Equal(t, int64(0), countRunnable(t, env))
}

func TestCountRunnable_RootPlusRunningChildCountsOne(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	userID := CreateTestUser(t, env.DB, "count_b@example.com")
	root := createJJRoot(t, env, userID)
	createChild(t, env, userID, root)
	_, err := env.Engine.ClaimTask(context.Background(), "griffin-worker-1", time.Minute)
	require.NoError(t, err)

	assert.Equal(t, int64(1), countRunnable(t, env))
}

func TestCountRunnable_RootPlusQueuedChildCountsChildOnly(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	userID := CreateTestUser(t, env.DB, "count_c@example.com")
	root := createJJRoot(t, env, userID)
	createChild(t, env, userID, root)

	assert.Equal(t, int64(1), countRunnable(t, env))
}

func TestCountRunnable_NormalRunningTaskStillCounts(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	userID := CreateTestUser(t, env.DB, "count_d@example.com")
	_, err := env.Engine.CreateTask(context.Background(), &domain.Task{UserID: userID, Title: "t", Goal: "g"}, nil)
	require.NoError(t, err)
	_, err = env.Engine.ClaimTask(context.Background(), "griffin-worker-1", time.Minute)
	require.NoError(t, err)

	assert.Equal(t, int64(1), countRunnable(t, env))
}

func TestSchedulerTick_OnlyFinishedRootDoesNotWakeRunner(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	userID := CreateTestUser(t, env.DB, "count_e@example.com")
	createJJRoot(t, env, userID)

	mgr := runner.NewMockInstanceManager(runner.RunnerStateStopped)
	h := handlers.NewSchedulerHandler(env.Engine, env.TaskRepo, runner.NewWakeupController(mgr, runner.WakeupConfig{}, nil))
	code, resp := doTick(t, h)
	assert.Equal(t, 200, code)
	assert.Equal(t, 0, resp.RunnableTasks)
	assert.False(t, resp.WakeupTriggered)
	assert.Equal(t, 0, mgr.StartCalls)
}

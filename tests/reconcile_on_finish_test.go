package tests

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/worker"
)

func newReconcilingWorker(env *TestEnv, id string, exec worker.TaskExecutor) *worker.Worker {
	return worker.NewWorker(worker.Config{
		ID:                id,
		Engine:            env.Engine,
		TaskRepo:          env.TaskRepo,
		Executor:          exec,
		LeaseDuration:     10 * time.Second,
		HeartbeatInterval: time.Second,
		OnTaskFinished: func(ctx context.Context, taskID uuid.UUID) {
			_ = env.Orchestrator.ReconcileByChildTask(ctx, taskID)
		},
	})
}

func submitResearchForReconcile(t *testing.T, env *TestEnv, userID uuid.UUID) *domain.OrchestrationRecord {
	t.Helper()
	orch, _, err := env.Orchestrator.SubmitRequest(context.Background(), &domain.UserRequest{
		UserID: userID, RequestText: "Research Linux kernel cgroup v2 limits", Priority: domain.TaskPriorityNormalP2,
	})
	require.NoError(t, err)
	return orch
}

func TestWorkerFinishReconcilesOrchestrationWithoutGET(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "reconcile_ok@example.com")
	orch := submitResearchForReconcile(t, env, userID)

	w := newReconcilingWorker(env, "w-rec", &fakeExecutor{output: json.RawMessage(`{"response":"step output"}`)})

	ok, err := w.ProcessNext(ctx)
	require.NoError(t, err)
	require.True(t, ok)

	// one child done, one still pending -> stays RUNNING
	mid, err := env.OrchRepo.GetOrchestrationByID(ctx, orch.ID, userID)
	require.NoError(t, err)
	assert.Equal(t, domain.OrchestrationStatusRunning, mid.Status)
	root, err := env.TaskRepo.GetTaskByID(ctx, orch.RootTaskID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskStatusRunning, root.Status)

	ok, err = w.ProcessNext(ctx)
	require.NoError(t, err)
	require.True(t, ok)

	// no GET / explicit reconcile in between
	final, err := env.OrchRepo.GetOrchestrationByID(ctx, orch.ID, userID)
	require.NoError(t, err)
	assert.Equal(t, domain.OrchestrationStatusCompleted, final.Status)
	require.NotNil(t, final.FinalResponse)
	assert.Contains(t, *final.FinalResponse, "step output")

	root, err = env.TaskRepo.GetTaskByID(ctx, orch.RootTaskID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskStatusCompleted, root.Status)
	assert.False(t, domain.IsEmptyOutput(root.OutputPayload))

	dels, err := env.OrchRepo.GetDelegationsByOrchestrationID(ctx, orch.ID, userID)
	require.NoError(t, err)
	require.Len(t, dels, 2)
	for _, d := range dels {
		assert.Equal(t, domain.TaskStatusCompleted, d.Status)
	}

	// repeated and concurrent reconcile is a no-op
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = env.Orchestrator.ReconcileOrchestration(ctx, orch.ID)
			_ = env.Orchestrator.ReconcileByChildTask(ctx, dels[0].ChildTaskID)
		}()
	}
	wg.Wait()
	again, err := env.OrchRepo.GetOrchestrationByID(ctx, orch.ID, userID)
	require.NoError(t, err)
	assert.Equal(t, domain.OrchestrationStatusCompleted, again.Status)
	assert.Equal(t, *final.FinalResponse, *again.FinalResponse)
	root2, err := env.TaskRepo.GetTaskByID(ctx, orch.RootTaskID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskStatusCompleted, root2.Status)
	assert.JSONEq(t, string(root.OutputPayload), string(root2.OutputPayload))
}

func TestWorkerChildFailureFailsRootWithoutGET(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "reconcile_fail@example.com")
	orch := submitResearchForReconcile(t, env, userID)

	// fakeExecutor failures are non-retryable, so the first child fails permanently.
	exec := &fakeExecutor{err: errors.New("boom")}
	w := newReconcilingWorker(env, "w-recf", exec)

	dels, err := env.OrchRepo.GetDelegationsByOrchestrationID(ctx, orch.ID, userID)
	require.NoError(t, err)

	ok, err := w.ProcessNext(ctx)
	require.NoError(t, err)
	require.True(t, ok)

	got, err := env.OrchRepo.GetOrchestrationByID(ctx, orch.ID, userID)
	require.NoError(t, err)
	assert.Equal(t, domain.OrchestrationStatusFailed, got.Status)
	root, err := env.TaskRepo.GetTaskByID(ctx, orch.RootTaskID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskStatusFailed, root.Status)

	dels, err = env.OrchRepo.GetDelegationsByOrchestrationID(ctx, orch.ID, userID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskStatusFailed, dels[0].Status)
}

func TestReconcileByChildTaskUnknownChildIsNoop(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	assert.NoError(t, env.Orchestrator.ReconcileByChildTask(context.Background(), uuid.New()))
}

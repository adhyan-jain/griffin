package tests

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
)

func submitResearch(t *testing.T, env *TestEnv, email string) (*domain.OrchestrationRecord, []*domain.DelegationRecord, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, email)
	orch, _, err := env.Orchestrator.SubmitRequest(ctx, &domain.UserRequest{UserID: userID, RequestText: "Research root semantics"})
	require.NoError(t, err)
	dels, err := env.OrchRepo.GetDelegationsByOrchestrationID(ctx, orch.ID, userID)
	require.NoError(t, err)
	require.Len(t, dels, 2)
	return orch, dels, userID
}

func taskStatus(t *testing.T, env *TestEnv, id uuid.UUID) domain.TaskStatus {
	t.Helper()
	task, err := env.TaskRepo.GetTaskByID(context.Background(), id)
	require.NoError(t, err)
	return task.Status
}

// The JJ root is a coordination record: no worker may claim it and the reaper must leave it alone.
func TestJJRoot_NeverClaimedNorReaped(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	orch, dels, _ := submitResearch(t, env, "root_claim@example.com")

	// Drain everything claimable (only the first child is QUEUED).
	var claimedIDs []uuid.UUID
	for i := 0; i < 5; i++ {
		c, err := env.Engine.ClaimTask(ctx, "w1", time.Minute)
		require.NoError(t, err)
		if c == nil {
			break
		}
		claimedIDs = append(claimedIDs, c.ID)
	}
	assert.Equal(t, []uuid.UUID{dels[0].ChildTaskID}, claimedIDs)
	assert.NotContains(t, claimedIDs, orch.RootTaskID)

	// Reaper must not reclaim the root (NULL lease_expires_at).
	reaped, err := env.Engine.ReapExpiredLeases(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), reaped)
	root, err := env.TaskRepo.GetTaskByID(ctx, orch.RootTaskID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskStatusRunning, root.Status)
	assert.Equal(t, 0, root.RetryCount)
}

func TestJJRoot_FailsWhenChildFailsPermanently(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	orch, dels, _ := submitResearch(t, env, "root_fail@example.com")

	c, err := env.Engine.ClaimTask(ctx, "w1", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, c)
	require.NoError(t, env.Engine.FailTask(ctx, c.ID, "w1", "boom", false))

	got, err := env.Orchestrator.ReconcileOrchestration(ctx, orch.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.OrchestrationStatusFailed, got.Status)
	assert.Equal(t, domain.TaskStatusFailed, taskStatus(t, env, orch.RootTaskID))
	// Dependent child is cancelled by the engine cascade.
	assert.Equal(t, domain.TaskStatusCancelled, taskStatus(t, env, dels[1].ChildTaskID))
}

func TestJJRoot_CancelPropagatesToChildren(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	orch, dels, _ := submitResearch(t, env, "root_cancel@example.com")

	require.NoError(t, env.Engine.CancelTask(ctx, orch.RootTaskID, "user cancelled"))

	assert.Equal(t, domain.TaskStatusCancelled, taskStatus(t, env, orch.RootTaskID))
	for _, d := range dels {
		assert.Equal(t, domain.TaskStatusCancelled, taskStatus(t, env, d.ChildTaskID), "child must not stay runnable after root cancel")
	}
	c, err := env.Engine.ClaimTask(ctx, "w1", time.Minute)
	require.NoError(t, err)
	assert.Nil(t, c, "nothing may be claimable after root cancel")

	got, err := env.Orchestrator.ReconcileOrchestration(ctx, orch.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.OrchestrationStatusCancelled, got.Status)
}

func TestJJRoot_ChildCancelFailsRootOnReconcile(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	orch, dels, _ := submitResearch(t, env, "child_cancel@example.com")

	require.NoError(t, env.Engine.CancelTask(ctx, dels[0].ChildTaskID, "user cancelled child"))
	got, err := env.Orchestrator.ReconcileOrchestration(ctx, orch.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.OrchestrationStatusFailed, got.Status)
	assert.Equal(t, domain.TaskStatusFailed, taskStatus(t, env, orch.RootTaskID))
}

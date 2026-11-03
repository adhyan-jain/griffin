package tests

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
)

// Orchestrated children must get the same bounded retry budget as POST /tasks, and a
// persistently failing attempt must go RETRYING ... then FAILED after that bound.
func TestJJ_ChildTasksHaveBoundedRetries(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "jj_retries@example.com")

	orch, _, err := env.Orchestrator.SubmitRequest(ctx, &domain.UserRequest{UserID: userID, RequestText: "Research retries"})
	require.NoError(t, err)

	root, err := env.TaskRepo.GetTaskByID(ctx, orch.RootTaskID)
	require.NoError(t, err)
	assert.Equal(t, 0, root.MaxRetries, "root is JJ's coordination record, never executed by a worker")

	dels, err := env.OrchRepo.GetDelegationsByOrchestrationID(ctx, orch.ID, userID)
	require.NoError(t, err)
	require.NotEmpty(t, dels)
	for _, d := range dels {
		child, err := env.TaskRepo.GetTaskByID(ctx, d.ChildTaskID)
		require.NoError(t, err)
		assert.Equal(t, domain.DefaultMaxRetries, child.MaxRetries)
	}

	childID := dels[0].ChildTaskID
	for attempt := 1; attempt <= domain.DefaultMaxRetries; attempt++ {
		claimed, err := env.Engine.ClaimTask(ctx, "w1", time.Minute)
		require.NoError(t, err)
		require.NotNil(t, claimed, "attempt %d must be claimable", attempt)
		require.Equal(t, childID, claimed.ID)
		require.NoError(t, env.TaskRepo.FailTask(ctx, childID, "w1", "boom", true, time.Millisecond))
		got, err := env.TaskRepo.GetTaskByID(ctx, childID)
		require.NoError(t, err)
		if attempt < domain.DefaultMaxRetries {
			assert.Equal(t, domain.TaskStatusRetrying, got.Status)
			time.Sleep(20 * time.Millisecond)
		} else {
			assert.Equal(t, domain.TaskStatusFailed, got.Status)
		}
	}
}

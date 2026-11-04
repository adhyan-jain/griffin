package tests

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
)

func TestDependencyOutputFlowsToDependentTask(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "dep_flow@example.com")

	exec := &fakeExecutor{output: json.RawMessage(`{"response":"UPSTREAM-FINDINGS-42"}`)}
	w := newTestWorker(env, "w-dep", exec)

	step1, err := env.Engine.CreateTask(ctx, &domain.Task{UserID: userID, Title: "Gather primary sources", Goal: "gather"}, nil)
	require.NoError(t, err)
	_, err = env.Engine.CreateTask(ctx, &domain.Task{
		UserID: userID, Title: "Synthesize", Goal: "write the report",
		InputPayload: json.RawMessage(`{"note":"keep"}`),
	}, nil)
	require.NoError(t, err)
	dep, err := env.Engine.CreateTask(ctx, &domain.Task{
		UserID: userID, Title: "Synthesize dep", Goal: "write the report",
		InputPayload: json.RawMessage(`{"note":"keep"}`),
	}, []uuid.UUID{step1.ID})
	require.NoError(t, err)

	// independent tasks run first (FIFO among queued); drain until step1 and step2 are done.
	for i := 0; i < 2; i++ {
		ok, err := w.ProcessNext(ctx)
		require.NoError(t, err)
		require.True(t, ok)
	}
	// independent step saw its goal unchanged
	for _, seen := range exec.seen {
		assert.NotContains(t, seen.Goal, "Results from earlier steps")
	}

	ok, err := w.ProcessNext(ctx)
	require.NoError(t, err)
	require.True(t, ok)

	got := exec.lastTask()
	require.Equal(t, dep.ID, got.ID)
	assert.Contains(t, got.Goal, "write the report")
	assert.Contains(t, got.Goal, "Results from earlier steps")
	assert.Contains(t, got.Goal, "Gather primary sources")
	assert.Contains(t, got.Goal, "UPSTREAM-FINDINGS-42")

	// stored task is untouched
	stored, err := env.Engine.GetTask(ctx, dep.ID)
	require.NoError(t, err)
	assert.Equal(t, "write the report", stored.Goal)
	assert.JSONEq(t, `{"note":"keep"}`, string(stored.InputPayload))
}

func TestRepoGetDependencyOutputsOnlyCompletedParents(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "dep_repo@example.com")

	p1, err := env.Engine.CreateTask(ctx, &domain.Task{UserID: userID, Title: "P1", Goal: "g"}, nil)
	require.NoError(t, err)
	p2, err := env.Engine.CreateTask(ctx, &domain.Task{UserID: userID, Title: "P2", Goal: "g"}, nil)
	require.NoError(t, err)
	child, err := env.Engine.CreateTask(ctx, &domain.Task{UserID: userID, Title: "C", Goal: "g"}, []uuid.UUID{p1.ID, p2.ID})
	require.NoError(t, err)

	claimed, err := env.Engine.ClaimTask(ctx, "w", time.Minute)
	require.NoError(t, err)
	require.NoError(t, env.Engine.CompleteTask(ctx, claimed.ID, "w", json.RawMessage(`{"response":"r"}`)))

	outs, err := env.TaskRepo.GetDependencyOutputs(ctx, child.ID)
	require.NoError(t, err)
	require.Len(t, outs, 1)
	assert.Equal(t, claimed.Title, outs[0].Title)
	assert.JSONEq(t, `{"response":"r"}`, string(outs[0].Output))
}

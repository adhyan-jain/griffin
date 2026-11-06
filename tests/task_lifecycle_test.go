package tests

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
)

func countTasks(t *testing.T, env *TestEnv, ids ...uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, env.DB.Pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM tasks WHERE id = ANY($1)`, ids).Scan(&n))
	return n
}

func TestResumeTask(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	user := CreateTestUser(t, env.DB, "resume@example.com")

	mk := func(title string, parent *uuid.UUID, deps []uuid.UUID) *domain.Task {
		task, err := env.Engine.CreateTask(ctx, &domain.Task{UserID: user, Title: title, Goal: title, ParentID: parent, MaxRetries: 3}, deps)
		require.NoError(t, err)
		return task
	}

	t.Run("cancelled task is re-queued with a fresh budget", func(t *testing.T) {
		task := mk("cancelled", nil, nil)
		require.NoError(t, env.Engine.CancelTask(ctx, task.ID, "nope"))
		_, err := env.DB.Pool.Exec(ctx, `UPDATE tasks SET retry_count = 2 WHERE id = $1`, task.ID)
		require.NoError(t, err)

		require.NoError(t, env.Engine.ResumeTask(ctx, task.ID))
		got, err := env.Engine.GetTask(ctx, task.ID)
		require.NoError(t, err)
		assert.Equal(t, domain.TaskStatusQueued, got.Status)
		assert.Zero(t, got.RetryCount)
		assert.Nil(t, got.FailureReason)
		assert.Nil(t, got.CompletedAt)
	})

	t.Run("failed task is re-queued", func(t *testing.T) {
		task := mk("failed", nil, nil)
		_, err := env.DB.Pool.Exec(ctx, `UPDATE tasks SET status='FAILED', failure_reason='boom', completed_at=NOW() WHERE id=$1`, task.ID)
		require.NoError(t, err)
		require.NoError(t, env.Engine.ResumeTask(ctx, task.ID))
		got, _ := env.Engine.GetTask(ctx, task.ID)
		assert.Equal(t, domain.TaskStatusQueued, got.Status)
	})

	t.Run("waits again when a prerequisite is unfinished", func(t *testing.T) {
		dep := mk("dep", nil, nil)
		task := mk("child", nil, []uuid.UUID{dep.ID})
		require.NoError(t, env.Engine.CancelTask(ctx, task.ID, "x"))
		require.NoError(t, env.Engine.ResumeTask(ctx, task.ID))
		got, _ := env.Engine.GetTask(ctx, task.ID)
		assert.Equal(t, domain.TaskStatusWaitingForDependency, got.Status)
	})

	t.Run("refuses completed, active and plan-owned tasks", func(t *testing.T) {
		active := mk("active", nil, nil)
		assert.ErrorIs(t, env.Engine.ResumeTask(ctx, active.ID), domain.ErrConflict)

		done := mk("done", nil, nil)
		_, err := env.DB.Pool.Exec(ctx, `UPDATE tasks SET status='COMPLETED', completed_at=NOW() WHERE id=$1`, done.ID)
		require.NoError(t, err)
		assert.ErrorIs(t, env.Engine.ResumeTask(ctx, done.ID), domain.ErrConflict)

		sub := mk("sub", &active.ID, nil)
		require.NoError(t, env.Engine.CancelTask(ctx, sub.ID, "x"))
		assert.ErrorIs(t, env.Engine.ResumeTask(ctx, sub.ID), domain.ErrConflict)
	})
}

func TestDeleteTask(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	user := CreateTestUser(t, env.DB, "delete@example.com")

	mk := func(parent *uuid.UUID) *domain.Task {
		task, err := env.Engine.CreateTask(ctx, &domain.Task{UserID: user, Title: "t", Goal: "g", ParentID: parent, MaxRetries: 3}, nil)
		require.NoError(t, err)
		return task
	}

	t.Run("deletes a finished task with its events", func(t *testing.T) {
		task := mk(nil)
		_, err := env.DB.Pool.Exec(ctx, `UPDATE tasks SET status='COMPLETED', completed_at=NOW() WHERE id=$1`, task.ID)
		require.NoError(t, err)
		require.NoError(t, env.Engine.DeleteTask(ctx, task.ID))
		assert.Zero(t, countTasks(t, env, task.ID))
		var ev int
		require.NoError(t, env.DB.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM task_events WHERE task_id=$1`, task.ID).Scan(&ev))
		assert.Zero(t, ev, "events must be removed with the task")
	})

	t.Run("deletes the whole subtree", func(t *testing.T) {
		root := mk(nil)
		a, b := mk(&root.ID), mk(&root.ID)
		grand := mk(&a.ID)
		_, err := env.DB.Pool.Exec(ctx, `UPDATE tasks SET status='CANCELLED', completed_at=NOW() WHERE id = ANY($1)`, []uuid.UUID{root.ID, a.ID, b.ID, grand.ID})
		require.NoError(t, err)
		require.NoError(t, env.Engine.DeleteTask(ctx, root.ID))
		assert.Zero(t, countTasks(t, env, root.ID, a.ID, b.ID, grand.ID), "no orphaned subtasks")
	})

	t.Run("refuses while anything in the tree is active and deletes nothing", func(t *testing.T) {
		root := mk(nil)
		child := mk(&root.ID) // stays QUEUED
		_, err := env.DB.Pool.Exec(ctx, `UPDATE tasks SET status='CANCELLED', completed_at=NOW() WHERE id=$1`, root.ID)
		require.NoError(t, err)
		assert.ErrorIs(t, env.Engine.DeleteTask(ctx, root.ID), domain.ErrConflict)
		assert.Equal(t, 2, countTasks(t, env, root.ID, child.ID))
	})

	t.Run("missing task is a conflict, not a crash", func(t *testing.T) {
		assert.ErrorIs(t, env.Engine.DeleteTask(ctx, uuid.New()), domain.ErrConflict)
	})
}

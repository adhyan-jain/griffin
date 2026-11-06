package tests

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/storage"
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

// Old finished tasks lose their activity history and, when the full answer is kept as a file, the long
// answer is shortened. Recent tasks, unfinished tasks and answers with no file copy are left alone.
func TestCompactFinishedTasks(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "compact@example.com")
	long := strings.Repeat("insight ", 300) // ~2400 chars

	mk := func(title, status string, daysAgo int, withFile bool) uuid.UUID {
		t.Helper()
		task, err := env.Engine.CreateTask(ctx, &domain.Task{UserID: userID, Title: title, Goal: title}, nil)
		require.NoError(t, err)
		out, _ := json.Marshal(map[string]string{"response": long})
		_, err = env.DB.Pool.Exec(ctx, `UPDATE tasks SET status = $2, output_payload = $3, completed_at = NOW() - make_interval(days => $4), updated_at = NOW() - make_interval(days => $4) WHERE id = $1`, task.ID, status, out, daysAgo)
		require.NoError(t, err)
		require.NoError(t, env.TaskRepo.RecordEvent(ctx, &domain.TaskEvent{TaskID: task.ID, EventType: "STATUS_CHANGE", Message: "x", Metadata: json.RawMessage(`{}`)}))
		require.NoError(t, env.TaskRepo.RecordAttempt(ctx, &domain.TaskAttempt{ID: uuid.New(), TaskID: task.ID, WorkerID: "w", AttemptNumber: 1, Status: "COMPLETED", StartedAt: time.Now().Add(-time.Duration(daysAgo) * 24 * time.Hour)}))
		if withFile {
			tid := task.ID
			_, err := env.StorageService.SaveArtifact(ctx, storage.SaveArtifactInput{UserID: userID, TaskID: &tid, Title: title, Filename: "r.md", ArtifactType: "MARKDOWN", ContentType: "text/markdown", Body: strings.NewReader(long)})
			require.NoError(t, err)
		}
		return task.ID
	}
	oldWithFile := mk("old with file", "COMPLETED", 10, true)
	oldNoFile := mk("old without file", "COMPLETED", 10, false)
	recent := mk("recent", "COMPLETED", 1, true)
	running := mk("still running", "RUNNING", 10, true)

	compactor, ok := env.TaskRepo.(repository.TaskCompactor)
	require.True(t, ok, "the Postgres task repository compacts")
	res, err := compactor.CompactFinishedTasks(ctx, time.Now().Add(-7*24*time.Hour), 600)
	require.NoError(t, err)
	assert.Equal(t, int64(4), res.Events, "only the two old finished tasks lose their events (each has its creation event plus one more)")
	assert.Equal(t, int64(2), res.Attempts)
	assert.Equal(t, int64(1), res.Trimmed, "only the old task whose answer is kept as a file is shortened")

	resp := func(id uuid.UUID) (string, bool) {
		task, err := env.TaskRepo.GetTaskByID(ctx, id)
		require.NoError(t, err)
		var p struct {
			Response string `json:"response"`
			Trimmed  bool   `json:"trimmed"`
		}
		require.NoError(t, json.Unmarshal(task.OutputPayload, &p))
		return p.Response, p.Trimmed
	}
	r, trimmed := resp(oldWithFile)
	assert.True(t, trimmed)
	assert.LessOrEqual(t, len([]rune(r)), 601)
	r, trimmed = resp(oldNoFile)
	assert.False(t, trimmed, "no file copy: the full answer must stay")
	assert.Equal(t, long, r)
	for _, id := range []uuid.UUID{recent, running} {
		r, trimmed = resp(id)
		assert.False(t, trimmed)
		assert.Equal(t, long, r)
		events, err := env.TaskRepo.GetEvents(ctx, id)
		require.NoError(t, err)
		assert.NotEmpty(t, events, "recent and unfinished tasks keep their history")
	}

	// Idempotent: a second pass changes nothing.
	again, err := compactor.CompactFinishedTasks(ctx, time.Now().Add(-7*24*time.Hour), 600)
	require.NoError(t, err)
	assert.Equal(t, repository.CompactResult{}, again)
}

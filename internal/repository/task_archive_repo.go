package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
)

// TaskArchiver is the archive side of the task store. Archiving hides a finished task, with its subtasks and the
// follow-ups that continue it, from the normal list; the task stays readable and can be restored.
type TaskArchiver interface {
	// ArchiveCandidates returns the user's unarchived top-level tasks (follow-ups included) with only what
	// domain.AutoArchiveIDs reads: long answers and inputs are cut down to the parts the rule looks at.
	ArchiveCandidates(ctx context.Context, userID uuid.UUID) ([]*domain.Task, error)
	// ArchiveTasks archives the tasks and everything under them, skipping any whose tree still has work in
	// flight. It returns how many tasks it archived, and is safe to repeat.
	ArchiveTasks(ctx context.Context, userID uuid.UUID, ids []uuid.UUID, at time.Time) (int64, error)
	// UnarchiveTask restores the task and everything archived under it.
	UnarchiveTask(ctx context.Context, userID, id uuid.UUID) (int64, error)
	// DeleteArchived permanently deletes every archived task of the user and returns how many.
	DeleteArchived(ctx context.Context, userID uuid.UUID) (int64, error)
}

var _ TaskArchiver = (*PostgresTaskRepository)(nil)

// taskTreeCTE walks from the given roots to their subtasks (parent_id) and to the follow-ups that continue them.
const taskTreeCTE = `
	WITH RECURSIVE tree(root, id) AS (
		SELECT id, id FROM tasks WHERE user_id = $1 AND id = ANY($2::uuid[])
		UNION
		SELECT tree.root, t.id FROM tasks t JOIN tree ON t.user_id = $1
			AND (t.parent_id = tree.id OR (t.input_payload->>'follow_up_of' IS NOT NULL AND t.input_payload->>'thread_id' = tree.id::text))
	)`

func uuidStrings(ids []uuid.UUID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

func (r *PostgresTaskRepository) ArchiveCandidates(ctx context.Context, userID uuid.UUID) ([]*domain.Task, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, parent_id, title, left(goal, 200), status, failure_reason, completed_at, created_at, updated_at,
			jsonb_strip_nulls(jsonb_build_object(
				'cadence', input_payload->'cadence', 'thread_id', input_payload->'thread_id',
				'follow_up_of', input_payload->'follow_up_of', 'apply_for', input_payload->'apply_for',
				'submit_for', input_payload->'submit_for')),
			CASE WHEN length(output_payload::text) > 2000
				THEN jsonb_build_object('response', left(COALESCE(output_payload->>'response', output_payload::text), 400))
				ELSE output_payload END
		FROM tasks
		WHERE user_id = $1 AND archived_at IS NULL AND parent_id IS NULL
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("archive candidates: %w", err)
	}
	defer rows.Close()
	var tasks []*domain.Task
	for rows.Next() {
		t := &domain.Task{UserID: userID}
		if err := rows.Scan(&t.ID, &t.ParentID, &t.Title, &t.Goal, &t.Status, &t.FailureReason, &t.CompletedAt, &t.CreatedAt, &t.UpdatedAt, &t.InputPayload, &t.OutputPayload); err != nil {
			return nil, fmt.Errorf("scan archive candidate: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (r *PostgresTaskRepository) ArchiveTasks(ctx context.Context, userID uuid.UUID, ids []uuid.UUID, at time.Time) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	cmd, err := r.pool.Exec(ctx, taskTreeCTE+`,
		settled AS (
			SELECT tree.root FROM tree JOIN tasks a ON a.id = tree.id
			GROUP BY tree.root
			HAVING bool_and(a.status IN ('COMPLETED', 'FAILED', 'CANCELLED'))
		)
		UPDATE tasks SET archived_at = $3::timestamptz
		WHERE archived_at IS NULL AND id IN (SELECT id FROM tree WHERE root IN (SELECT root FROM settled))
	`, userID, uuidStrings(ids), at.UTC())
	if err != nil {
		return 0, fmt.Errorf("archive tasks: %w", err)
	}
	return cmd.RowsAffected(), nil
}

func (r *PostgresTaskRepository) UnarchiveTask(ctx context.Context, userID, id uuid.UUID) (int64, error) {
	cmd, err := r.pool.Exec(ctx, taskTreeCTE+`
		UPDATE tasks SET archived_at = NULL
		WHERE archived_at IS NOT NULL AND id IN (SELECT id FROM tree)
	`, userID, uuidStrings([]uuid.UUID{id}))
	if err != nil {
		return 0, fmt.Errorf("unarchive task: %w", err)
	}
	return cmd.RowsAffected(), nil
}

func (r *PostgresTaskRepository) DeleteArchived(ctx context.Context, userID uuid.UUID) (int64, error) {
	cmd, err := r.pool.Exec(ctx, `
		DELETE FROM tasks
		WHERE user_id = $1 AND archived_at IS NOT NULL AND status IN ('COMPLETED', 'FAILED', 'CANCELLED')
	`, userID)
	if err != nil {
		return 0, fmt.Errorf("delete archived tasks: %w", err)
	}
	return cmd.RowsAffected(), nil
}

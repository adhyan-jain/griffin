package handlers

import (
	"context"
	"time"

	"github.com/griffin/core/internal/database"
)

// PostgresSystemStatusStore reads component-level aggregates for the system status endpoint.
// It must be given a Querier that resolves per request (database.Scoped or a user tx) so that RLS
// confines every aggregate to the authenticated user's tasks. No identifiers are returned.
type PostgresSystemStatusStore struct {
	q database.Querier
}

func NewPostgresSystemStatusStore(q database.Querier) *PostgresSystemStatusStore {
	return &PostgresSystemStatusStore{q: q}
}

var _ SystemStatusStore = (*PostgresSystemStatusStore)(nil)

func (s *PostgresSystemStatusStore) WorkerEvidence(ctx context.Context, now time.Time) (WorkerEvidence, error) {
	var ev WorkerEvidence
	err := s.q.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE status = 'RUNNING' AND lease_expires_at > $1),
			count(*) FILTER (WHERE status = 'RUNNING' AND (lease_expires_at IS NULL OR lease_expires_at <= $1)),
			max(updated_at) FILTER (WHERE status = 'RUNNING' AND leased_by IS NOT NULL),
			count(*) FILTER (WHERE status IN ('QUEUED','RETRYING') AND (scheduled_for IS NULL OR scheduled_for <= $1)),
			min(GREATEST(updated_at, COALESCE(scheduled_for, updated_at)))
				FILTER (WHERE status IN ('QUEUED','RETRYING') AND (scheduled_for IS NULL OR scheduled_for <= $1))
		FROM tasks
		WHERE status IN ('RUNNING','QUEUED','RETRYING')`, now).
		Scan(&ev.ActiveLeases, &ev.ExpiredLeases, &ev.LastHeartbeat, &ev.QueuedCount, &ev.OldestQueuedAt)
	if err != nil {
		return ev, err
	}
	var started, completed *time.Time
	err = s.q.QueryRow(ctx, `
		SELECT max(started_at), max(completed_at) FROM task_attempts
		WHERE started_at > $1::timestamptz - interval '1 day'`, now).Scan(&started, &completed)
	if err != nil {
		return ev, err
	}
	ev.LastActivity = started
	if completed != nil && (started == nil || completed.After(*started)) {
		ev.LastActivity = completed
	}
	return ev, nil
}

func (s *PostgresSystemStatusStore) RecentAttempts(ctx context.Context, limit int) ([]ProviderAttempt, error) {
	rows, err := s.q.Query(ctx, `
		SELECT status, left(COALESCE(error_message, ''), 500), COALESCE(completed_at, started_at)
		FROM task_attempts
		WHERE status <> 'RUNNING'
		ORDER BY COALESCE(completed_at, started_at) DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProviderAttempt
	for rows.Next() {
		var a ProviderAttempt
		if err := rows.Scan(&a.Status, &a.Error, &a.EndedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

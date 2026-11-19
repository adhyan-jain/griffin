package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/griffin/core/internal/database"
	"time"
)

// Operation is one claimed run of a scheduled operation.
type Operation struct {
	ID       uuid.UUID
	Attempts int
}

// OperationRepository is the durable ledger behind proactive operations: idempotent claims with
// leases and retry/backoff, plus persistent cursors. It replaces in-memory "already sent" maps.
type OperationRepository struct {
	pool database.Querier
}

// NewOperationRepository constructs an OperationRepository.
func NewOperationRepository(pool database.Querier) *OperationRepository {
	return &OperationRepository{pool: pool}
}

// Claim takes the run (userID, opType, runKey) if nobody has finished it and nobody else holds an
// unexpired lease. It returns (nil, nil) when the run is already succeeded, dead, leased, waiting
// out a backoff, or out of attempts. A new run is created on first claim.
func (r *OperationRepository) Claim(ctx context.Context, userID uuid.UUID, opType, runKey string, lease time.Duration, maxAttempts int) (*Operation, error) {
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	const q = `
		INSERT INTO scheduled_operations (user_id, op_type, run_key, status, attempts, lease_expires_at)
		VALUES ($1, $2, $3, 'running', 1, NOW() + make_interval(secs => $4))
		ON CONFLICT (user_id, op_type, run_key) DO UPDATE SET
			status = 'running',
			attempts = scheduled_operations.attempts + 1,
			lease_expires_at = NOW() + make_interval(secs => $4),
			started_at = NOW(),
			updated_at = NOW()
		WHERE scheduled_operations.attempts < $5
		  AND ((scheduled_operations.status = 'failed' AND scheduled_operations.next_attempt_at <= NOW())
		    OR (scheduled_operations.status = 'running' AND scheduled_operations.lease_expires_at < NOW()))
		RETURNING id, attempts`
	op := &Operation{}
	err := r.pool.QueryRow(ctx, q, userID, opType, runKey, lease.Seconds(), maxAttempts).Scan(&op.ID, &op.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim operation: %w", err)
	}
	return op, nil
}

// Complete marks a claimed run succeeded and records the success time.
func (r *OperationRepository) Complete(ctx context.Context, userID uuid.UUID, opType string, op *Operation) error {
	if _, err := r.pool.Exec(ctx, `
		UPDATE scheduled_operations SET status='succeeded', finished_at=NOW(), updated_at=NOW(), lease_expires_at=NULL, last_error=''
		WHERE id=$1`, op.ID); err != nil {
		return fmt.Errorf("complete operation: %w", err)
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO operation_cursors (user_id, op_type, last_success_at, last_error)
		VALUES ($1, $2, NOW(), '')
		ON CONFLICT (user_id, op_type) DO UPDATE SET last_success_at=NOW(), last_error='', updated_at=NOW()`, userID, opType)
	return err
}

// Fail records a failed attempt. It is retried after retryAfter until maxAttempts, then marked dead.
func (r *OperationRepository) Fail(ctx context.Context, userID uuid.UUID, opType string, op *Operation, maxAttempts int, cause error, retryAfter time.Duration) error {
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	msg := ""
	if cause != nil {
		msg = cause.Error()
		if len(msg) > 500 {
			msg = msg[:500]
		}
	}
	if _, err := r.pool.Exec(ctx, `
		UPDATE scheduled_operations SET
			status = CASE WHEN attempts >= $3 THEN 'dead' ELSE 'failed' END,
			next_attempt_at = NOW() + make_interval(secs => $4),
			last_error = $2, finished_at = NOW(), updated_at = NOW(), lease_expires_at = NULL
		WHERE id = $1`, op.ID, msg, maxAttempts, retryAfter.Seconds()); err != nil {
		return fmt.Errorf("fail operation: %w", err)
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO operation_cursors (user_id, op_type, last_error)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, op_type) DO UPDATE SET last_error=$3, updated_at=NOW()`, userID, opType, msg)
	return err
}

// Cursor returns the persisted cursor JSON for (user, operation); empty object when none.
func (r *OperationRepository) Cursor(ctx context.Context, userID uuid.UUID, opType string) (json.RawMessage, error) {
	var c json.RawMessage
	err := r.pool.QueryRow(ctx, `SELECT cursor FROM operation_cursors WHERE user_id=$1 AND op_type=$2`, userID, opType).Scan(&c)
	if errors.Is(err, pgx.ErrNoRows) {
		return json.RawMessage(`{}`), nil
	}
	return c, err
}

// SetCursor stores the cursor JSON for (user, operation).
func (r *OperationRepository) SetCursor(ctx context.Context, userID uuid.UUID, opType string, cursor json.RawMessage) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO operation_cursors (user_id, op_type, cursor) VALUES ($1, $2, $3)
		ON CONFLICT (user_id, op_type) DO UPDATE SET cursor=$3, updated_at=NOW()`, userID, opType, cursor)
	return err
}

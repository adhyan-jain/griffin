package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/database"
)

// CallRecord is one row of the inbound call log.
type CallRecord struct {
	CallSID, From, Outcome, Message string
}

// CallRepository stores the inbound call log.
type CallRepository struct{ pool database.Querier }

// NewCallRepository constructs a CallRepository.
func NewCallRepository(pool database.Querier) *CallRepository { return &CallRepository{pool: pool} }

// Upsert records a call, or updates its outcome and message when the provider calls back for the same call.
func (r *CallRepository) Upsert(ctx context.Context, userID uuid.UUID, c CallRecord) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO call_log (user_id, call_sid, from_number, outcome, message) VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (user_id, call_sid) DO UPDATE SET outcome = EXCLUDED.outcome,
			message = CASE WHEN EXCLUDED.message <> '' THEN EXCLUDED.message ELSE call_log.message END, updated_at = NOW()`,
		userID, c.CallSID, c.From, c.Outcome, c.Message)
	if err != nil {
		return fmt.Errorf("record call: %w", err)
	}
	return nil
}

// Recent lists the owner's latest calls, newest first.
func (r *CallRepository) Recent(ctx context.Context, userID uuid.UUID, limit int) ([]CallRecord, error) {
	rows, err := r.pool.Query(ctx, `SELECT call_sid, from_number, outcome, message FROM call_log WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list calls: %w", err)
	}
	defer rows.Close()
	var out []CallRecord
	for rows.Next() {
		var c CallRecord
		if err := rows.Scan(&c.CallSID, &c.From, &c.Outcome, &c.Message); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

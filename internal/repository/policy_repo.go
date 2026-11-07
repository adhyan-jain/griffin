package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/griffin/core/internal/database"
	"github.com/griffin/core/internal/domain"
)

// StoredPolicy is a policy as persisted: opaque JSON plus its version. Version 0 means none stored yet.
type StoredPolicy struct {
	Policy  json.RawMessage
	Version int
}

// PolicyRepository persists the owner's permission policy.
type PolicyRepository interface {
	// Get returns the stored policy, or Version 0 and nil Policy when none exists.
	Get(ctx context.Context, userID uuid.UUID) (StoredPolicy, error)
	// Put stores policy if the stored version still equals expected (0 = none stored). A mismatch is
	// domain.ErrConflict: someone else changed it, and the caller must re-read.
	Put(ctx context.Context, userID uuid.UUID, policy json.RawMessage, expected int) (int, error)
}

// PostgresPolicyRepository implements PolicyRepository.
type PostgresPolicyRepository struct{ pool database.Querier }

// NewPostgresPolicyRepository constructs a PostgresPolicyRepository.
func NewPostgresPolicyRepository(pool database.Querier) *PostgresPolicyRepository {
	return &PostgresPolicyRepository{pool: pool}
}

func (r *PostgresPolicyRepository) Get(ctx context.Context, userID uuid.UUID) (StoredPolicy, error) {
	var sp StoredPolicy
	err := r.pool.QueryRow(ctx, `SELECT policy, version FROM permission_policies WHERE user_id = $1`, userID).Scan(&sp.Policy, &sp.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return StoredPolicy{}, nil
	}
	if err != nil {
		return StoredPolicy{}, fmt.Errorf("get policy: %w", err)
	}
	return sp, nil
}

func (r *PostgresPolicyRepository) Put(ctx context.Context, userID uuid.UUID, policy json.RawMessage, expected int) (int, error) {
	var version int
	if expected == 0 {
		err := r.pool.QueryRow(ctx, `
			INSERT INTO permission_policies (user_id, policy, version) VALUES ($1, $2, 1)
			ON CONFLICT (user_id) DO NOTHING RETURNING version`, userID, policy).Scan(&version)
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, domain.ErrConflict
		}
		if err != nil {
			return 0, fmt.Errorf("put policy: %w", err)
		}
		return version, nil
	}
	err := r.pool.QueryRow(ctx, `
		UPDATE permission_policies SET policy = $2, version = version + 1, updated_at = NOW()
		WHERE user_id = $1 AND version = $3 RETURNING version`, userID, policy, expected).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, domain.ErrConflict
	}
	if err != nil {
		return 0, fmt.Errorf("put policy: %w", err)
	}
	return version, nil
}

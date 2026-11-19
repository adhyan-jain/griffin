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

// OwnerProfileRepository stores the versioned canonical owner profile.
type OwnerProfileRepository struct {
	pool database.Querier
}

// NewOwnerProfileRepository constructs an OwnerProfileRepository.
func NewOwnerProfileRepository(pool database.Querier) *OwnerProfileRepository {
	return &OwnerProfileRepository{pool: pool}
}

// Latest returns the newest profile version, or (nil, nil) when none has been saved.
func (r *OwnerProfileRepository) Latest(ctx context.Context, userID uuid.UUID) (*domain.OwnerProfile, error) {
	var raw []byte
	var version int
	err := r.pool.QueryRow(ctx, `SELECT version, data FROM owner_profiles WHERE user_id=$1 ORDER BY version DESC LIMIT 1`, userID).Scan(&version, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load owner profile: %w", err)
	}
	p := &domain.OwnerProfile{}
	if err := json.Unmarshal(raw, p); err != nil {
		return nil, fmt.Errorf("decode owner profile: %w", err)
	}
	p.Version = version
	return p, nil
}

// Save stores p as the next version (history is kept) and returns the new version number.
func (r *OwnerProfileRepository) Save(ctx context.Context, userID uuid.UUID, p *domain.OwnerProfile, source string) (int, error) {
	data, err := json.Marshal(p)
	if err != nil {
		return 0, err
	}
	var v int
	err = r.pool.QueryRow(ctx, `
		INSERT INTO owner_profiles (user_id, version, data, source)
		VALUES ($1, COALESCE((SELECT MAX(version) FROM owner_profiles WHERE user_id=$1), 0) + 1, $2, $3)
		RETURNING version`, userID, data, source).Scan(&v)
	if err != nil {
		return 0, fmt.Errorf("save owner profile: %w", err)
	}
	return v, nil
}

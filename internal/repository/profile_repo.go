package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/griffin/core/internal/database"
	"github.com/griffin/core/internal/domain"
)

// ProfileRepository reads and writes the owner's profile settings.
type ProfileRepository interface {
	// ManagerName returns the chosen name, or "" when none was ever set.
	ManagerName(ctx context.Context, userID uuid.UUID) (string, error)
	SetManagerName(ctx context.Context, userID uuid.UUID, name string) error
	// VaultMisses returns the stored list of documents that were needed and were not there (JSON array).
	VaultMisses(ctx context.Context, userID uuid.UUID) ([]byte, error)
	SetVaultMisses(ctx context.Context, userID uuid.UUID, misses []byte) error
}

// PostgresProfileRepository implements ProfileRepository on profiles.settings (no schema change).
type PostgresProfileRepository struct{ pool database.Querier }

// NewPostgresProfileRepository constructs a PostgresProfileRepository.
func NewPostgresProfileRepository(pool database.Querier) *PostgresProfileRepository {
	return &PostgresProfileRepository{pool: pool}
}

func (r *PostgresProfileRepository) ManagerName(ctx context.Context, userID uuid.UUID) (string, error) {
	var name *string
	err := r.pool.QueryRow(ctx, `SELECT settings->>'manager_name' FROM profiles WHERE id = $1`, userID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read manager name: %w", err)
	}
	if name == nil {
		return "", nil
	}
	return *name, nil
}

func (r *PostgresProfileRepository) SetManagerName(ctx context.Context, userID uuid.UUID, name string) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE profiles SET settings = jsonb_set(COALESCE(settings, '{}'::jsonb), '{manager_name}', to_jsonb($2::text), true), updated_at = NOW()
		WHERE id = $1`, userID, name)
	if err != nil {
		return fmt.Errorf("save manager name: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *PostgresProfileRepository) VaultMisses(ctx context.Context, userID uuid.UUID) ([]byte, error) {
	var raw []byte
	err := r.pool.QueryRow(ctx, `SELECT COALESCE(settings->'vault_misses', '[]'::jsonb) FROM profiles WHERE id = $1`, userID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return []byte("[]"), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read vault misses: %w", err)
	}
	return raw, nil
}

func (r *PostgresProfileRepository) SetVaultMisses(ctx context.Context, userID uuid.UUID, misses []byte) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE profiles SET settings = jsonb_set(COALESCE(settings, '{}'::jsonb), '{vault_misses}', $2::jsonb, true), updated_at = NOW()
		WHERE id = $1`, userID, misses)
	if err != nil {
		return fmt.Errorf("save vault misses: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

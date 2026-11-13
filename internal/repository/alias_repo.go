package repository

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/database"
)

// AliasRepository persists the owner's aliases ("mummy" means "mom").
type AliasRepository interface {
	List(ctx context.Context, userID uuid.UUID) (map[string]string, error)
	// Put adds or replaces an alias. Both names are already normalised.
	Put(ctx context.Context, userID uuid.UUID, alias, target string) error
	// Delete removes an alias and reports whether it existed.
	Delete(ctx context.Context, userID uuid.UUID, alias string) (bool, error)
}

// PostgresAliasRepository implements AliasRepository.
type PostgresAliasRepository struct{ pool database.Querier }

// NewPostgresAliasRepository constructs a PostgresAliasRepository.
func NewPostgresAliasRepository(pool database.Querier) *PostgresAliasRepository {
	return &PostgresAliasRepository{pool: pool}
}

func (r *PostgresAliasRepository) List(ctx context.Context, userID uuid.UUID) (map[string]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT alias, target FROM contact_aliases WHERE user_id = $1 ORDER BY alias`, userID)
	if err != nil {
		return nil, fmt.Errorf("list aliases: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var a, t string
		if err := rows.Scan(&a, &t); err != nil {
			return nil, fmt.Errorf("scan alias: %w", err)
		}
		out[a] = t
	}
	return out, rows.Err()
}

func (r *PostgresAliasRepository) Put(ctx context.Context, userID uuid.UUID, alias, target string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO contact_aliases (user_id, alias, target) VALUES ($1, $2, $3)
		ON CONFLICT (user_id, alias) DO UPDATE SET target = EXCLUDED.target`, userID, alias, target)
	if err != nil {
		return fmt.Errorf("put alias: %w", err)
	}
	return nil
}

func (r *PostgresAliasRepository) Delete(ctx context.Context, userID uuid.UUID, alias string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM contact_aliases WHERE user_id = $1 AND alias = $2`, userID, alias)
	if err != nil {
		return false, fmt.Errorf("delete alias: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

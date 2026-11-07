package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/griffin/core/internal/database"
	"github.com/griffin/core/internal/domain"
)

// HostRepository defines persistence for execution hosts (laptop, phone).
type HostRepository interface {
	CreateHost(ctx context.Context, h *domain.Host, tokenHash string) (*domain.Host, error)
	// GetHostByTokenHash returns the host for a token hash. A revoked host is reported as ErrHostRevoked.
	GetHostByTokenHash(ctx context.Context, tokenHash string) (*domain.Host, error)
	// Touch records that the host was just heard from. advertised is stored when non-nil.
	Touch(ctx context.Context, id uuid.UUID, advertised []string, version string, now time.Time) error
	ListHosts(ctx context.Context, userID uuid.UUID) ([]*domain.Host, error)
	// ListLiveCandidates returns non-revoked hosts last seen at or after since, across the scope in force
	// (one user's under RLS, every user's on the system path).
	ListLiveCandidates(ctx context.Context, since time.Time) ([]*domain.Host, error)
	SetPaused(ctx context.Context, userID, id uuid.UUID, paused bool) error
	SetGrantedCapabilities(ctx context.Context, userID, id uuid.UUID, caps []string) error
	Revoke(ctx context.Context, userID, id uuid.UUID, now time.Time) error
}

// PostgresHostRepository implements HostRepository.
type PostgresHostRepository struct {
	pool database.Querier
}

// NewPostgresHostRepository constructs a PostgresHostRepository.
func NewPostgresHostRepository(pool database.Querier) *PostgresHostRepository {
	return &PostgresHostRepository{pool: pool}
}

const hostColumns = `id, user_id, name, kind, capabilities, advertised, version, paused, last_seen_at, revoked_at, created_at, updated_at`

func scanHost(s scannable) (*domain.Host, error) {
	var h domain.Host
	var kind string
	if err := s.Scan(&h.ID, &h.UserID, &h.Name, &kind, &h.Capabilities, &h.Advertised, &h.Version,
		&h.Paused, &h.LastSeenAt, &h.RevokedAt, &h.CreatedAt, &h.UpdatedAt); err != nil {
		return nil, err
	}
	h.Kind = domain.HostKind(kind)
	if h.Capabilities == nil {
		h.Capabilities = []string{}
	}
	if h.Advertised == nil {
		h.Advertised = []string{}
	}
	return &h, nil
}

func (r *PostgresHostRepository) CreateHost(ctx context.Context, h *domain.Host, tokenHash string) (*domain.Host, error) {
	if h.ID == uuid.Nil {
		h.ID = uuid.New()
	}
	if h.Capabilities == nil {
		h.Capabilities = []string{}
	}
	now := time.Now().UTC()
	row := r.pool.QueryRow(ctx, `
		INSERT INTO hosts (id, user_id, name, kind, capabilities, token_hash, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
		RETURNING `+hostColumns,
		h.ID, h.UserID, h.Name, string(h.Kind), h.Capabilities, tokenHash, now)
	created, err := scanHost(row)
	if err != nil {
		return nil, fmt.Errorf("create host: %w", err)
	}
	return created, nil
}

func (r *PostgresHostRepository) GetHostByTokenHash(ctx context.Context, tokenHash string) (*domain.Host, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+hostColumns+` FROM hosts WHERE token_hash = $1`, tokenHash)
	h, err := scanHost(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get host by token: %w", err)
	}
	if h.RevokedAt != nil {
		return nil, domain.ErrHostRevoked
	}
	return h, nil
}

func (r *PostgresHostRepository) Touch(ctx context.Context, id uuid.UUID, advertised []string, version string, now time.Time) error {
	var (
		cmd interface{ RowsAffected() int64 }
		err error
	)
	if advertised != nil {
		cmd, err = r.pool.Exec(ctx, `
			UPDATE hosts SET last_seen_at = $2, advertised = $3, version = $4, updated_at = $2
			WHERE id = $1 AND revoked_at IS NULL`, id, now, advertised, version)
	} else {
		cmd, err = r.pool.Exec(ctx, `
			UPDATE hosts SET last_seen_at = $2, updated_at = $2
			WHERE id = $1 AND revoked_at IS NULL`, id, now)
	}
	if err != nil {
		return fmt.Errorf("touch host: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *PostgresHostRepository) ListHosts(ctx context.Context, userID uuid.UUID) ([]*domain.Host, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+hostColumns+` FROM hosts
		WHERE user_id = $1 AND revoked_at IS NULL ORDER BY created_at ASC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list hosts: %w", err)
	}
	return collectHosts(rows)
}

func (r *PostgresHostRepository) ListLiveCandidates(ctx context.Context, since time.Time) ([]*domain.Host, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+hostColumns+` FROM hosts
		WHERE revoked_at IS NULL AND paused = FALSE AND last_seen_at >= $1`, since)
	if err != nil {
		return nil, fmt.Errorf("list live hosts: %w", err)
	}
	return collectHosts(rows)
}

func collectHosts(rows pgx.Rows) ([]*domain.Host, error) {
	defer rows.Close()
	out := []*domain.Host{}
	for rows.Next() {
		h, err := scanHost(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (r *PostgresHostRepository) SetPaused(ctx context.Context, userID, id uuid.UUID, paused bool) error {
	cmd, err := r.pool.Exec(ctx, `UPDATE hosts SET paused = $3, updated_at = NOW()
		WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`, id, userID, paused)
	if err != nil {
		return fmt.Errorf("pause host: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *PostgresHostRepository) SetGrantedCapabilities(ctx context.Context, userID, id uuid.UUID, caps []string) error {
	if caps == nil {
		caps = []string{}
	}
	cmd, err := r.pool.Exec(ctx, `UPDATE hosts SET capabilities = $3, updated_at = NOW()
		WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`, id, userID, caps)
	if err != nil {
		return fmt.Errorf("set host capabilities: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *PostgresHostRepository) Revoke(ctx context.Context, userID, id uuid.UUID, now time.Time) error {
	cmd, err := r.pool.Exec(ctx, `UPDATE hosts SET revoked_at = $3, updated_at = $3
		WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`, id, userID, now)
	if err != nil {
		return fmt.Errorf("revoke host: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

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

// ErrIllegalMove means a listing was asked to skip a pipeline step (for example straight to submitting).
var ErrIllegalMove = errors.New("listing cannot move to that stage")

// ListingRepository stores verified internship listings (the job_discoveries table).
type ListingRepository struct{ pool database.Querier }

// NewListingRepository constructs a ListingRepository.
func NewListingRepository(pool database.Querier) *ListingRepository {
	return &ListingRepository{pool: pool}
}

const listingCols = `id, source, external_id, company, role, url, location, pay, tier, posted_at, stage, stage_note, verified_at, last_checked_at, task_id, created_at, updated_at`

func scanListing(row pgx.Row, userID uuid.UUID) (*domain.Listing, error) {
	l := &domain.Listing{UserID: userID}
	err := row.Scan(&l.ID, &l.Source, &l.ExternalID, &l.Company, &l.Role, &l.URL, &l.Location, &l.Pay, &l.Tier, &l.PostedAt, &l.Stage, &l.StageNote, &l.VerifiedAt, &l.LastCheckedAt, &l.TaskID, &l.CreatedAt, &l.UpdatedAt)
	return l, err
}

// Upsert records a verified listing once per owner, company and role. A listing seen again is re-checked (its
// link, location and check time refresh) and a closed one reopens; its pipeline stage is otherwise kept.
// created reports whether it is new.
func (r *ListingRepository) Upsert(ctx context.Context, l *domain.Listing) (created bool, err error) {
	var inserted bool
	err = r.pool.QueryRow(ctx, `
		INSERT INTO job_discoveries (user_id, source, external_id, company, role, url, description, tier, status, location, pay, posted_at, stage, verified_at, last_checked_at)
		VALUES ($1, $2, $3, $4, $5, $6, '', $10, 'discovered', $7, $8, $9, 'verified', NOW(), NOW())
		ON CONFLICT (user_id, company, role) DO UPDATE SET
			url = EXCLUDED.url, location = EXCLUDED.location, pay = CASE WHEN EXCLUDED.pay <> '' THEN EXCLUDED.pay ELSE job_discoveries.pay END, tier = EXCLUDED.tier,
			last_checked_at = NOW(), updated_at = NOW(),
			stage = CASE WHEN job_discoveries.stage = 'closed' THEN 'verified' ELSE job_discoveries.stage END,
			stage_note = CASE WHEN job_discoveries.stage = 'closed' THEN 'reopened: the posting is back' ELSE job_discoveries.stage_note END
		RETURNING `+listingCols+`, (xmax = 0)`,
		l.UserID, l.Source, l.ExternalID, l.Company, l.Role, l.URL, l.Location, l.Pay, l.PostedAt, l.Tier,
	).Scan(&l.ID, &l.Source, &l.ExternalID, &l.Company, &l.Role, &l.URL, &l.Location, &l.Pay, &l.Tier, &l.PostedAt, &l.Stage, &l.StageNote, &l.VerifiedAt, &l.LastCheckedAt, &l.TaskID, &l.CreatedAt, &l.UpdatedAt, &inserted)
	if err != nil {
		return false, fmt.Errorf("upsert listing: %w", err)
	}
	return inserted, nil
}

// List returns the owner's listings, newest first; stages filters when given.
func (r *ListingRepository) List(ctx context.Context, userID uuid.UUID, stages []string, limit int) ([]*domain.Listing, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	q := `SELECT ` + listingCols + ` FROM job_discoveries WHERE user_id = $1`
	args := []any{userID}
	if len(stages) > 0 {
		q += ` AND stage = ANY($2)`
		args = append(args, stages)
	}
	q += fmt.Sprintf(` ORDER BY created_at DESC LIMIT %d`, limit)
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list listings: %w", err)
	}
	defer rows.Close()
	var out []*domain.Listing
	for rows.Next() {
		l, err := scanListing(rows, userID)
		if err != nil {
			return nil, fmt.Errorf("scan listing: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// Get returns one listing.
func (r *ListingRepository) Get(ctx context.Context, userID, id uuid.UUID) (*domain.Listing, error) {
	l, err := scanListing(r.pool.QueryRow(ctx, `SELECT `+listingCols+` FROM job_discoveries WHERE user_id = $1 AND id = $2`, userID, id), userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get listing: %w", err)
	}
	return l, nil
}

// Move changes a listing's stage, only along the pipeline (domain.CanMove), and records why. The move is atomic:
// two workers cannot both move the same listing from the same stage.
func (r *ListingRepository) Move(ctx context.Context, userID, id uuid.UUID, from, to, note string, taskID *uuid.UUID) error {
	if !domain.CanMove(from, to) {
		return fmt.Errorf("%w: %s to %s", ErrIllegalMove, from, to)
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE job_discoveries SET stage = $4, stage_note = $5, task_id = COALESCE($6, task_id), updated_at = NOW(),
			status = CASE WHEN $4 = 'confirmed' THEN 'applied' WHEN $4 = 'closed' THEN 'rejected' ELSE status END
		WHERE user_id = $1 AND id = $2 AND stage = $3`, userID, id, from, to, note, taskID)
	if err != nil {
		return fmt.Errorf("move listing: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: the listing is no longer %s", ErrIllegalMove, from)
	}
	return nil
}

// Checked records that a listing's link was looked at just now.
func (r *ListingRepository) Checked(ctx context.Context, userID, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `UPDATE job_discoveries SET last_checked_at = NOW() WHERE user_id = $1 AND id = $2`, userID, id)
	return err
}

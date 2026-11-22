package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
)

// detailRetryAfter is how long a listing whose detail page failed is left alone before it is tried again.
const detailRetryAfter = 24 * time.Hour

// ListingsNeedingDetail returns the owner's open Internshala and Unstop listings that have no description yet,
// newest first, leaving out those whose detail page failed to read in the last day (see source_scans, source
// "<site>_detail", target = the listing's link), so a page that is gone does not hold up the rest.
func (r *OpportunityRepository) ListingsNeedingDetail(ctx context.Context, userID uuid.UUID, limit int) ([]*domain.Listing, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+prefixed("j.", listingCols)+`
		FROM job_discoveries j
		WHERE j.user_id = $1 AND j.description = '' AND j.source IN ('internshala', 'unstop')
			AND j.stage <> 'closed' AND j.closed_at IS NULL
			AND NOT EXISTS (
				SELECT 1 FROM source_scans s
				WHERE s.user_id = j.user_id AND s.source = j.source || '_detail' AND s.target = j.url
					AND s.status = 'failed' AND s.finished_at > $2)
		ORDER BY j.created_at DESC, j.id
		LIMIT $3`, userID, time.Now().Add(-detailRetryAfter), limit)
	if err != nil {
		return nil, fmt.Errorf("listings needing detail: %w", err)
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

// SaveDetail stores what a listing's detail page said: its description and the description's hash, and the deadline
// when the page stated one (a deadline already known is never erased). The assessment reads the hash, so the
// listing is assessed again on its next run.
func (r *OpportunityRepository) SaveDetail(ctx context.Context, userID, id uuid.UUID, description, hash string, deadline *time.Time) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE job_discoveries SET description = $3, description_hash = $4, deadline = COALESCE($5::timestamptz, deadline), updated_at = NOW()
		WHERE user_id = $1 AND id = $2`, userID, id, description, hash, deadline)
	if err != nil {
		return fmt.Errorf("save detail: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

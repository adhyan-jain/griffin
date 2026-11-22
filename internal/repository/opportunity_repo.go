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

// OpportunityRepository stores the opportunity-intelligence tables (companies, ATS boards, scan history, change
// snapshots, evidence) and the ingest-side reads and writes of job_discoveries. It embeds ListingRepository so
// one value satisfies both the listing routes and the ingest.
//
// Production reaches Postgres in the simple protocol: every argument here is a scalar, string or time; id lists are
// never passed as typed arrays.
type OpportunityRepository struct {
	*ListingRepository
	pool database.Querier
}

// NewOpportunityRepository constructs an OpportunityRepository.
func NewOpportunityRepository(pool database.Querier) *OpportunityRepository {
	return &OpportunityRepository{ListingRepository: NewListingRepository(pool), pool: pool}
}

// UpsertCompany records an employer once per owner and normalized name. A candidate promoted to active takes the
// employer's evidence; an active or ignored company is never demoted or re-activated here. A known domain is kept.
// c.ID, Status and the other stored fields are filled in.
func (r *OpportunityRepository) UpsertCompany(ctx context.Context, c *domain.Company) error {
	err := r.pool.QueryRow(ctx, `
		INSERT INTO companies (user_id, name, normalized_name, domain, kind, discovered_via, evidence_url, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (user_id, normalized_name) DO UPDATE SET
			domain = COALESCE(companies.domain, EXCLUDED.domain),
			evidence_url = CASE WHEN companies.status = 'candidate' AND EXCLUDED.status = 'active' THEN COALESCE(EXCLUDED.evidence_url, companies.evidence_url) ELSE COALESCE(companies.evidence_url, EXCLUDED.evidence_url) END,
			status = CASE WHEN companies.status = 'candidate' AND EXCLUDED.status = 'active' THEN 'active' ELSE companies.status END,
			updated_at = NOW()
		RETURNING id, name, normalized_name, domain, kind, discovered_via, evidence_url, status, created_at, updated_at`,
		c.UserID, c.Name, c.NormalizedName, c.Domain, c.Kind, c.DiscoveredVia, c.EvidenceURL, c.Status,
	).Scan(&c.ID, &c.Name, &c.NormalizedName, &c.Domain, &c.Kind, &c.DiscoveredVia, &c.EvidenceURL, &c.Status, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert company: %w", err)
	}
	return nil
}

// ListCompanies returns the owner's companies, newest first; statuses filters when given.
func (r *OpportunityRepository) ListCompanies(ctx context.Context, userID uuid.UUID, statuses []string) ([]*domain.Company, error) {
	q := `SELECT id, name, normalized_name, domain, kind, discovered_via, evidence_url, status, created_at, updated_at FROM companies WHERE user_id = $1`
	args := []any{userID}
	if len(statuses) > 0 {
		q += ` AND status = ANY($2)`
		args = append(args, statuses)
	}
	rows, err := r.pool.Query(ctx, q+` ORDER BY created_at DESC, name`, args...)
	if err != nil {
		return nil, fmt.Errorf("list companies: %w", err)
	}
	defer rows.Close()
	var out []*domain.Company
	for rows.Next() {
		c := &domain.Company{UserID: userID}
		if err := rows.Scan(&c.ID, &c.Name, &c.NormalizedName, &c.Domain, &c.Kind, &c.DiscoveredVia, &c.EvidenceURL, &c.Status, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan company: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpsertBoard records a board once per owner, provider and token. A known company is kept.
func (r *OpportunityRepository) UpsertBoard(ctx context.Context, b *domain.ATSBoard) error {
	err := r.pool.QueryRow(ctx, `
		INSERT INTO ats_boards (user_id, company_id, provider, token_or_url)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, provider, token_or_url) DO UPDATE SET company_id = COALESCE(ats_boards.company_id, EXCLUDED.company_id)
		RETURNING id, company_id, status, last_scan_at, last_ok_at, consecutive_failures`,
		b.UserID, b.CompanyID, b.Provider, b.TokenOrURL,
	).Scan(&b.ID, &b.CompanyID, &b.Status, &b.LastScanAt, &b.LastOKAt, &b.ConsecutiveFailures)
	if err != nil {
		return fmt.Errorf("upsert board: %w", err)
	}
	return nil
}

// RecordBoardScan updates a known board after a scan of it: an ok scan clears the failures and makes the board
// active again, a failed scan counts one failure (domain.BoardBrokenAfter in a row marks it broken), a partial scan
// changes neither. A retired board stays retired. A board that does not exist is left alone.
func (r *OpportunityRepository) RecordBoardScan(ctx context.Context, userID uuid.UUID, provider, token, status string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE ats_boards SET
			last_scan_at = NOW(),
			last_ok_at = CASE WHEN $4 = 'ok' THEN NOW() ELSE last_ok_at END,
			consecutive_failures = CASE WHEN $4 = 'ok' THEN 0 WHEN $4 = 'failed' THEN consecutive_failures + 1 ELSE consecutive_failures END,
			status = CASE
				WHEN status = 'retired' THEN status
				WHEN $4 = 'ok' THEN 'active'
				WHEN $4 = 'failed' AND consecutive_failures + 1 >= $5 THEN 'broken'
				ELSE status END
		WHERE user_id = $1 AND provider = $2 AND token_or_url = $3`,
		userID, provider, token, status, domain.BoardBrokenAfter)
	if err != nil {
		return fmt.Errorf("record board scan: %w", err)
	}
	return nil
}

// ListBoards returns the owner's boards.
func (r *OpportunityRepository) ListBoards(ctx context.Context, userID uuid.UUID) ([]*domain.ATSBoard, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, company_id, provider, token_or_url, status, last_scan_at, last_ok_at, consecutive_failures FROM ats_boards WHERE user_id = $1 ORDER BY provider, token_or_url`, userID)
	if err != nil {
		return nil, fmt.Errorf("list boards: %w", err)
	}
	defer rows.Close()
	var out []*domain.ATSBoard
	for rows.Next() {
		b := &domain.ATSBoard{UserID: userID}
		if err := rows.Scan(&b.ID, &b.CompanyID, &b.Provider, &b.TokenOrURL, &b.Status, &b.LastScanAt, &b.LastOKAt, &b.ConsecutiveFailures); err != nil {
			return nil, fmt.Errorf("scan board: %w", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// RecordScan stores one scan in the scan history.
func (r *OpportunityRepository) RecordScan(ctx context.Context, s *domain.SourceScan) error {
	var errText *string
	if s.Error != "" {
		errText = &s.Error
	}
	err := r.pool.QueryRow(ctx, `
		INSERT INTO source_scans (user_id, source, target, status, jobs_seen, pages, error, started_at, finished_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		s.UserID, s.Source, s.Target, s.Status, s.JobsSeen, s.Pages, errText, s.StartedAt, s.FinishedAt).Scan(&s.ID)
	if err != nil {
		return fmt.Errorf("record scan: %w", err)
	}
	return nil
}

// SourceHealth returns the latest scan of every source target the owner has scanned, with the count of failed scans
// since the last ok one, most recently scanned first.
func (r *OpportunityRepository) SourceHealth(ctx context.Context, userID uuid.UUID) ([]domain.SourceHealth, error) {
	rows, err := r.pool.Query(ctx, `
		WITH a AS (
			SELECT source, target, status, error, jobs_seen, pages, finished_at,
				row_number() OVER (PARTITION BY source, target ORDER BY finished_at DESC, started_at DESC, id) AS rn,
				max(finished_at) FILTER (WHERE status = 'ok') OVER (PARTITION BY source, target) AS last_ok,
				count(*) OVER (PARTITION BY source, target) AS scans
			FROM source_scans WHERE user_id = $1
		), b AS (
			SELECT *, count(*) FILTER (WHERE status = 'failed' AND (last_ok IS NULL OR finished_at > last_ok)) OVER (PARTITION BY source, target) AS failed_since_ok FROM a
		)
		SELECT source, target, status, COALESCE(error, ''), jobs_seen, pages, finished_at, last_ok, failed_since_ok, scans
		FROM b WHERE rn = 1 ORDER BY finished_at DESC, source, target`, userID)
	if err != nil {
		return nil, fmt.Errorf("source health: %w", err)
	}
	defer rows.Close()
	out := []domain.SourceHealth{}
	for rows.Next() {
		var h domain.SourceHealth
		if err := rows.Scan(&h.Source, &h.Target, &h.LastStatus, &h.LastError, &h.JobsSeen, &h.Pages, &h.LastScanAt, &h.LastOKAt, &h.FailedSinceOK, &h.Scans); err != nil {
			return nil, fmt.Errorf("scan health: %w", err)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// FindListing returns the listing that is the same opportunity as id, or nil. A match on the source's own id wins
// over a canonical URL, which wins over employer and role keys.
func (r *OpportunityRepository) FindListing(ctx context.Context, userID uuid.UUID, id domain.Identity) (*domain.Listing, error) {
	l, err := scanListing(r.pool.QueryRow(ctx, `
		SELECT `+listingCols+` FROM job_discoveries
		WHERE user_id = $1 AND (
			($2::text <> '' AND source = $3 AND external_id = $2::text)
			OR ($4::text <> '' AND canonical_url = $4::text)
			OR ($5::text <> '' AND $6::text <> '' AND employer_key = $5::text AND role_key = $6::text))
		ORDER BY CASE
				WHEN $2::text <> '' AND source = $3 AND external_id = $2::text THEN 0
				WHEN $4::text <> '' AND canonical_url = $4::text THEN 1
				ELSE 2 END,
			created_at, id
		LIMIT 1`, userID, id.ExternalID, id.Source, id.CanonicalURL, id.EmployerKey, id.RoleKey), userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find listing: %w", err)
	}
	return l, nil
}

// InsertListing stores a new listing read by ingest. l.ID and the stored timestamps are filled in.
func (r *OpportunityRepository) InsertListing(ctx context.Context, l *domain.Listing) error {
	targets := scanTargets(l)
	err := r.pool.QueryRow(ctx, `
		INSERT INTO job_discoveries (user_id, source, external_id, company, role, url, description, tier, status, location, pay, posted_at, stage, verified_at, last_checked_at,
			company_id, ats_board_id, source_kind, verification, canonical_url, employer_url, description_hash, compensation_text, deadline, first_seen_at, last_seen_at,
			employer_key, role_key, source_target)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'discovered', $9, $10, $11, 'verified', $12, $12,
			$13, $14, $15, $16, $17, $18, $19, $20, $21, $12, $12,
			$22, $23, $24)
		RETURNING `+listingCols,
		l.UserID, l.Source, l.ExternalID, l.Company, l.Role, l.URL, l.Description, l.Tier, l.Location, l.Pay, l.PostedAt, l.FirstSeenAt,
		l.CompanyID, l.ATSBoardID, l.SourceKind, l.Verification, l.CanonicalURL, l.EmployerURL, l.DescriptionHash, l.CompensationText, l.Deadline,
		l.EmployerKey, l.RoleKey, l.SourceTarget,
	).Scan(targets...)
	if err != nil {
		return fmt.Errorf("insert listing: %w", err)
	}
	return nil
}

// UpdateListing writes what a scan learned about an existing listing: its primary source and URL, its facts, and
// the times it was seen. Its pipeline stage, task and stage note are untouched.
func (r *OpportunityRepository) UpdateListing(ctx context.Context, l *domain.Listing) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE job_discoveries SET
			source = $3, external_id = $4, company = $5, role = $6, url = $7, description = CASE WHEN $8::text <> '' THEN $8::text ELSE description END, tier = $9, location = $10, pay = $11, posted_at = $12,
			company_id = $13, ats_board_id = $14, source_kind = $15, verification = $16, canonical_url = $17, employer_url = $18,
			description_hash = $19, compensation_text = $20, deadline = $21, last_seen_at = $22, last_checked_at = $22,
			employer_key = $23, role_key = $24, source_target = $25, updated_at = NOW()
		WHERE user_id = $1 AND id = $2`,
		l.UserID, l.ID, l.Source, l.ExternalID, l.Company, l.Role, l.URL, l.Description, l.Tier, l.Location, l.Pay, l.PostedAt,
		l.CompanyID, l.ATSBoardID, l.SourceKind, l.Verification, l.CanonicalURL, l.EmployerURL,
		l.DescriptionHash, l.CompensationText, l.Deadline, l.LastSeenAt,
		l.EmployerKey, l.RoleKey, l.SourceTarget)
	if err != nil {
		return fmt.Errorf("update listing: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// Reopen marks a closed listing as open again because its posting is back: a closed stage returns to verified, any
// other stage (an application already confirmed) is kept, and the reopen is counted.
func (r *OpportunityRepository) Reopen(ctx context.Context, userID, id uuid.UUID, note string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE job_discoveries SET
			stage = CASE WHEN stage = 'closed' THEN 'verified' ELSE stage END,
			stage_note = CASE WHEN stage = 'closed' THEN $3 ELSE stage_note END,
			status = CASE WHEN stage = 'closed' THEN 'discovered' ELSE status END,
			closed_at = NULL, reopened_count = reopened_count + 1, updated_at = NOW()
		WHERE user_id = $1 AND id = $2 AND closed_at IS NOT NULL`, userID, id, note)
	if err != nil {
		return fmt.Errorf("reopen listing: %w", err)
	}
	return nil
}

// SetClosedAt records that a listing's posting is gone without touching its stage, for a listing whose stage may not
// move to closed (an application already confirmed).
func (r *OpportunityRepository) SetClosedAt(ctx context.Context, userID, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `UPDATE job_discoveries SET closed_at = COALESCE(closed_at, NOW()), updated_at = NOW() WHERE user_id = $1 AND id = $2`, userID, id)
	if err != nil {
		return fmt.Errorf("set closed_at: %w", err)
	}
	return nil
}

// OpenFromTarget returns the listings last read from one source target that are not closed.
func (r *OpportunityRepository) OpenFromTarget(ctx context.Context, userID uuid.UUID, source, target string) ([]*domain.Listing, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+listingCols+` FROM job_discoveries WHERE user_id = $1 AND source = $2 AND source_target = $3 AND closed_at IS NULL AND stage <> 'closed'`, userID, source, target)
	if err != nil {
		return nil, fmt.Errorf("open listings from target: %w", err)
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

// BackfillIdentity fills in the identity keys and canonical URL of listings written before they existed.
func (r *OpportunityRepository) BackfillIdentity(ctx context.Context, userID uuid.UUID) error {
	rows, err := r.pool.Query(ctx, `SELECT id, company, role, url FROM job_discoveries WHERE user_id = $1 AND employer_key = ''`, userID)
	if err != nil {
		return fmt.Errorf("backfill identity: %w", err)
	}
	type row struct {
		id                 uuid.UUID
		company, role, url string
	}
	var todo []row
	for rows.Next() {
		var x row
		if err := rows.Scan(&x.id, &x.company, &x.role, &x.url); err != nil {
			rows.Close()
			return fmt.Errorf("backfill identity: %w", err)
		}
		todo = append(todo, x)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("backfill identity: %w", err)
	}
	for _, x := range todo {
		id := domain.NewIdentity("", "", x.company, x.role, x.url)
		var canonical *string
		if id.CanonicalURL != "" {
			canonical = &id.CanonicalURL
		}
		if _, err := r.pool.Exec(ctx, `UPDATE job_discoveries SET employer_key = $3, role_key = $4, canonical_url = COALESCE(canonical_url, $5) WHERE user_id = $1 AND id = $2`,
			userID, x.id, id.EmployerKey, id.RoleKey, canonical); err != nil {
			return fmt.Errorf("backfill identity: %w", err)
		}
	}
	return nil
}

// AddSnapshot records what a listing said at one moment.
func (r *OpportunityRepository) AddSnapshot(ctx context.Context, s *domain.JobSnapshot) error {
	err := r.pool.QueryRow(ctx, `
		INSERT INTO job_snapshots (user_id, listing_id, captured_at, description_hash, compensation_text, deadline, title, location)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
		s.UserID, s.ListingID, s.CapturedAt, s.DescriptionHash, s.CompensationText, s.Deadline, s.Title, s.Location).Scan(&s.ID)
	if err != nil {
		return fmt.Errorf("add snapshot: %w", err)
	}
	return nil
}

// Snapshots returns a listing's snapshots, oldest first.
func (r *OpportunityRepository) Snapshots(ctx context.Context, userID, listingID uuid.UUID) ([]*domain.JobSnapshot, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, captured_at, description_hash, compensation_text, deadline, title, location FROM job_snapshots WHERE user_id = $1 AND listing_id = $2 ORDER BY captured_at, id`, userID, listingID)
	if err != nil {
		return nil, fmt.Errorf("snapshots: %w", err)
	}
	defer rows.Close()
	var out []*domain.JobSnapshot
	for rows.Next() {
		s := &domain.JobSnapshot{UserID: userID, ListingID: listingID}
		if err := rows.Scan(&s.ID, &s.CapturedAt, &s.DescriptionHash, &s.CompensationText, &s.Deadline, &s.Title, &s.Location); err != nil {
			return nil, fmt.Errorf("scan snapshot: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// AddEvidence records a claim about a listing with the exact words and page that support it.
func (r *OpportunityRepository) AddEvidence(ctx context.Context, e *domain.ListingEvidence) error {
	err := r.pool.QueryRow(ctx, `
		INSERT INTO listing_evidence (user_id, listing_id, claim, value, quote, source_url, captured_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		e.UserID, e.ListingID, e.Claim, e.Value, e.Quote, e.SourceURL, e.CapturedAt).Scan(&e.ID)
	if err != nil {
		return fmt.Errorf("add evidence: %w", err)
	}
	return nil
}

// Evidence returns a listing's evidence, oldest first.
func (r *OpportunityRepository) Evidence(ctx context.Context, userID, listingID uuid.UUID) ([]*domain.ListingEvidence, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, claim, value, quote, source_url, captured_at FROM listing_evidence WHERE user_id = $1 AND listing_id = $2 ORDER BY captured_at, id`, userID, listingID)
	if err != nil {
		return nil, fmt.Errorf("evidence: %w", err)
	}
	defer rows.Close()
	var out []*domain.ListingEvidence
	for rows.Next() {
		e := &domain.ListingEvidence{UserID: userID, ListingID: listingID}
		if err := rows.Scan(&e.ID, &e.Claim, &e.Value, &e.Quote, &e.SourceURL, &e.CapturedAt); err != nil {
			return nil, fmt.Errorf("scan evidence: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/griffin/core/internal/database"
	"github.com/griffin/core/internal/domain"
)

// DiscoveryRepository persists discovered jobs and submission receipts.
type DiscoveryRepository struct {
	pool database.Querier
}

// NewDiscoveryRepository constructs a DiscoveryRepository.
func NewDiscoveryRepository(pool database.Querier) *DiscoveryRepository {
	return &DiscoveryRepository{pool: pool}
}

// SaveDiscoveredJob records an opportunity discovered from an authorized source.
func (r *DiscoveryRepository) SaveDiscoveredJob(ctx context.Context, job *domain.DiscoveredJob) error {
	query := `
		INSERT INTO job_discoveries (user_id, source, external_id, company, role, url, description, tier, status, task_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW())
		ON CONFLICT (user_id, company, role)
		DO UPDATE SET
			description = EXCLUDED.description,
			url = EXCLUDED.url
		RETURNING id, created_at`
	return r.pool.QueryRow(ctx, query,
		job.UserID,
		string(job.Source),
		job.ExternalID,
		job.Company,
		job.Role,
		job.URL,
		job.Description,
		job.Tier,
		job.Status,
		job.TaskID,
	).Scan(&job.ID, &job.CreatedAt)
}

// IsJobDuplicate reports whether an active or applied job matches company and role.
func (r *DiscoveryRepository) IsJobDuplicate(ctx context.Context, userID uuid.UUID, company, role string) (bool, error) {
	query := `
		SELECT EXISTS (
			SELECT 1 FROM job_discoveries
			WHERE user_id = $1 AND LOWER(company) = LOWER($2) AND LOWER(role) = LOWER($3)
		)`
	var exists bool
	err := r.pool.QueryRow(ctx, query, userID, company, role).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check duplicate job: %w", err)
	}
	return exists, nil
}

// ListDiscoveredJobs retrieves discovered jobs matching a status filter.
func (r *DiscoveryRepository) ListDiscoveredJobs(ctx context.Context, userID uuid.UUID, status string, limit int) ([]*domain.DiscoveredJob, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	query := `
		SELECT id, user_id, source, external_id, company, role, url, description, tier, status, task_id, created_at
		FROM job_discoveries
		WHERE user_id = $1 AND ($2 = '' OR status = $2)
		ORDER BY created_at DESC
		LIMIT $3`
	rows, err := r.pool.Query(ctx, query, userID, status, limit)
	if err != nil {
		return nil, fmt.Errorf("list discovered jobs: %w", err)
	}
	defer rows.Close()

	var out []*domain.DiscoveredJob
	for rows.Next() {
		job := &domain.DiscoveredJob{}
		var src string
		if err := rows.Scan(
			&job.ID,
			&job.UserID,
			&src,
			&job.ExternalID,
			&job.Company,
			&job.Role,
			&job.URL,
			&job.Description,
			&job.Tier,
			&job.Status,
			&job.TaskID,
			&job.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan discovered job: %w", err)
		}
		job.Source = domain.DiscoverySource(src)
		out = append(out, job)
	}
	return out, rows.Err()
}

// RecordSubmissionReceipt persists verifiable audit proof of a submitted application.
func (r *DiscoveryRepository) RecordSubmissionReceipt(ctx context.Context, rec *domain.SubmissionReceipt) error {
	payloadBytes, _ := json.Marshal(rec.FormPayload)
	query := `
		INSERT INTO application_submissions (id, user_id, company, role, url, policy_decision, form_payload, tailored_bullets, receipt_path, receipt_hash, submitted_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, submitted_at`
	if rec.ID == uuid.Nil {
		rec.ID = uuid.New()
	}
	return r.pool.QueryRow(ctx, query,
		rec.ID,
		rec.UserID,
		rec.Company,
		rec.Role,
		rec.URL,
		string(rec.PolicyDecision),
		payloadBytes,
		rec.TailoredBullets,
		rec.ReceiptPath,
		rec.ReceiptHash,
		rec.SubmittedAt,
	).Scan(&rec.ID, &rec.SubmittedAt)
}

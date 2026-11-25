package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/griffin/core/internal/domain"
)

// Application packages are read and written through OpportunityRepository. Production reaches Postgres in the
// simple protocol: every argument is a scalar, string, time or uuid, and JSON goes in as text cast to jsonb.

const packageCols = `id, listing_id, resume_file, role_class, selected_projects::text, cover_letter, answers::text, required_documents::text, deadline,
	missing_information::text, evidence::text, status, approval_id, task_id, description_hash, profile_hash, model_used, model_calls, created_at, updated_at`

func scanPackage(row pgx.Row, userID uuid.UUID) (*domain.ApplicationPackage, error) {
	p := &domain.ApplicationPackage{UserID: userID}
	var projects, answers, docs, missing, evidence string
	err := row.Scan(&p.ID, &p.ListingID, &p.ResumeFile, &p.RoleClass, &projects, &p.CoverLetter, &answers, &docs, &p.Deadline,
		&missing, &evidence, &p.Status, &p.ApprovalID, &p.TaskID, &p.DescriptionHash, &p.ProfileHash, &p.ModelUsed, &p.ModelCalls, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	p.SelectedProjects, p.Answers, p.RequiredDocuments, p.MissingInformation, p.Evidence = []domain.PackageProject{}, []domain.PackageAnswer{}, []domain.RequiredDocument{}, []string{}, []domain.Reason{}
	for _, f := range []struct {
		raw string
		dst any
	}{{projects, &p.SelectedProjects}, {answers, &p.Answers}, {docs, &p.RequiredDocuments}, {missing, &p.MissingInformation}, {evidence, &p.Evidence}} {
		if f.raw != "" {
			if err := json.Unmarshal([]byte(f.raw), f.dst); err != nil {
				return nil, fmt.Errorf("read package json: %w", err)
			}
		}
	}
	return p, nil
}

// GetPackage returns one of the owner's packages by its id.
func (r *OpportunityRepository) GetPackage(ctx context.Context, userID, id uuid.UUID) (*domain.ApplicationPackage, error) {
	p, err := scanPackage(r.pool.QueryRow(ctx, `SELECT `+packageCols+` FROM application_packages WHERE user_id = $1 AND id = $2`, userID, id), userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get package: %w", err)
	}
	return p, nil
}

// PackageFor returns a listing's package, or nil when it has none.
func (r *OpportunityRepository) PackageFor(ctx context.Context, userID, listingID uuid.UUID) (*domain.ApplicationPackage, error) {
	p, err := scanPackage(r.pool.QueryRow(ctx, `SELECT `+packageCols+` FROM application_packages WHERE user_id = $1 AND listing_id = $2`, userID, listingID), userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get package: %w", err)
	}
	return p, nil
}

// SavePackage stores a listing's package (one per listing; a new one replaces the old, keeping its id) and fills in
// its id and times. A replaced package starts again without an approval.
func (r *OpportunityRepository) SavePackage(ctx context.Context, p *domain.ApplicationPackage) error {
	projects, err := jsonText(p.SelectedProjects)
	if err != nil {
		return err
	}
	answers, err := jsonText(p.Answers)
	if err != nil {
		return err
	}
	docs, err := jsonText(p.RequiredDocuments)
	if err != nil {
		return err
	}
	missing, err := jsonText(p.MissingInformation)
	if err != nil {
		return err
	}
	evidence, err := jsonText(p.Evidence)
	if err != nil {
		return err
	}
	err = r.pool.QueryRow(ctx, `
		INSERT INTO application_packages (user_id, listing_id, resume_file, role_class, selected_projects, cover_letter, answers, required_documents, deadline,
			missing_information, evidence, status, description_hash, profile_hash, model_used, model_calls)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7::jsonb, $8::jsonb, $9, $10::jsonb, $11::jsonb, $12, $13, $14, $15, $16)
		ON CONFLICT (listing_id) DO UPDATE SET
			resume_file = EXCLUDED.resume_file, role_class = EXCLUDED.role_class, selected_projects = EXCLUDED.selected_projects,
			cover_letter = EXCLUDED.cover_letter, answers = EXCLUDED.answers, required_documents = EXCLUDED.required_documents,
			deadline = EXCLUDED.deadline, missing_information = EXCLUDED.missing_information, evidence = EXCLUDED.evidence,
			status = EXCLUDED.status, approval_id = NULL, task_id = NULL, description_hash = EXCLUDED.description_hash,
			profile_hash = EXCLUDED.profile_hash, model_used = EXCLUDED.model_used, model_calls = EXCLUDED.model_calls, updated_at = NOW()
		RETURNING id, created_at, updated_at`,
		p.UserID, p.ListingID, p.ResumeFile, p.RoleClass, projects, p.CoverLetter, answers, docs, p.Deadline,
		missing, evidence, p.Status, p.DescriptionHash, p.ProfileHash, p.ModelUsed, p.ModelCalls).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return fmt.Errorf("save package: %w", err)
	}
	p.ApprovalID, p.TaskID = nil, nil
	return nil
}

// HoldPackage records that a package is waiting on the owner's approval: its approval and the task that carries it.
func (r *OpportunityRepository) HoldPackage(ctx context.Context, userID, id, approvalID, taskID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `UPDATE application_packages SET status = 'ready_for_approval', approval_id = $3, task_id = $4, updated_at = NOW()
		WHERE user_id = $1 AND id = $2 AND status IN ('draft', 'needs_owner')`, userID, id, approvalID, taskID)
	if err != nil {
		return fmt.Errorf("hold package: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: the package is no longer waiting to be held", domain.ErrConflict)
	}
	return nil
}

// SetPackageStatus moves a package to a status the owner's decision gave it, only from one of the allowed statuses
// (a decision is made once).
func (r *OpportunityRepository) SetPackageStatus(ctx context.Context, userID, id uuid.UUID, from []string, to string) error {
	if !domain.PackageStatuses[to] {
		return fmt.Errorf("%w: package status %q", domain.ErrValidation, to)
	}
	q := `UPDATE application_packages SET status = $3, updated_at = NOW() WHERE user_id = $1 AND id = $2 AND status IN (`
	args := []any{userID, id, to}
	for i, f := range from {
		if i > 0 {
			q += ", "
		}
		args = append(args, f)
		q += fmt.Sprintf("$%d", len(args))
	}
	tag, err := r.pool.Exec(ctx, q+`)`, args...)
	if err != nil {
		return fmt.Errorf("set package status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: the package is not %v", domain.ErrConflict, from)
	}
	return nil
}

// DeletePackage removes a package (used when the listing could not move to drafted after it was stored).
func (r *OpportunityRepository) DeletePackage(ctx context.Context, userID, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM application_packages WHERE user_id = $1 AND id = $2`, userID, id)
	return err
}

// PackageBriefs returns a brief of every package the owner has, by listing.
func (r *OpportunityRepository) PackageBriefs(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]*domain.PackageBrief, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, listing_id, status, approval_id, jsonb_array_length(missing_information), description_hash, profile_hash
		FROM application_packages WHERE user_id = $1`, userID)
	if err != nil {
		return nil, fmt.Errorf("package briefs: %w", err)
	}
	defer rows.Close()
	out := map[uuid.UUID]*domain.PackageBrief{}
	for rows.Next() {
		b := &domain.PackageBrief{}
		if err := rows.Scan(&b.ID, &b.ListingID, &b.Status, &b.ApprovalID, &b.Missing, &b.DescriptionHash, &b.ProfileHash); err != nil {
			return nil, fmt.Errorf("scan package brief: %w", err)
		}
		out[b.ListingID] = b
	}
	return out, rows.Err()
}

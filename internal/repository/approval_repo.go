package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/griffin/core/internal/domain"
)

// ApprovalRepository defines the persistence contract for approvals.
type ApprovalRepository interface {
	GetApprovalByID(ctx context.Context, id uuid.UUID) (*domain.Approval, error)
	ListApprovals(ctx context.Context, userID uuid.UUID, status *domain.ApprovalStatus) ([]*domain.Approval, error)
	RespondToApproval(ctx context.Context, id uuid.UUID, userID uuid.UUID, approved bool, reason string) (*domain.Approval, error)
}

// PostgresApprovalRepository implements ApprovalRepository.
type PostgresApprovalRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresApprovalRepository(pool *pgxpool.Pool) *PostgresApprovalRepository {
	return &PostgresApprovalRepository{pool: pool}
}

func (r *PostgresApprovalRepository) GetApprovalByID(ctx context.Context, id uuid.UUID) (*domain.Approval, error) {
	query := `
		SELECT id, user_id, task_id, action_type, risk_level, summary, action_payload,
		       status, decided_by, decision_reason, decided_at, expires_at, created_at, updated_at
		FROM approvals
		WHERE id = $1
	`
	row := r.pool.QueryRow(ctx, query, id)
	var a domain.Approval
	err := row.Scan(
		&a.ID, &a.UserID, &a.TaskID, &a.ActionType, &a.RiskLevel, &a.Summary, &a.ActionPayload,
		&a.Status, &a.DecidedBy, &a.DecisionReason, &a.DecidedAt, &a.ExpiresAt, &a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get approval by id: %w", err)
	}
	return &a, nil
}

func (r *PostgresApprovalRepository) ListApprovals(ctx context.Context, userID uuid.UUID, status *domain.ApprovalStatus) ([]*domain.Approval, error) {
	query := `
		SELECT id, user_id, task_id, action_type, risk_level, summary, action_payload,
		       status, decided_by, decision_reason, decided_at, expires_at, created_at, updated_at
		FROM approvals
		WHERE user_id = $1 AND ($2::approval_status IS NULL OR status = $2)
		ORDER BY created_at DESC
	`
	rows, err := r.pool.Query(ctx, query, userID, status)
	if err != nil {
		return nil, fmt.Errorf("list approvals: %w", err)
	}
	defer rows.Close()

	var approvals []*domain.Approval
	for rows.Next() {
		var a domain.Approval
		if err := rows.Scan(
			&a.ID, &a.UserID, &a.TaskID, &a.ActionType, &a.RiskLevel, &a.Summary, &a.ActionPayload,
			&a.Status, &a.DecidedBy, &a.DecisionReason, &a.DecidedAt, &a.ExpiresAt, &a.CreatedAt, &a.UpdatedAt,
		); err != nil {
			return nil, err
		}
		approvals = append(approvals, &a)
	}
	return approvals, nil
}

func (r *PostgresApprovalRepository) RespondToApproval(ctx context.Context, id uuid.UUID, userID uuid.UUID, approved bool, reason string) (*domain.Approval, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	now := time.Now().UTC()
	var newStatus domain.ApprovalStatus = domain.ApprovalStatusApproved
	var taskStatus domain.TaskStatus = domain.TaskStatusQueued

	if !approved {
		newStatus = domain.ApprovalStatusRejected
		taskStatus = domain.TaskStatusCancelled
	}

	query := `
		UPDATE approvals
		SET 
			status = $1,
			decided_by = $2,
			decision_reason = $3,
			decided_at = $4,
			updated_at = $4
		WHERE id = $5 AND user_id = $2 AND status = 'PENDING'
		RETURNING id, user_id, task_id, action_type, risk_level, summary, action_payload,
		          status, decided_by, decision_reason, decided_at, expires_at, created_at, updated_at
	`
	row := tx.QueryRow(ctx, query, newStatus, userID, reason, now, id)
	var a domain.Approval
	if err := row.Scan(
		&a.ID, &a.UserID, &a.TaskID, &a.ActionType, &a.RiskLevel, &a.Summary, &a.ActionPayload,
		&a.Status, &a.DecidedBy, &a.DecisionReason, &a.DecidedAt, &a.ExpiresAt, &a.CreatedAt, &a.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrApprovalNotPending
		}
		return nil, fmt.Errorf("update approval: %w", err)
	}

	// Update associated task status
	if approved {
		depCheck := `
			SELECT COUNT(*) 
			FROM task_dependencies d
			JOIN tasks p ON p.id = d.depends_on_task_id
			WHERE d.task_id = $1 AND p.status != 'COMPLETED'
		`
		var pendingDeps int
		if err := tx.QueryRow(ctx, depCheck, a.TaskID).Scan(&pendingDeps); err == nil && pendingDeps > 0 {
			taskStatus = domain.TaskStatusWaitingForDependency
		}
	}

	taskUpdate := `
		UPDATE tasks
		SET status = $1, updated_at = $2
		WHERE id = $3 AND status = 'AWAITING_APPROVAL'
	`
	if _, err := tx.Exec(ctx, taskUpdate, taskStatus, now, a.TaskID); err != nil {
		return nil, fmt.Errorf("update task status from approval: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit approval response tx: %w", err)
	}

	return &a, nil
}

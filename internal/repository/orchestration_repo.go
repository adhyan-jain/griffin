package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/griffin/core/internal/domain"
)

// OrchestrationRepository handles persistence of orchestrations, plans, and delegation records.
type OrchestrationRepository interface {
	CreateOrchestration(ctx context.Context, orch *domain.OrchestrationRecord) (*domain.OrchestrationRecord, error)
	GetOrchestrationByID(ctx context.Context, id uuid.UUID, userID uuid.UUID) (*domain.OrchestrationRecord, error)
	UpdateOrchestrationStatus(ctx context.Context, id uuid.UUID, status domain.OrchestrationStatus, finalResponse *string, errorMsg *string) error
	ListOrchestrations(ctx context.Context, userID uuid.UUID, status string) ([]*domain.OrchestrationRecord, error)
	ListActiveOrchestrations(ctx context.Context) ([]*domain.OrchestrationRecord, error)

	CreatePlan(ctx context.Context, plan *domain.OrchestrationPlan) (*domain.OrchestrationPlan, error)
	GetPlanByID(ctx context.Context, id uuid.UUID, userID uuid.UUID) (*domain.OrchestrationPlan, error)
	GetPlanByOrchestrationID(ctx context.Context, orchestrationID uuid.UUID, userID uuid.UUID) (*domain.OrchestrationPlan, error)
	UpdatePlanStatus(ctx context.Context, id uuid.UUID, status domain.PlanStatus, validationErrors []string) error

	CreateDelegations(ctx context.Context, records []*domain.DelegationRecord) error
	GetDelegationsByOrchestrationID(ctx context.Context, orchestrationID uuid.UUID, userID uuid.UUID) ([]*domain.DelegationRecord, error)
	UpdateDelegationStatus(ctx context.Context, childTaskID uuid.UUID, status domain.TaskStatus, resultSummary *string) error
	GetDelegationByChildTaskID(ctx context.Context, childTaskID uuid.UUID) (*domain.DelegationRecord, error)
}

// PostgresOrchestrationRepository implements OrchestrationRepository using pgxpool.
type PostgresOrchestrationRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresOrchestrationRepository(pool *pgxpool.Pool) *PostgresOrchestrationRepository {
	return &PostgresOrchestrationRepository{pool: pool}
}

func (r *PostgresOrchestrationRepository) CreateOrchestration(ctx context.Context, orch *domain.OrchestrationRecord) (*domain.OrchestrationRecord, error) {
	if orch.ID == uuid.Nil {
		orch.ID = uuid.New()
	}
	now := time.Now().UTC()
	orch.CreatedAt = now
	orch.UpdatedAt = now

	query := `
		INSERT INTO orchestrations (
			id, user_id, workspace_id, root_task_id, request_text, manager_id,
			status, final_response, error_message, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
		) RETURNING id, user_id, workspace_id, root_task_id, request_text, manager_id,
		            status, final_response, error_message, created_at, updated_at
	`
	row := r.pool.QueryRow(ctx, query,
		orch.ID, orch.UserID, orch.WorkspaceID, orch.RootTaskID, orch.RequestText, orch.ManagerID,
		orch.Status, orch.FinalResponse, orch.ErrorMessage, orch.CreatedAt, orch.UpdatedAt,
	)

	var created domain.OrchestrationRecord
	err := row.Scan(
		&created.ID, &created.UserID, &created.WorkspaceID, &created.RootTaskID, &created.RequestText,
		&created.ManagerID, &created.Status, &created.FinalResponse, &created.ErrorMessage,
		&created.CreatedAt, &created.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create orchestration: %w", err)
	}
	return &created, nil
}

func (r *PostgresOrchestrationRepository) GetOrchestrationByID(ctx context.Context, id uuid.UUID, userID uuid.UUID) (*domain.OrchestrationRecord, error) {
	query := `
		SELECT id, user_id, workspace_id, root_task_id, request_text, manager_id,
		       status, final_response, error_message, created_at, updated_at
		FROM orchestrations
		WHERE id = $1 AND ($2 = '00000000-0000-0000-0000-000000000000'::uuid OR user_id = $2)
	`
	row := r.pool.QueryRow(ctx, query, id, userID)
	var o domain.OrchestrationRecord
	err := row.Scan(
		&o.ID, &o.UserID, &o.WorkspaceID, &o.RootTaskID, &o.RequestText,
		&o.ManagerID, &o.Status, &o.FinalResponse, &o.ErrorMessage,
		&o.CreatedAt, &o.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get orchestration by id: %w", err)
	}
	return &o, nil
}

func (r *PostgresOrchestrationRepository) UpdateOrchestrationStatus(ctx context.Context, id uuid.UUID, status domain.OrchestrationStatus, finalResponse *string, errorMsg *string) error {
	now := time.Now().UTC()
	query := `
		UPDATE orchestrations
		SET status = $1, final_response = COALESCE($2, final_response),
		    error_message = COALESCE($3, error_message), updated_at = $4
		WHERE id = $5
	`
	tag, err := r.pool.Exec(ctx, query, status, finalResponse, errorMsg, now, id)
	if err != nil {
		return fmt.Errorf("update orchestration status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *PostgresOrchestrationRepository) ListOrchestrations(ctx context.Context, userID uuid.UUID, status string) ([]*domain.OrchestrationRecord, error) {
	query := `
		SELECT id, user_id, workspace_id, root_task_id, request_text, manager_id,
		       status, final_response, error_message, created_at, updated_at
		FROM orchestrations
		WHERE user_id = $1 AND ($2 = '' OR status = $2)
		ORDER BY created_at DESC
	`
	rows, err := r.pool.Query(ctx, query, userID, status)
	if err != nil {
		return nil, fmt.Errorf("list orchestrations: %w", err)
	}
	defer rows.Close()

	var list []*domain.OrchestrationRecord
	for rows.Next() {
		var o domain.OrchestrationRecord
		if err := rows.Scan(
			&o.ID, &o.UserID, &o.WorkspaceID, &o.RootTaskID, &o.RequestText,
			&o.ManagerID, &o.Status, &o.FinalResponse, &o.ErrorMessage,
			&o.CreatedAt, &o.UpdatedAt,
		); err != nil {
			return nil, err
		}
		list = append(list, &o)
	}
	return list, nil
}

func (r *PostgresOrchestrationRepository) ListActiveOrchestrations(ctx context.Context) ([]*domain.OrchestrationRecord, error) {
	query := `
		SELECT id, user_id, workspace_id, root_task_id, request_text, manager_id,
		       status, final_response, error_message, created_at, updated_at
		FROM orchestrations
		WHERE status NOT IN ('COMPLETED', 'FAILED', 'CANCELLED')
		ORDER BY created_at ASC
	`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list active orchestrations: %w", err)
	}
	defer rows.Close()

	var list []*domain.OrchestrationRecord
	for rows.Next() {
		var o domain.OrchestrationRecord
		if err := rows.Scan(
			&o.ID, &o.UserID, &o.WorkspaceID, &o.RootTaskID, &o.RequestText,
			&o.ManagerID, &o.Status, &o.FinalResponse, &o.ErrorMessage,
			&o.CreatedAt, &o.UpdatedAt,
		); err != nil {
			return nil, err
		}
		list = append(list, &o)
	}
	return list, nil
}

func (r *PostgresOrchestrationRepository) CreatePlan(ctx context.Context, plan *domain.OrchestrationPlan) (*domain.OrchestrationPlan, error) {
	if plan.ID == uuid.Nil {
		plan.ID = uuid.New()
	}
	now := time.Now().UTC()
	plan.CreatedAt = now
	plan.UpdatedAt = now

	payloadJSON, err := json.Marshal(plan.PlanPayload)
	if err != nil {
		return nil, fmt.Errorf("marshal plan payload: %w", err)
	}

	valErrorsJSON, err := json.Marshal(plan.ValidationErrors)
	if err != nil {
		valErrorsJSON = []byte("[]")
	}

	query := `
		INSERT INTO orchestration_plans (
			id, orchestration_id, user_id, objective, interpreted_request, manager_id,
			task_type, priority, plan_payload, status, validation_errors, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
		) RETURNING id, orchestration_id, user_id, objective, interpreted_request, manager_id,
		            task_type, priority, plan_payload, status, validation_errors, created_at, updated_at
	`
	row := r.pool.QueryRow(ctx, query,
		plan.ID, plan.OrchestrationID, plan.UserID, plan.Objective, plan.InterpretedRequest,
		plan.ManagerID, plan.TaskType, plan.Priority, payloadJSON, plan.Status,
		valErrorsJSON, plan.CreatedAt, plan.UpdatedAt,
	)

	var created domain.OrchestrationPlan
	var rawPayload, rawErrors []byte
	err = row.Scan(
		&created.ID, &created.OrchestrationID, &created.UserID, &created.Objective,
		&created.InterpretedRequest, &created.ManagerID, &created.TaskType, &created.Priority,
		&rawPayload, &created.Status, &rawErrors, &created.CreatedAt, &created.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create plan: %w", err)
	}
	_ = json.Unmarshal(rawPayload, &created.PlanPayload)
	_ = json.Unmarshal(rawErrors, &created.ValidationErrors)
	return &created, nil
}

func (r *PostgresOrchestrationRepository) GetPlanByID(ctx context.Context, id uuid.UUID, userID uuid.UUID) (*domain.OrchestrationPlan, error) {
	query := `
		SELECT id, orchestration_id, user_id, objective, interpreted_request, manager_id,
		       task_type, priority, plan_payload, status, validation_errors, created_at, updated_at
		FROM orchestration_plans
		WHERE id = $1 AND ($2 = '00000000-0000-0000-0000-000000000000'::uuid OR user_id = $2)
	`
	row := r.pool.QueryRow(ctx, query, id, userID)
	return scanOrchestrationPlan(row)
}

func (r *PostgresOrchestrationRepository) GetPlanByOrchestrationID(ctx context.Context, orchestrationID uuid.UUID, userID uuid.UUID) (*domain.OrchestrationPlan, error) {
	query := `
		SELECT id, orchestration_id, user_id, objective, interpreted_request, manager_id,
		       task_type, priority, plan_payload, status, validation_errors, created_at, updated_at
		FROM orchestration_plans
		WHERE orchestration_id = $1 AND ($2 = '00000000-0000-0000-0000-000000000000'::uuid OR user_id = $2)
		ORDER BY created_at DESC
		LIMIT 1
	`
	row := r.pool.QueryRow(ctx, query, orchestrationID, userID)
	return scanOrchestrationPlan(row)
}

func (r *PostgresOrchestrationRepository) UpdatePlanStatus(ctx context.Context, id uuid.UUID, status domain.PlanStatus, validationErrors []string) error {
	now := time.Now().UTC()
	errsJSON, _ := json.Marshal(validationErrors)
	query := `
		UPDATE orchestration_plans
		SET status = $1, validation_errors = $2, updated_at = $3
		WHERE id = $4
	`
	tag, err := r.pool.Exec(ctx, query, status, errsJSON, now, id)
	if err != nil {
		return fmt.Errorf("update plan status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *PostgresOrchestrationRepository) CreateDelegations(ctx context.Context, records []*domain.DelegationRecord) error {
	if len(records) == 0 {
		return nil
	}

	now := time.Now().UTC()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin delegations tx: %w", err)
	}
	defer tx.Rollback(ctx)

	query := `
		INSERT INTO delegation_records (
			id, orchestration_id, user_id, parent_task_id, child_task_id,
			task_key, manager_id, worker_key, status, result_summary, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
		)
	`
	for _, rec := range records {
		if rec.ID == uuid.Nil {
			rec.ID = uuid.New()
		}
		rec.CreatedAt = now
		rec.UpdatedAt = now

		_, err := tx.Exec(ctx, query,
			rec.ID, rec.OrchestrationID, rec.UserID, rec.ParentTaskID, rec.ChildTaskID,
			rec.TaskKey, rec.ManagerID, rec.WorkerKey, rec.Status, rec.ResultSummary,
			rec.CreatedAt, rec.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("insert delegation record: %w", err)
		}
	}

	return tx.Commit(ctx)
}

func (r *PostgresOrchestrationRepository) GetDelegationsByOrchestrationID(ctx context.Context, orchestrationID uuid.UUID, userID uuid.UUID) ([]*domain.DelegationRecord, error) {
	query := `
		SELECT id, orchestration_id, user_id, parent_task_id, child_task_id,
		       task_key, manager_id, worker_key, status, result_summary, created_at, updated_at
		FROM delegation_records
		WHERE orchestration_id = $1 AND ($2 = '00000000-0000-0000-0000-000000000000'::uuid OR user_id = $2)
		ORDER BY created_at ASC
	`
	rows, err := r.pool.Query(ctx, query, orchestrationID, userID)
	if err != nil {
		return nil, fmt.Errorf("get delegations by orchestration: %w", err)
	}
	defer rows.Close()

	var list []*domain.DelegationRecord
	for rows.Next() {
		var d domain.DelegationRecord
		if err := rows.Scan(
			&d.ID, &d.OrchestrationID, &d.UserID, &d.ParentTaskID, &d.ChildTaskID,
			&d.TaskKey, &d.ManagerID, &d.WorkerKey, &d.Status, &d.ResultSummary,
			&d.CreatedAt, &d.UpdatedAt,
		); err != nil {
			return nil, err
		}
		list = append(list, &d)
	}
	return list, nil
}

func (r *PostgresOrchestrationRepository) UpdateDelegationStatus(ctx context.Context, childTaskID uuid.UUID, status domain.TaskStatus, resultSummary *string) error {
	now := time.Now().UTC()
	query := `
		UPDATE delegation_records
		SET status = $1, result_summary = COALESCE($2, result_summary), updated_at = $3
		WHERE child_task_id = $4
	`
	_, err := r.pool.Exec(ctx, query, status, resultSummary, now, childTaskID)
	if err != nil {
		return fmt.Errorf("update delegation status: %w", err)
	}
	return nil
}

func (r *PostgresOrchestrationRepository) GetDelegationByChildTaskID(ctx context.Context, childTaskID uuid.UUID) (*domain.DelegationRecord, error) {
	query := `
		SELECT id, orchestration_id, user_id, parent_task_id, child_task_id,
		       task_key, manager_id, worker_key, status, result_summary, created_at, updated_at
		FROM delegation_records
		WHERE child_task_id = $1
	`
	row := r.pool.QueryRow(ctx, query, childTaskID)
	var d domain.DelegationRecord
	err := row.Scan(
		&d.ID, &d.OrchestrationID, &d.UserID, &d.ParentTaskID, &d.ChildTaskID,
		&d.TaskKey, &d.ManagerID, &d.WorkerKey, &d.Status, &d.ResultSummary,
		&d.CreatedAt, &d.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get delegation by child task: %w", err)
	}
	return &d, nil
}

func scanOrchestrationPlan(row pgx.Row) (*domain.OrchestrationPlan, error) {
	var p domain.OrchestrationPlan
	var rawPayload, rawErrors []byte
	err := row.Scan(
		&p.ID, &p.OrchestrationID, &p.UserID, &p.Objective, &p.InterpretedRequest,
		&p.ManagerID, &p.TaskType, &p.Priority, &rawPayload, &p.Status, &rawErrors,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("scan orchestration plan: %w", err)
	}
	_ = json.Unmarshal(rawPayload, &p.PlanPayload)
	_ = json.Unmarshal(rawErrors, &p.ValidationErrors)
	return &p, nil
}

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/griffin/core/internal/domain"
)

// TaskFilter specifies criteria for listing tasks.
type TaskFilter struct {
	UserID        *uuid.UUID
	Status        *domain.TaskStatus
	ManagerDomain *string
	ParentID      *uuid.UUID
	Limit         int
	Offset        int
}

// TaskRepository defines the persistence contract for Griffin tasks.
type TaskRepository interface {
	CreateTask(ctx context.Context, task *domain.Task) (*domain.Task, error)
	GetTaskByID(ctx context.Context, id uuid.UUID) (*domain.Task, error)
	GetTaskByIDAndUser(ctx context.Context, id uuid.UUID, userID uuid.UUID) (*domain.Task, error)
	ListTasks(ctx context.Context, filter TaskFilter) ([]*domain.Task, error)
	UpdateTask(ctx context.Context, task *domain.Task) error
	ClaimNextTask(ctx context.Context, workerID string, leaseDuration time.Duration) (*domain.Task, error)
	RenewLease(ctx context.Context, taskID uuid.UUID, workerID string, leaseDuration time.Duration) error
	CompleteTask(ctx context.Context, taskID uuid.UUID, workerID string, output json.RawMessage) error
	FailTask(ctx context.Context, taskID uuid.UUID, workerID string, reason string, retryable bool, backoff time.Duration) error
	CancelTask(ctx context.Context, taskID uuid.UUID, reason string) error
	ReapExpiredLeases(ctx context.Context) (int64, error)

	AddDependency(ctx context.Context, taskID, dependsOnID uuid.UUID) error
	GetDependencies(ctx context.Context, taskID uuid.UUID) ([]uuid.UUID, error)
	CheckDependenciesCompleted(ctx context.Context, taskID uuid.UUID) (bool, error)
	GetDependentTasks(ctx context.Context, prerequisiteTaskID uuid.UUID) ([]uuid.UUID, error)
	UnblockTaskIfEligible(ctx context.Context, taskID uuid.UUID) (bool, error)

	RecordEvent(ctx context.Context, event *domain.TaskEvent) error
	GetEvents(ctx context.Context, taskID uuid.UUID) ([]*domain.TaskEvent, error)

	RecordAttempt(ctx context.Context, attempt *domain.TaskAttempt) error
	GetAttempts(ctx context.Context, taskID uuid.UUID) ([]*domain.TaskAttempt, error)

	TransitionToApproval(ctx context.Context, taskID uuid.UUID, approval *domain.Approval) error
	CountRunnableTasks(ctx context.Context) (int64, error)
}

// PostgresTaskRepository implements TaskRepository on top of pgxpool.Pool.
type PostgresTaskRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresTaskRepository constructs a new PostgresTaskRepository.
func NewPostgresTaskRepository(pool *pgxpool.Pool) *PostgresTaskRepository {
	return &PostgresTaskRepository{pool: pool}
}

func (r *PostgresTaskRepository) CreateTask(ctx context.Context, t *domain.Task) (*domain.Task, error) {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	now := time.Now().UTC()
	t.CreatedAt = now
	t.UpdatedAt = now

	if t.InputPayload == nil {
		t.InputPayload = json.RawMessage("{}")
	}

	query := `
		INSERT INTO tasks (
			id, user_id, workspace_id, parent_id, root_id, assigned_agent_id,
			manager_domain, title, goal, input_payload, output_payload,
			status, priority, idempotency_key, retry_count, max_retries,
			scheduled_for, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19
		) RETURNING 
			id, user_id, workspace_id, parent_id, root_id, assigned_agent_id,
			manager_domain, title, goal, input_payload, output_payload,
			status, priority, idempotency_key, leased_by, lease_expires_at,
			retry_count, max_retries, failure_reason, scheduled_for, started_at, completed_at,
			created_at, updated_at
	`

	row := r.pool.QueryRow(ctx, query,
		t.ID, t.UserID, t.WorkspaceID, t.ParentID, t.RootID, t.AssignedAgentID,
		t.ManagerDomain, t.Title, t.Goal, t.InputPayload, t.OutputPayload,
		t.Status, t.Priority, t.IdempotencyKey, t.RetryCount, t.MaxRetries,
		t.ScheduledFor, t.CreatedAt, t.UpdatedAt,
	)

	created, err := scanTask(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique violation (idempotency key)
			return nil, domain.ErrIdempotentConflict
		}
		return nil, fmt.Errorf("insert task: %w", err)
	}

	return created, nil
}

func (r *PostgresTaskRepository) GetTaskByID(ctx context.Context, id uuid.UUID) (*domain.Task, error) {
	query := `
		SELECT 
			id, user_id, workspace_id, parent_id, root_id, assigned_agent_id,
			manager_domain, title, goal, input_payload, output_payload,
			status, priority, idempotency_key, leased_by, lease_expires_at,
			retry_count, max_retries, failure_reason, scheduled_for, started_at, completed_at,
			created_at, updated_at
		FROM tasks
		WHERE id = $1
	`
	row := r.pool.QueryRow(ctx, query, id)
	t, err := scanTask(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get task by id: %w", err)
	}
	return t, nil
}

func (r *PostgresTaskRepository) GetTaskByIDAndUser(ctx context.Context, id uuid.UUID, userID uuid.UUID) (*domain.Task, error) {
	query := `
		SELECT 
			id, user_id, workspace_id, parent_id, root_id, assigned_agent_id,
			manager_domain, title, goal, input_payload, output_payload,
			status, priority, idempotency_key, leased_by, lease_expires_at,
			retry_count, max_retries, failure_reason, scheduled_for, started_at, completed_at,
			created_at, updated_at
		FROM tasks
		WHERE id = $1 AND user_id = $2
	`
	row := r.pool.QueryRow(ctx, query, id, userID)
	t, err := scanTask(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get task by id and user: %w", err)
	}
	return t, nil
}

func (r *PostgresTaskRepository) ListTasks(ctx context.Context, filter TaskFilter) ([]*domain.Task, error) {
	query := `
		SELECT 
			id, user_id, workspace_id, parent_id, root_id, assigned_agent_id,
			manager_domain, title, goal, input_payload, output_payload,
			status, priority, idempotency_key, leased_by, lease_expires_at,
			retry_count, max_retries, failure_reason, scheduled_for, started_at, completed_at,
			created_at, updated_at
		FROM tasks
		WHERE ($1::uuid IS NULL OR user_id = $1)
		  AND ($2::task_status IS NULL OR status = $2)
		  AND ($3::varchar IS NULL OR manager_domain = $3)
		  AND ($4::uuid IS NULL OR parent_id = $4)
		ORDER BY created_at DESC
		LIMIT $5 OFFSET $6
	`
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}

	rows, err := r.pool.Query(ctx, query, filter.UserID, filter.Status, filter.ManagerDomain, filter.ParentID, limit, filter.Offset)
	if err != nil {
		return nil, fmt.Errorf("list tasks query: %w", err)
	}
	defer rows.Close()

	var tasks []*domain.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("scan task in list: %w", err)
		}
		tasks = append(tasks, t)
	}

	return tasks, nil
}

func (r *PostgresTaskRepository) UpdateTask(ctx context.Context, t *domain.Task) error {
	t.UpdatedAt = time.Now().UTC()
	query := `
		UPDATE tasks
		SET 
			status = $2,
			output_payload = $3,
			leased_by = $4,
			lease_expires_at = $5,
			retry_count = $6,
			failure_reason = $7,
			scheduled_for = $8,
			started_at = $9,
			completed_at = $10,
			updated_at = $11
		WHERE id = $1
	`
	cmd, err := r.pool.Exec(ctx, query,
		t.ID, t.Status, t.OutputPayload, t.LeasedBy, t.LeaseExpiresAt,
		t.RetryCount, t.FailureReason, t.ScheduledFor, t.StartedAt, t.CompletedAt,
		t.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("update task: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *PostgresTaskRepository) ClaimNextTask(ctx context.Context, workerID string, leaseDuration time.Duration) (*domain.Task, error) {
	now := time.Now().UTC()
	leaseExpires := now.Add(leaseDuration)

	// Atomic claim using SELECT FOR UPDATE SKIP LOCKED
	query := `
		UPDATE tasks
		SET 
			status = 'RUNNING',
			leased_by = $1,
			lease_expires_at = $2,
			started_at = COALESCE(started_at, $3),
			updated_at = $3
		WHERE id = (
			SELECT id
			FROM tasks
			WHERE status = 'QUEUED'
			  AND (scheduled_for IS NULL OR scheduled_for <= $3)
			ORDER BY priority DESC, created_at ASC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING 
			id, user_id, workspace_id, parent_id, root_id, assigned_agent_id,
			manager_domain, title, goal, input_payload, output_payload,
			status, priority, idempotency_key, leased_by, lease_expires_at,
			retry_count, max_retries, failure_reason, scheduled_for, started_at, completed_at,
			created_at, updated_at
	`

	row := r.pool.QueryRow(ctx, query, workerID, leaseExpires, now)
	task, err := scanTask(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil // No task available to claim
		}
		return nil, fmt.Errorf("claim next task: %w", err)
	}

	return task, nil
}

func (r *PostgresTaskRepository) RenewLease(ctx context.Context, taskID uuid.UUID, workerID string, leaseDuration time.Duration) error {
	now := time.Now().UTC()
	leaseExpires := now.Add(leaseDuration)

	query := `
		UPDATE tasks
		SET lease_expires_at = $1, updated_at = $2
		WHERE id = $3 
		  AND leased_by = $4 
		  AND status = 'RUNNING'
	`
	cmd, err := r.pool.Exec(ctx, query, leaseExpires, now, taskID, workerID)
	if err != nil {
		return fmt.Errorf("renew lease: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return domain.ErrLeaseExpired
	}
	return nil
}

func (r *PostgresTaskRepository) CompleteTask(ctx context.Context, taskID uuid.UUID, workerID string, output json.RawMessage) error {
	now := time.Now().UTC()
	if output == nil {
		output = json.RawMessage("{}")
	}

	query := `
		UPDATE tasks
		SET 
			status = 'COMPLETED',
			output_payload = $1,
			completed_at = $2,
			leased_by = NULL,
			lease_expires_at = NULL,
			updated_at = $2
		WHERE id = $3 
		  AND leased_by = $4 
		  AND status IN ('RUNNING', 'VERIFYING')
	`
	cmd, err := r.pool.Exec(ctx, query, output, now, taskID, workerID)
	if err != nil {
		return fmt.Errorf("complete task: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return domain.ErrLeaseExpired
	}
	return nil
}

func (r *PostgresTaskRepository) FailTask(ctx context.Context, taskID uuid.UUID, workerID string, reason string, retryable bool, backoff time.Duration) error {
	now := time.Now().UTC()

	var query string
	var args []any

	if retryable {
		scheduledFor := now.Add(backoff)
		query = `
			UPDATE tasks
			SET 
				status = CASE 
					WHEN retry_count + 1 < max_retries THEN 'RETRYING'::task_status 
					ELSE 'FAILED'::task_status 
				END,
				retry_count = retry_count + 1,
				failure_reason = $1,
				scheduled_for = CASE 
					WHEN retry_count + 1 < max_retries THEN $2::timestamptz 
					ELSE NULL 
				END,
				completed_at = CASE 
					WHEN retry_count + 1 >= max_retries THEN $3::timestamptz 
					ELSE NULL 
				END,
				leased_by = NULL,
				lease_expires_at = NULL,
				updated_at = $3::timestamptz
			WHERE id = $4 AND leased_by = $5 AND status IN ('RUNNING', 'VERIFYING')
		`
		args = []any{reason, scheduledFor, now, taskID, workerID}
	} else {
		query = `
			UPDATE tasks
			SET 
				status = 'FAILED',
				failure_reason = $1,
				completed_at = $2::timestamptz,
				leased_by = NULL,
				lease_expires_at = NULL,
				updated_at = $2::timestamptz
			WHERE id = $3 AND leased_by = $4 AND status IN ('RUNNING', 'VERIFYING')
		`
		args = []any{reason, now, taskID, workerID}
	}

	cmd, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("fail task: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return domain.ErrLeaseExpired
	}
	return nil
}

func (r *PostgresTaskRepository) CancelTask(ctx context.Context, taskID uuid.UUID, reason string) error {
	now := time.Now().UTC()
	query := `
		UPDATE tasks
		SET 
			status = 'CANCELLED',
			failure_reason = $1,
			completed_at = COALESCE(completed_at, $2),
			leased_by = NULL,
			lease_expires_at = NULL,
			updated_at = $2
		WHERE id = $3 AND status NOT IN ('COMPLETED', 'FAILED', 'CANCELLED')
	`
	cmd, err := r.pool.Exec(ctx, query, reason, now, taskID)
	if err != nil {
		return fmt.Errorf("cancel task: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return domain.ErrTaskAlreadyTerminal
	}
	return nil
}

func (r *PostgresTaskRepository) ReapExpiredLeases(ctx context.Context) (int64, error) {
	now := time.Now().UTC()
	query := `
		UPDATE tasks
		SET 
			status = CASE 
				WHEN retry_count + 1 < max_retries THEN 'RETRYING'::task_status 
				ELSE 'FAILED'::task_status 
			END,
			retry_count = retry_count + 1,
			failure_reason = 'lease expired without heartbeat',
			scheduled_for = $1,
			completed_at = CASE 
				WHEN retry_count + 1 >= max_retries THEN $1 
				ELSE NULL 
			END,
			leased_by = NULL,
			lease_expires_at = NULL,
			updated_at = $1
		WHERE status = 'RUNNING' 
		  AND lease_expires_at < $1
	`
	cmd, err := r.pool.Exec(ctx, query, now)
	if err != nil {
		return 0, fmt.Errorf("reap expired leases: %w", err)
	}
	return cmd.RowsAffected(), nil
}

func (r *PostgresTaskRepository) AddDependency(ctx context.Context, taskID, dependsOnID uuid.UUID) error {
	query := `
		INSERT INTO task_dependencies (task_id, depends_on_task_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`
	_, err := r.pool.Exec(ctx, query, taskID, dependsOnID)
	if err != nil {
		return fmt.Errorf("add dependency: %w", err)
	}
	return nil
}

func (r *PostgresTaskRepository) GetDependencies(ctx context.Context, taskID uuid.UUID) ([]uuid.UUID, error) {
	query := `
		SELECT depends_on_task_id
		FROM task_dependencies
		WHERE task_id = $1
	`
	rows, err := r.pool.Query(ctx, query, taskID)
	if err != nil {
		return nil, fmt.Errorf("get dependencies: %w", err)
	}
	defer rows.Close()

	var deps []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		deps = append(deps, id)
	}
	return deps, nil
}

func (r *PostgresTaskRepository) CheckDependenciesCompleted(ctx context.Context, taskID uuid.UUID) (bool, error) {
	query := `
		SELECT COUNT(*)
		FROM task_dependencies td
		JOIN tasks parent ON parent.id = td.depends_on_task_id
		WHERE td.task_id = $1 AND parent.status != 'COMPLETED'
	`
	var uncompletedCount int
	if err := r.pool.QueryRow(ctx, query, taskID).Scan(&uncompletedCount); err != nil {
		return false, fmt.Errorf("check dependencies completed: %w", err)
	}
	return uncompletedCount == 0, nil
}

func (r *PostgresTaskRepository) GetDependentTasks(ctx context.Context, prerequisiteTaskID uuid.UUID) ([]uuid.UUID, error) {
	query := `
		SELECT td.task_id
		FROM task_dependencies td
		JOIN tasks child ON child.id = td.task_id
		WHERE td.depends_on_task_id = $1
		  AND child.status = 'WAITING_FOR_DEPENDENCY'
	`
	rows, err := r.pool.Query(ctx, query, prerequisiteTaskID)
	if err != nil {
		return nil, fmt.Errorf("get dependent tasks: %w", err)
	}
	defer rows.Close()

	var tasks []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		tasks = append(tasks, id)
	}
	return tasks, nil
}

func (r *PostgresTaskRepository) UnblockTaskIfEligible(ctx context.Context, taskID uuid.UUID) (bool, error) {
	query := `
		UPDATE tasks
		SET status = 'QUEUED', updated_at = NOW()
		WHERE id = $1
		  AND status = 'WAITING_FOR_DEPENDENCY'
		  AND NOT EXISTS (
			  SELECT 1 
			  FROM task_dependencies td
			  JOIN tasks parent ON parent.id = td.depends_on_task_id
			  WHERE td.task_id = $1 AND parent.status != 'COMPLETED'
		  )
	`
	cmd, err := r.pool.Exec(ctx, query, taskID)
	if err != nil {
		return false, fmt.Errorf("unblock task if eligible: %w", err)
	}
	return cmd.RowsAffected() > 0, nil
}

func (r *PostgresTaskRepository) RecordEvent(ctx context.Context, e *domain.TaskEvent) error {
	now := time.Now().UTC()
	if e.Metadata == nil {
		e.Metadata = json.RawMessage("{}")
	}
	query := `
		INSERT INTO task_events (task_id, event_type, message, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at
	`
	return r.pool.QueryRow(ctx, query, e.TaskID, e.EventType, e.Message, e.Metadata, now).Scan(&e.ID, &e.CreatedAt)
}

func (r *PostgresTaskRepository) GetEvents(ctx context.Context, taskID uuid.UUID) ([]*domain.TaskEvent, error) {
	query := `
		SELECT id, task_id, event_type, message, metadata, created_at
		FROM task_events
		WHERE task_id = $1
		ORDER BY created_at ASC
	`
	rows, err := r.pool.Query(ctx, query, taskID)
	if err != nil {
		return nil, fmt.Errorf("get events: %w", err)
	}
	defer rows.Close()

	var events []*domain.TaskEvent
	for rows.Next() {
		var e domain.TaskEvent
		if err := rows.Scan(&e.ID, &e.TaskID, &e.EventType, &e.Message, &e.Metadata, &e.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, &e)
	}
	return events, nil
}

func (r *PostgresTaskRepository) RecordAttempt(ctx context.Context, a *domain.TaskAttempt) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	now := time.Now().UTC()
	a.CreatedAt = now

	query := `
		INSERT INTO task_attempts (
			id, task_id, agent_id, worker_id, attempt_number, status,
			prompt_tokens, completion_tokens, duration_ms, error_message,
			started_at, completed_at, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
		) ON CONFLICT (id) DO UPDATE SET
			status = EXCLUDED.status,
			prompt_tokens = EXCLUDED.prompt_tokens,
			completion_tokens = EXCLUDED.completion_tokens,
			duration_ms = EXCLUDED.duration_ms,
			error_message = EXCLUDED.error_message,
			completed_at = EXCLUDED.completed_at
	`
	_, err := r.pool.Exec(ctx, query,
		a.ID, a.TaskID, a.AgentID, a.WorkerID, a.AttemptNumber, a.Status,
		a.PromptTokens, a.CompletionTokens, a.DurationMs, a.ErrorMessage,
		a.StartedAt, a.CompletedAt, a.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("record attempt: %w", err)
	}
	return nil
}

func (r *PostgresTaskRepository) GetAttempts(ctx context.Context, taskID uuid.UUID) ([]*domain.TaskAttempt, error) {
	query := `
		SELECT 
			id, task_id, agent_id, worker_id, attempt_number, status,
			prompt_tokens, completion_tokens, duration_ms, error_message,
			started_at, completed_at, created_at
		FROM task_attempts
		WHERE task_id = $1
		ORDER BY attempt_number ASC
	`
	rows, err := r.pool.Query(ctx, query, taskID)
	if err != nil {
		return nil, fmt.Errorf("get attempts: %w", err)
	}
	defer rows.Close()

	var attempts []*domain.TaskAttempt
	for rows.Next() {
		var a domain.TaskAttempt
		if err := rows.Scan(
			&a.ID, &a.TaskID, &a.AgentID, &a.WorkerID, &a.AttemptNumber, &a.Status,
			&a.PromptTokens, &a.CompletionTokens, &a.DurationMs, &a.ErrorMessage,
			&a.StartedAt, &a.CompletedAt, &a.CreatedAt,
		); err != nil {
			return nil, err
		}
		attempts = append(attempts, &a)
	}
	return attempts, nil
}

func (r *PostgresTaskRepository) TransitionToApproval(ctx context.Context, taskID uuid.UUID, app *domain.Approval) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin approval tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	now := time.Now().UTC()
	// 1. Update task to AWAITING_APPROVAL
	taskQuery := `
		UPDATE tasks
		SET status = 'AWAITING_APPROVAL', updated_at = $1
		WHERE id = $2 AND status IN ('RUNNING', 'QUEUED')
	`
	cmd, err := tx.Exec(ctx, taskQuery, now, taskID)
	if err != nil {
		return fmt.Errorf("transition task to approval: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return domain.ErrInvalidTransition
	}

	// 2. Insert approval
	if app.ID == uuid.Nil {
		app.ID = uuid.New()
	}
	app.CreatedAt = now
	app.UpdatedAt = now
	app.Status = domain.ApprovalStatusPending

	approvalQuery := `
		INSERT INTO approvals (
			id, user_id, task_id, action_type, risk_level, summary,
			action_payload, status, expires_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
		)
	`
	_, err = tx.Exec(ctx, approvalQuery,
		app.ID, app.UserID, app.TaskID, app.ActionType, app.RiskLevel, app.Summary,
		app.ActionPayload, app.Status, app.ExpiresAt, app.CreatedAt, app.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert approval: %w", err)
	}

	// 3. Log event
	eventQuery := `
		INSERT INTO task_events (task_id, event_type, message, metadata, created_at)
		VALUES ($1, 'STATUS_CHANGE', 'Task entered AWAITING_APPROVAL', $2, $3)
	`
	meta, _ := json.Marshal(map[string]any{"approval_id": app.ID, "risk_level": app.RiskLevel})
	if _, err := tx.Exec(ctx, eventQuery, taskID, meta, now); err != nil {
		return fmt.Errorf("log approval event: %w", err)
	}

	return tx.Commit(ctx)
}

// CountRunnableTasks returns the number of tasks that are currently runnable or in flight.
func (r *PostgresTaskRepository) CountRunnableTasks(ctx context.Context) (int64, error) {
	query := `
		SELECT COUNT(*)
		FROM tasks t
		WHERE t.status IN ('RUNNING', 'QUEUED', 'ROUTED')
		   OR (t.status = 'WAITING_FOR_DEPENDENCY' AND NOT EXISTS (
		       SELECT 1 FROM task_dependencies td
		       JOIN tasks parent ON parent.id = td.depends_on_task_id
		       WHERE td.task_id = t.id AND parent.status != 'COMPLETED'
		   ))
		   OR (t.status = 'RETRYING' AND (t.scheduled_for IS NULL OR t.scheduled_for <= NOW()));
	`
	var count int64
	err := r.pool.QueryRow(ctx, query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count runnable tasks: %w", err)
	}
	return count, nil
}

type scannable interface {
	Scan(dest ...any) error
}

func scanTask(s scannable) (*domain.Task, error) {
	var t domain.Task
	err := s.Scan(
		&t.ID, &t.UserID, &t.WorkspaceID, &t.ParentID, &t.RootID, &t.AssignedAgentID,
		&t.ManagerDomain, &t.Title, &t.Goal, &t.InputPayload, &t.OutputPayload,
		&t.Status, &t.Priority, &t.IdempotencyKey, &t.LeasedBy, &t.LeaseExpiresAt,
		&t.RetryCount, &t.MaxRetries, &t.FailureReason, &t.ScheduledFor, &t.StartedAt, &t.CompletedAt,
		&t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

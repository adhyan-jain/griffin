package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/griffin/core/internal/database"
	"github.com/griffin/core/internal/domain"
	"github.com/jackc/pgx/v5"
)

var (
	ErrScheduleNotFound = errors.New("schedule not found")
)

// ScheduleRepository handles CRUD and durable execution lifecycle for schedules.
type ScheduleRepository interface {
	CreateSchedule(ctx context.Context, s *domain.Schedule) (*domain.Schedule, error)
	GetScheduleByID(ctx context.Context, id uuid.UUID) (*domain.Schedule, error)
	ListSchedules(ctx context.Context, userID uuid.UUID) ([]*domain.Schedule, error)
	UpdateSchedule(ctx context.Context, s *domain.Schedule) error
	SetPaused(ctx context.Context, id uuid.UUID, userID uuid.UUID, paused bool) error
	DeleteSchedule(ctx context.Context, id uuid.UUID, userID uuid.UUID) error

	// ClaimDueSchedules atomically selects up to limit non-paused schedules due at or before now.
	// Uses SELECT ... FOR UPDATE SKIP LOCKED to prevent duplicate execution across ticks/nodes.
	ClaimDueSchedules(ctx context.Context, now time.Time, limit int) ([]*domain.Schedule, error)

	// AdvanceNextRun updates last_run_at and sets next_run_at to the given timestamp.
	AdvanceNextRun(ctx context.Context, id uuid.UUID, lastRun, nextRun time.Time) error
}

type PostgresScheduleRepository struct {
	pool database.Querier
}

func NewPostgresScheduleRepository(pool database.Querier) *PostgresScheduleRepository {
	return &PostgresScheduleRepository{pool: pool}
}

func (r *PostgresScheduleRepository) CreateSchedule(ctx context.Context, s *domain.Schedule) (*domain.Schedule, error) {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	now := time.Now().UTC()
	s.CreatedAt = now
	s.UpdatedAt = now

	if s.TaskSpecTemplate == nil {
		s.TaskSpecTemplate = json.RawMessage("{}")
	}
	if s.Timezone == "" {
		s.Timezone = "Asia/Kolkata"
	}
	if s.ManagerDomain == "" {
		s.ManagerDomain = "JJ"
	}

	query := `
		INSERT INTO schedules (
			id, user_id, manager_domain, name, cron_expression,
			timezone, task_spec_template, is_paused, last_run_at,
			next_run_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
		) RETURNING id, user_id, manager_domain, name, cron_expression,
		            timezone, task_spec_template, is_paused, last_run_at,
		            next_run_at, created_at, updated_at
	`
	row := r.pool.QueryRow(ctx, query,
		s.ID, s.UserID, s.ManagerDomain, s.Name, s.CronExpression,
		s.Timezone, s.TaskSpecTemplate, s.IsPaused, s.LastRunAt,
		s.NextRunAt, s.CreatedAt, s.UpdatedAt,
	)
	var out domain.Schedule
	if err := row.Scan(
		&out.ID, &out.UserID, &out.ManagerDomain, &out.Name, &out.CronExpression,
		&out.Timezone, &out.TaskSpecTemplate, &out.IsPaused, &out.LastRunAt,
		&out.NextRunAt, &out.CreatedAt, &out.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("create schedule: %w", err)
	}
	return &out, nil
}

func (r *PostgresScheduleRepository) GetScheduleByID(ctx context.Context, id uuid.UUID) (*domain.Schedule, error) {
	query := `
		SELECT id, user_id, manager_domain, name, cron_expression,
		       timezone, task_spec_template, is_paused, last_run_at,
		       next_run_at, created_at, updated_at
		FROM schedules
		WHERE id = $1
	`
	row := r.pool.QueryRow(ctx, query, id)
	var s domain.Schedule
	if err := row.Scan(
		&s.ID, &s.UserID, &s.ManagerDomain, &s.Name, &s.CronExpression,
		&s.Timezone, &s.TaskSpecTemplate, &s.IsPaused, &s.LastRunAt,
		&s.NextRunAt, &s.CreatedAt, &s.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrScheduleNotFound
		}
		return nil, fmt.Errorf("get schedule: %w", err)
	}
	return &s, nil
}

func (r *PostgresScheduleRepository) ListSchedules(ctx context.Context, userID uuid.UUID) ([]*domain.Schedule, error) {
	query := `
		SELECT id, user_id, manager_domain, name, cron_expression,
		       timezone, task_spec_template, is_paused, last_run_at,
		       next_run_at, created_at, updated_at
		FROM schedules
		WHERE user_id = $1
		ORDER BY next_run_at ASC
	`
	rows, err := r.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}
	defer rows.Close()

	var list []*domain.Schedule
	for rows.Next() {
		var s domain.Schedule
		if err := rows.Scan(
			&s.ID, &s.UserID, &s.ManagerDomain, &s.Name, &s.CronExpression,
			&s.Timezone, &s.TaskSpecTemplate, &s.IsPaused, &s.LastRunAt,
			&s.NextRunAt, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, err
		}
		list = append(list, &s)
	}
	return list, nil
}

func (r *PostgresScheduleRepository) UpdateSchedule(ctx context.Context, s *domain.Schedule) error {
	query := `
		UPDATE schedules
		SET name = $1,
		    cron_expression = $2,
		    timezone = $3,
		    task_spec_template = $4,
		    is_paused = $5,
		    next_run_at = $6,
		    updated_at = NOW()
		WHERE id = $7
	`
	cmd, err := r.pool.Exec(ctx, query,
		s.Name, s.CronExpression, s.Timezone, s.TaskSpecTemplate,
		s.IsPaused, s.NextRunAt, s.ID,
	)
	if err != nil {
		return fmt.Errorf("update schedule: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return ErrScheduleNotFound
	}
	return nil
}

func (r *PostgresScheduleRepository) SetPaused(ctx context.Context, id uuid.UUID, userID uuid.UUID, paused bool) error {
	query := `
		UPDATE schedules
		SET is_paused = $1,
		    updated_at = NOW()
		WHERE id = $2 AND user_id = $3
	`
	cmd, err := r.pool.Exec(ctx, query, paused, id, userID)
	if err != nil {
		return fmt.Errorf("set paused: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return ErrScheduleNotFound
	}
	return nil
}

func (r *PostgresScheduleRepository) DeleteSchedule(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	query := `DELETE FROM schedules WHERE id = $1 AND user_id = $2`
	cmd, err := r.pool.Exec(ctx, query, id, userID)
	if err != nil {
		return fmt.Errorf("delete schedule: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return ErrScheduleNotFound
	}
	return nil
}

func (r *PostgresScheduleRepository) ClaimDueSchedules(ctx context.Context, now time.Time, limit int) ([]*domain.Schedule, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `
		SELECT id, user_id, manager_domain, name, cron_expression,
		       timezone, task_spec_template, is_paused, last_run_at,
		       next_run_at, created_at, updated_at
		FROM schedules
		WHERE is_paused = FALSE AND next_run_at <= $1
		ORDER BY next_run_at ASC
		LIMIT $2
		FOR UPDATE SKIP LOCKED
	`
	rows, err := r.pool.Query(ctx, query, now, limit)
	if err != nil {
		return nil, fmt.Errorf("claim due schedules: %w", err)
	}
	defer rows.Close()

	var due []*domain.Schedule
	for rows.Next() {
		var s domain.Schedule
		if err := rows.Scan(
			&s.ID, &s.UserID, &s.ManagerDomain, &s.Name, &s.CronExpression,
			&s.Timezone, &s.TaskSpecTemplate, &s.IsPaused, &s.LastRunAt,
			&s.NextRunAt, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, err
		}
		due = append(due, &s)
	}
	return due, nil
}

func (r *PostgresScheduleRepository) AdvanceNextRun(ctx context.Context, id uuid.UUID, lastRun, nextRun time.Time) error {
	query := `
		UPDATE schedules
		SET last_run_at = $1,
		    next_run_at = $2,
		    updated_at = NOW()
		WHERE id = $3
	`
	cmd, err := r.pool.Exec(ctx, query, lastRun, nextRun, id)
	if err != nil {
		return fmt.Errorf("advance next run: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return ErrScheduleNotFound
	}
	return nil
}

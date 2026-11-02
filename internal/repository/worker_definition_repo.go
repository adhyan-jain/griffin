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

// WorkerDefinitionRepository handles durable persistence of specialist worker definitions and version history.
type WorkerDefinitionRepository interface {
	CreateDefinition(ctx context.Context, def *domain.WorkerDefinition) (*domain.WorkerDefinition, error)
	GetDefinitionByID(ctx context.Context, id uuid.UUID, userID uuid.UUID) (*domain.WorkerDefinition, error)
	GetDefinitionByKeyLatest(ctx context.Context, userID uuid.UUID, workerKey string) (*domain.WorkerDefinition, error)
	GetDefinitionByKeyAndVersion(ctx context.Context, userID uuid.UUID, workerKey string, version int) (*domain.WorkerDefinition, error)
	ListDefinitions(ctx context.Context, userID uuid.UUID, managerID string) ([]*domain.WorkerDefinition, error)
	ListDefinitionVersions(ctx context.Context, userID uuid.UUID, workerKey string) ([]*domain.WorkerDefinition, error)
	ToggleDefinition(ctx context.Context, id uuid.UUID, userID uuid.UUID, isActive bool) (*domain.WorkerDefinition, error)
	RecordAudit(ctx context.Context, audit *domain.AgentFactoryAudit) error
	ListAudits(ctx context.Context, userID uuid.UUID, workerKey string) ([]*domain.AgentFactoryAudit, error)
}

// PostgresWorkerDefinitionRepository implements WorkerDefinitionRepository using pgxpool.
type PostgresWorkerDefinitionRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresWorkerDefinitionRepository(pool *pgxpool.Pool) *PostgresWorkerDefinitionRepository {
	return &PostgresWorkerDefinitionRepository{pool: pool}
}

func (r *PostgresWorkerDefinitionRepository) CreateDefinition(ctx context.Context, def *domain.WorkerDefinition) (*domain.WorkerDefinition, error) {
	if def.ID == uuid.Nil {
		def.ID = uuid.New()
	}
	now := time.Now().UTC()
	def.CreatedAt = now
	def.UpdatedAt = now

	if def.Version <= 0 {
		def.Version = 1
	}

	if def.AllowedCapabilities == nil {
		def.AllowedCapabilities = []string{}
	}
	if def.AllowedTools == nil {
		def.AllowedTools = []string{}
	}
	if def.VerificationCriteria == nil {
		def.VerificationCriteria = []string{}
	}

	query := `
		INSERT INTO worker_definitions (
			id, user_id, worker_key, version, manager_id, name, description,
			system_prompt, domain, input_schema, output_schema, allowed_capabilities,
			allowed_tools, execution_timeout_sec, max_retries, verification_criteria,
			is_active, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19
		) RETURNING id, user_id, worker_key, version, manager_id, name, description,
		            system_prompt, domain, input_schema, output_schema, allowed_capabilities,
		            allowed_tools, execution_timeout_sec, max_retries, verification_criteria,
		            is_active, created_at, updated_at
	`
	row := r.pool.QueryRow(ctx, query,
		def.ID, def.UserID, def.WorkerKey, def.Version, def.ManagerID, def.Name, def.Description,
		def.SystemPrompt, def.Domain, def.InputSchema, def.OutputSchema, def.AllowedCapabilities,
		def.AllowedTools, def.ExecutionTimeoutSec, def.MaxRetries, def.VerificationCriteria,
		def.IsActive, def.CreatedAt, def.UpdatedAt,
	)

	var created domain.WorkerDefinition
	err := row.Scan(
		&created.ID, &created.UserID, &created.WorkerKey, &created.Version, &created.ManagerID,
		&created.Name, &created.Description, &created.SystemPrompt, &created.Domain,
		&created.InputSchema, &created.OutputSchema, &created.AllowedCapabilities,
		&created.AllowedTools, &created.ExecutionTimeoutSec, &created.MaxRetries,
		&created.VerificationCriteria, &created.IsActive, &created.CreatedAt, &created.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create worker definition: %w", err)
	}
	return &created, nil
}

func (r *PostgresWorkerDefinitionRepository) GetDefinitionByID(ctx context.Context, id uuid.UUID, userID uuid.UUID) (*domain.WorkerDefinition, error) {
	query := `
		SELECT id, user_id, worker_key, version, manager_id, name, description,
		       system_prompt, domain, input_schema, output_schema, allowed_capabilities,
		       allowed_tools, execution_timeout_sec, max_retries, verification_criteria,
		       is_active, created_at, updated_at
		FROM worker_definitions
		WHERE id = $1 AND user_id = $2
	`
	row := r.pool.QueryRow(ctx, query, id, userID)
	return scanWorkerDefinition(row)
}

func (r *PostgresWorkerDefinitionRepository) GetDefinitionByKeyLatest(ctx context.Context, userID uuid.UUID, workerKey string) (*domain.WorkerDefinition, error) {
	query := `
		SELECT id, user_id, worker_key, version, manager_id, name, description,
		       system_prompt, domain, input_schema, output_schema, allowed_capabilities,
		       allowed_tools, execution_timeout_sec, max_retries, verification_criteria,
		       is_active, created_at, updated_at
		FROM worker_definitions
		WHERE user_id = $1 AND worker_key = $2
		ORDER BY version DESC
		LIMIT 1
	`
	row := r.pool.QueryRow(ctx, query, userID, workerKey)
	return scanWorkerDefinition(row)
}

func (r *PostgresWorkerDefinitionRepository) GetDefinitionByKeyAndVersion(ctx context.Context, userID uuid.UUID, workerKey string, version int) (*domain.WorkerDefinition, error) {
	query := `
		SELECT id, user_id, worker_key, version, manager_id, name, description,
		       system_prompt, domain, input_schema, output_schema, allowed_capabilities,
		       allowed_tools, execution_timeout_sec, max_retries, verification_criteria,
		       is_active, created_at, updated_at
		FROM worker_definitions
		WHERE user_id = $1 AND worker_key = $2 AND version = $3
	`
	row := r.pool.QueryRow(ctx, query, userID, workerKey, version)
	return scanWorkerDefinition(row)
}

func (r *PostgresWorkerDefinitionRepository) ListDefinitions(ctx context.Context, userID uuid.UUID, managerID string) ([]*domain.WorkerDefinition, error) {
	// Returns the latest version of each worker definition
	query := `
		SELECT DISTINCT ON (worker_key)
		       id, user_id, worker_key, version, manager_id, name, description,
		       system_prompt, domain, input_schema, output_schema, allowed_capabilities,
		       allowed_tools, execution_timeout_sec, max_retries, verification_criteria,
		       is_active, created_at, updated_at
		FROM worker_definitions
		WHERE user_id = $1 AND ($2 = '' OR manager_id = $2)
		ORDER BY worker_key, version DESC
	`
	rows, err := r.pool.Query(ctx, query, userID, managerID)
	if err != nil {
		return nil, fmt.Errorf("list worker definitions: %w", err)
	}
	defer rows.Close()

	var defs []*domain.WorkerDefinition
	for rows.Next() {
		def, err := scanWorkerDefinition(rows)
		if err != nil {
			return nil, err
		}
		defs = append(defs, def)
	}
	return defs, nil
}

func (r *PostgresWorkerDefinitionRepository) ListDefinitionVersions(ctx context.Context, userID uuid.UUID, workerKey string) ([]*domain.WorkerDefinition, error) {
	query := `
		SELECT id, user_id, worker_key, version, manager_id, name, description,
		       system_prompt, domain, input_schema, output_schema, allowed_capabilities,
		       allowed_tools, execution_timeout_sec, max_retries, verification_criteria,
		       is_active, created_at, updated_at
		FROM worker_definitions
		WHERE user_id = $1 AND worker_key = $2
		ORDER BY version DESC
	`
	rows, err := r.pool.Query(ctx, query, userID, workerKey)
	if err != nil {
		return nil, fmt.Errorf("list worker versions: %w", err)
	}
	defer rows.Close()

	var defs []*domain.WorkerDefinition
	for rows.Next() {
		def, err := scanWorkerDefinition(rows)
		if err != nil {
			return nil, err
		}
		defs = append(defs, def)
	}
	return defs, nil
}

func (r *PostgresWorkerDefinitionRepository) ToggleDefinition(ctx context.Context, id uuid.UUID, userID uuid.UUID, isActive bool) (*domain.WorkerDefinition, error) {
	now := time.Now().UTC()
	query := `
		UPDATE worker_definitions
		SET is_active = $1, updated_at = $2
		WHERE id = $3 AND user_id = $4
		RETURNING id, user_id, worker_key, version, manager_id, name, description,
		          system_prompt, domain, input_schema, output_schema, allowed_capabilities,
		          allowed_tools, execution_timeout_sec, max_retries, verification_criteria,
		          is_active, created_at, updated_at
	`
	row := r.pool.QueryRow(ctx, query, isActive, now, id, userID)
	return scanWorkerDefinition(row)
}

func (r *PostgresWorkerDefinitionRepository) RecordAudit(ctx context.Context, audit *domain.AgentFactoryAudit) error {
	now := time.Now().UTC()
	audit.CreatedAt = now

	if audit.Changes == nil {
		audit.Changes = json.RawMessage("{}")
	}

	query := `
		INSERT INTO agent_factory_audits (
			user_id, worker_definition_id, worker_key, version, action, actor, changes, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8
		) RETURNING id
	`
	err := r.pool.QueryRow(ctx, query,
		audit.UserID, audit.WorkerDefinitionID, audit.WorkerKey, audit.Version,
		audit.Action, audit.Actor, audit.Changes, audit.CreatedAt,
	).Scan(&audit.ID)
	if err != nil {
		return fmt.Errorf("record agent factory audit: %w", err)
	}
	return nil
}

func (r *PostgresWorkerDefinitionRepository) ListAudits(ctx context.Context, userID uuid.UUID, workerKey string) ([]*domain.AgentFactoryAudit, error) {
	query := `
		SELECT id, user_id, worker_definition_id, worker_key, version, action, actor, changes, created_at
		FROM agent_factory_audits
		WHERE user_id = $1 AND ($2 = '' OR worker_key = $2)
		ORDER BY created_at DESC
	`
	rows, err := r.pool.Query(ctx, query, userID, workerKey)
	if err != nil {
		return nil, fmt.Errorf("list agent factory audits: %w", err)
	}
	defer rows.Close()

	var audits []*domain.AgentFactoryAudit
	for rows.Next() {
		var a domain.AgentFactoryAudit
		if err := rows.Scan(
			&a.ID, &a.UserID, &a.WorkerDefinitionID, &a.WorkerKey, &a.Version,
			&a.Action, &a.Actor, &a.Changes, &a.CreatedAt,
		); err != nil {
			return nil, err
		}
		audits = append(audits, &a)
	}
	return audits, nil
}

func scanWorkerDefinition(row pgx.Row) (*domain.WorkerDefinition, error) {
	var d domain.WorkerDefinition
	err := row.Scan(
		&d.ID, &d.UserID, &d.WorkerKey, &d.Version, &d.ManagerID,
		&d.Name, &d.Description, &d.SystemPrompt, &d.Domain,
		&d.InputSchema, &d.OutputSchema, &d.AllowedCapabilities,
		&d.AllowedTools, &d.ExecutionTimeoutSec, &d.MaxRetries,
		&d.VerificationCriteria, &d.IsActive, &d.CreatedAt, &d.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("scan worker definition: %w", err)
	}
	return &d, nil
}

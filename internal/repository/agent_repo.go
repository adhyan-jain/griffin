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

// AgentRepository defines the persistence contract for registered agents.
type AgentRepository interface {
	RegisterAgent(ctx context.Context, agent *domain.Agent) (*domain.Agent, error)
	GetAgentByID(ctx context.Context, id uuid.UUID) (*domain.Agent, error)
	ListAgents(ctx context.Context, userID uuid.UUID) ([]*domain.Agent, error)
}

// PostgresAgentRepository implements AgentRepository.
type PostgresAgentRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresAgentRepository(pool *pgxpool.Pool) *PostgresAgentRepository {
	return &PostgresAgentRepository{pool: pool}
}

func (r *PostgresAgentRepository) RegisterAgent(ctx context.Context, a *domain.Agent) (*domain.Agent, error) {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	now := time.Now().UTC()
	a.CreatedAt = now
	a.UpdatedAt = now

	query := `
		INSERT INTO agents (
			id, user_id, name, role_type, manager_domain, description, system_prompt,
			provider_preference, model_preference, max_execution_timeout_sec, is_active,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
		) RETURNING id, user_id, name, role_type, manager_domain, description, system_prompt,
		            provider_preference, model_preference, max_execution_timeout_sec, is_active,
		            created_at, updated_at
	`
	row := r.pool.QueryRow(ctx, query,
		a.ID, a.UserID, a.Name, a.RoleType, a.ManagerDomain, a.Description, a.SystemPrompt,
		a.ProviderPreference, a.ModelPreference, a.MaxExecutionTimeoutSec, a.IsActive,
		a.CreatedAt, a.UpdatedAt,
	)

	var created domain.Agent
	err := row.Scan(
		&created.ID, &created.UserID, &created.Name, &created.RoleType, &created.ManagerDomain,
		&created.Description, &created.SystemPrompt, &created.ProviderPreference,
		&created.ModelPreference, &created.MaxExecutionTimeoutSec, &created.IsActive,
		&created.CreatedAt, &created.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("register agent: %w", err)
	}
	return &created, nil
}

func (r *PostgresAgentRepository) GetAgentByID(ctx context.Context, id uuid.UUID) (*domain.Agent, error) {
	query := `
		SELECT id, user_id, name, role_type, manager_domain, description, system_prompt,
		       provider_preference, model_preference, max_execution_timeout_sec, is_active,
		       created_at, updated_at
		FROM agents
		WHERE id = $1
	`
	row := r.pool.QueryRow(ctx, query, id)
	var a domain.Agent
	err := row.Scan(
		&a.ID, &a.UserID, &a.Name, &a.RoleType, &a.ManagerDomain,
		&a.Description, &a.SystemPrompt, &a.ProviderPreference,
		&a.ModelPreference, &a.MaxExecutionTimeoutSec, &a.IsActive,
		&a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get agent by id: %w", err)
	}
	return &a, nil
}

func (r *PostgresAgentRepository) ListAgents(ctx context.Context, userID uuid.UUID) ([]*domain.Agent, error) {
	query := `
		SELECT id, user_id, name, role_type, manager_domain, description, system_prompt,
		       provider_preference, model_preference, max_execution_timeout_sec, is_active,
		       created_at, updated_at
		FROM agents
		WHERE user_id = $1 AND is_active = TRUE
		ORDER BY name ASC
	`
	rows, err := r.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()

	var agents []*domain.Agent
	for rows.Next() {
		var a domain.Agent
		if err := rows.Scan(
			&a.ID, &a.UserID, &a.Name, &a.RoleType, &a.ManagerDomain,
			&a.Description, &a.SystemPrompt, &a.ProviderPreference,
			&a.ModelPreference, &a.MaxExecutionTimeoutSec, &a.IsActive,
			&a.CreatedAt, &a.UpdatedAt,
		); err != nil {
			return nil, err
		}
		agents = append(agents, &a)
	}
	return agents, nil
}

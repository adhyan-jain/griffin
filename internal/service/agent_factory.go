package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/manager"
	"github.com/griffin/core/internal/repository"
)

// AgentFactoryService manages creation, versioning, auditing, and task instantiation for specialist workers.
type AgentFactoryService struct {
	workerRepo repository.WorkerDefinitionRepository
	mgrReg     *manager.Registry
	logger     *slog.Logger
}

// NewAgentFactoryService constructs a new AgentFactoryService.
func NewAgentFactoryService(workerRepo repository.WorkerDefinitionRepository, mgrReg *manager.Registry, logger *slog.Logger) *AgentFactoryService {
	if logger == nil {
		logger = slog.Default()
	}
	return &AgentFactoryService{
		workerRepo: workerRepo,
		mgrReg:     mgrReg,
		logger:     logger,
	}
}

// CreateWorkerDefinition validates, registers, and audits a new specialist worker definition.
func (f *AgentFactoryService) CreateWorkerDefinition(ctx context.Context, def *domain.WorkerDefinition, actor string) (*domain.WorkerDefinition, error) {
	if def == nil {
		return nil, fmt.Errorf("%w: worker definition cannot be nil", domain.ErrValidation)
	}

	// 1. Domain validation
	if err := def.Validate(); err != nil {
		return nil, err
	}

	// 2. Manager registry check
	if !f.mgrReg.IsKnownManager(def.ManagerID) {
		return nil, fmt.Errorf("%w: unknown manager_id '%s'", domain.ErrValidation, def.ManagerID)
	}

	// 3. Governance: Actor validation
	actor = strings.ToUpper(strings.TrimSpace(actor))
	if actor == "" {
		actor = "USER"
	}
	if actor != "USER" && actor != "JJ" && actor != "SYSTEM" {
		return nil, fmt.Errorf("%w: unauthorized actor '%s'", domain.ErrForbidden, actor)
	}

	// Ensure clean initial state
	def.Version = 1
	def.IsActive = true

	// 4. Check if worker key already exists for user
	existing, err := f.workerRepo.GetDefinitionByKeyLatest(ctx, def.UserID, def.WorkerKey)
	if err == nil && existing != nil {
		return nil, fmt.Errorf("%w: worker_key '%s' already exists; use Update to create a new version", domain.ErrConflict, def.WorkerKey)
	}

	// 5. Persist definition
	created, err := f.workerRepo.CreateDefinition(ctx, def)
	if err != nil {
		return nil, fmt.Errorf("agent_factory: create definition: %w", err)
	}

	// 6. Record audit
	changes, _ := json.Marshal(map[string]any{
		"name":         created.Name,
		"manager_id":   created.ManagerID,
		"capabilities": created.AllowedCapabilities,
		"tools":        created.AllowedTools,
	})
	_ = f.workerRepo.RecordAudit(ctx, &domain.AgentFactoryAudit{
		UserID:             created.UserID,
		WorkerDefinitionID: &created.ID,
		WorkerKey:          created.WorkerKey,
		Version:            created.Version,
		Action:             "CREATED",
		Actor:              actor,
		Changes:            changes,
	})

	f.logger.Info("Registered new specialist worker definition", "worker_key", created.WorkerKey, "manager_id", created.ManagerID, "version", created.Version)
	return created, nil
}

// UpdateWorkerDefinition versions an existing worker definition, creating an immutable new version record.
func (f *AgentFactoryService) UpdateWorkerDefinition(ctx context.Context, userID uuid.UUID, workerKey string, updates domain.WorkerDefinitionUpdate, actor string) (*domain.WorkerDefinition, error) {
	actor = strings.ToUpper(strings.TrimSpace(actor))
	if actor == "" {
		actor = "USER"
	}

	// Governance: Workers cannot modify their own permissions or definition
	if actor != "USER" && actor != "JJ" && actor != "SYSTEM" {
		return nil, fmt.Errorf("%w: unauthorized actor '%s' cannot update worker definitions", domain.ErrForbidden, actor)
	}

	// 1. Fetch current latest version
	latest, err := f.workerRepo.GetDefinitionByKeyLatest(ctx, userID, workerKey)
	if err != nil {
		return nil, fmt.Errorf("agent_factory: get latest worker: %w", err)
	}

	// 2. Clone and apply updates to new version
	newVersion := &domain.WorkerDefinition{
		UserID:               latest.UserID,
		WorkerKey:            latest.WorkerKey,
		Version:              latest.Version + 1,
		ManagerID:            latest.ManagerID,
		Name:                 latest.Name,
		Description:          latest.Description,
		SystemPrompt:         latest.SystemPrompt,
		Domain:               latest.Domain,
		InputSchema:          latest.InputSchema,
		OutputSchema:         latest.OutputSchema,
		AllowedCapabilities:  latest.AllowedCapabilities,
		AllowedTools:         latest.AllowedTools,
		ExecutionTimeoutSec:  latest.ExecutionTimeoutSec,
		MaxRetries:           latest.MaxRetries,
		VerificationCriteria: latest.VerificationCriteria,
		IsActive:             latest.IsActive,
	}

	changesMap := make(map[string]any)

	if updates.Name != nil && strings.TrimSpace(*updates.Name) != "" {
		changesMap["name"] = *updates.Name
		newVersion.Name = *updates.Name
	}
	if updates.Description != nil {
		changesMap["description"] = *updates.Description
		newVersion.Description = *updates.Description
	}
	if updates.SystemPrompt != nil && strings.TrimSpace(*updates.SystemPrompt) != "" {
		changesMap["system_prompt"] = *updates.SystemPrompt
		newVersion.SystemPrompt = *updates.SystemPrompt
	}
	if updates.InputSchema != nil && len(*updates.InputSchema) > 0 {
		changesMap["input_schema"] = *updates.InputSchema
		newVersion.InputSchema = *updates.InputSchema
	}
	if updates.OutputSchema != nil && len(*updates.OutputSchema) > 0 {
		changesMap["output_schema"] = *updates.OutputSchema
		newVersion.OutputSchema = *updates.OutputSchema
	}
	if updates.AllowedCapabilities != nil {
		changesMap["allowed_capabilities"] = *updates.AllowedCapabilities
		newVersion.AllowedCapabilities = *updates.AllowedCapabilities
	}
	if updates.AllowedTools != nil {
		changesMap["allowed_tools"] = *updates.AllowedTools
		newVersion.AllowedTools = *updates.AllowedTools
	}
	if updates.ExecutionTimeoutSec != nil && *updates.ExecutionTimeoutSec > 0 {
		changesMap["execution_timeout_sec"] = *updates.ExecutionTimeoutSec
		newVersion.ExecutionTimeoutSec = *updates.ExecutionTimeoutSec
	}
	if updates.MaxRetries != nil && *updates.MaxRetries >= 0 {
		changesMap["max_retries"] = *updates.MaxRetries
		newVersion.MaxRetries = *updates.MaxRetries
	}
	if updates.VerificationCriteria != nil {
		changesMap["verification_criteria"] = *updates.VerificationCriteria
		newVersion.VerificationCriteria = *updates.VerificationCriteria
	}

	// 3. Validate new version
	if err := newVersion.Validate(); err != nil {
		return nil, err
	}

	// 4. Persist new immutable version
	created, err := f.workerRepo.CreateDefinition(ctx, newVersion)
	if err != nil {
		return nil, fmt.Errorf("agent_factory: persist new version: %w", err)
	}

	// 5. Audit log
	changesJSON, _ := json.Marshal(changesMap)
	_ = f.workerRepo.RecordAudit(ctx, &domain.AgentFactoryAudit{
		UserID:             created.UserID,
		WorkerDefinitionID: &created.ID,
		WorkerKey:          created.WorkerKey,
		Version:            created.Version,
		Action:             "UPDATED",
		Actor:              actor,
		Changes:            changesJSON,
	})

	f.logger.Info("Updated worker definition to new version", "worker_key", created.WorkerKey, "version", created.Version)
	return created, nil
}

// ToggleWorker enables or disables a specialist worker definition.
func (f *AgentFactoryService) ToggleWorker(ctx context.Context, id uuid.UUID, userID uuid.UUID, isActive bool, actor string) (*domain.WorkerDefinition, error) {
	actor = strings.ToUpper(strings.TrimSpace(actor))
	if actor == "" {
		actor = "USER"
	}

	updated, err := f.workerRepo.ToggleDefinition(ctx, id, userID, isActive)
	if err != nil {
		return nil, fmt.Errorf("agent_factory: toggle worker: %w", err)
	}

	changes, _ := json.Marshal(map[string]any{"is_active": isActive})
	_ = f.workerRepo.RecordAudit(ctx, &domain.AgentFactoryAudit{
		UserID:             updated.UserID,
		WorkerDefinitionID: &updated.ID,
		WorkerKey:          updated.WorkerKey,
		Version:            updated.Version,
		Action:             "TOGGLED",
		Actor:              actor,
		Changes:            changes,
	})

	return updated, nil
}

// GetWorkerDefinition retrieves a worker definition by ID.
func (f *AgentFactoryService) GetWorkerDefinition(ctx context.Context, id uuid.UUID, userID uuid.UUID) (*domain.WorkerDefinition, error) {
	return f.workerRepo.GetDefinitionByID(ctx, id, userID)
}

// GetWorkerDefinitionByKey retrieves the latest version of a worker definition by key.
func (f *AgentFactoryService) GetWorkerDefinitionByKey(ctx context.Context, userID uuid.UUID, workerKey string) (*domain.WorkerDefinition, error) {
	return f.workerRepo.GetDefinitionByKeyLatest(ctx, userID, workerKey)
}

// ListWorkerDefinitions returns the latest versions of all worker definitions for a user.
func (f *AgentFactoryService) ListWorkerDefinitions(ctx context.Context, userID uuid.UUID, managerID string) ([]*domain.WorkerDefinition, error) {
	return f.workerRepo.ListDefinitions(ctx, userID, managerID)
}

// ListWorkerVersions returns the full version history for a worker key.
func (f *AgentFactoryService) ListWorkerVersions(ctx context.Context, userID uuid.UUID, workerKey string) ([]*domain.WorkerDefinition, error) {
	return f.workerRepo.ListDefinitionVersions(ctx, userID, workerKey)
}

// InstantiateTask creates a ready-to-execute domain.Task initialized with the worker's prompts, capabilities, and schemas.
func (f *AgentFactoryService) InstantiateTask(
	ctx context.Context,
	def *domain.WorkerDefinition,
	parentTaskID *uuid.UUID,
	rootTaskID *uuid.UUID,
	title string,
	goal string,
	userPayload json.RawMessage,
	priority domain.TaskPriority,
) (*domain.Task, error) {
	if def == nil {
		return nil, fmt.Errorf("%w: worker definition cannot be nil", domain.ErrValidation)
	}
	if !def.IsActive {
		return nil, fmt.Errorf("%w: worker '%s' is currently disabled", domain.ErrValidation, def.WorkerKey)
	}

	if strings.TrimSpace(title) == "" {
		title = fmt.Sprintf("[%s] %s", def.Name, goal)
	}

	// Assemble execution payload embedding worker instructions, schemas, and capabilities
	var inputMap map[string]any
	if len(userPayload) > 0 && string(userPayload) != "{}" && string(userPayload) != "null" {
		_ = json.Unmarshal(userPayload, &inputMap)
	}
	if inputMap == nil {
		inputMap = make(map[string]any)
	}

	inputMap["_worker_key"] = def.WorkerKey
	inputMap["_worker_version"] = def.Version
	inputMap["_system_prompt"] = def.SystemPrompt
	inputMap["_capabilities"] = def.AllowedCapabilities
	inputMap["_tools"] = def.AllowedTools
	inputMap["_output_schema"] = def.OutputSchema

	assembledInput, err := json.Marshal(inputMap)
	if err != nil {
		assembledInput = json.RawMessage("{}")
	}

	maxRetries := def.MaxRetries
	if maxRetries <= 0 {
		maxRetries = domain.DefaultMaxRetries
	}

	task := &domain.Task{
		UserID:        def.UserID,
		ParentID:      parentTaskID,
		RootID:        rootTaskID,
		ManagerDomain: def.ManagerID,
		Title:         title,
		Goal:          goal,
		InputPayload:  assembledInput,
		Priority:      priority,
		MaxRetries:    maxRetries,
	}

	return task, nil
}

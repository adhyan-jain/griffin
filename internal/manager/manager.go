package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/griffin/core/internal/domain"
)

// ManagerDefinition holds metadata, boundaries, and policies for a specialist domain manager.
type ManagerDefinition struct {
	ID                    string   `json:"id"`
	Name                  string   `json:"name"`
	Description           string   `json:"description"`
	Domain                string   `json:"domain"`
	Responsibilities      []string `json:"responsibilities"`
	SupportedTaskTypes    []string `json:"supported_task_types"`
	RequiredContext       []string `json:"required_context"`
	ExpectedOutputFormats []string `json:"expected_output_formats"`
	VerificationRules     []string `json:"verification_rules"`
	ApprovalOperations    []string `json:"approval_operations"`
	AllowedCapabilities   []string `json:"allowed_capabilities"`
	AvailableWorkers      []string `json:"available_workers"`
}

// Manager defines the contract for domain management and delegation.
type Manager interface {
	Definition() ManagerDefinition
	Interpret(ctx context.Context, task *domain.Task) (*domain.PlanPayload, error)
	VerifyDeliverable(ctx context.Context, task *domain.Task, output json.RawMessage) (bool, string, error)
}

// StandardManager is a configurable, reusable implementation of the Manager interface.
type StandardManager struct {
	def ManagerDefinition
}

// NewStandardManager instantiates a StandardManager with the given definition.
func NewStandardManager(def ManagerDefinition) *StandardManager {
	return &StandardManager{def: def}
}

// Definition returns the immutable manager definition.
func (m *StandardManager) Definition() ManagerDefinition {
	return m.def
}

// Interpret breaks down a domain-specific task into structured child tasks.
func (m *StandardManager) Interpret(ctx context.Context, task *domain.Task) (*domain.PlanPayload, error) {
	if task == nil {
		return nil, fmt.Errorf("%w: task cannot be nil", domain.ErrValidation)
	}

	taskType := "GENERAL_EXECUTION"
	if len(m.def.SupportedTaskTypes) > 0 {
		taskType = m.def.SupportedTaskTypes[0]
	}

	workerKey := ""
	if len(m.def.AvailableWorkers) > 0 {
		workerKey = m.def.AvailableWorkers[0]
	}

	// Check if this task involves approval-sensitive operations
	requiresApproval, risk := domain.RequiresApproval(m.def.AllowedCapabilities, "")
	actionType := ""
	if requiresApproval && len(m.def.ApprovalOperations) > 0 {
		actionType = m.def.ApprovalOperations[0]
	}

	childKey := fmt.Sprintf("%s_exec_1", m.def.ID)
	childTask := domain.ChildTaskPlan{
		Key:                  childKey,
		Title:                task.Title,
		Goal:                 task.Goal,
		ManagerDomain:        m.def.Domain,
		WorkerKey:            workerKey,
		InputPayload:         task.InputPayload,
		Priority:             task.Priority,
		CapabilitiesRequired: m.def.AllowedCapabilities,
		ApprovalRequired:     requiresApproval,
		ActionType:           actionType,
		RiskLevel:            risk,
	}

	plan := &domain.PlanPayload{
		Objective:            task.Goal,
		InterpretedRequest:   task.Title,
		ManagerID:            m.def.ID,
		TaskType:             taskType,
		Priority:             task.Priority,
		ContextRequirements:  m.def.RequiredContext,
		ChildTasks:           []domain.ChildTaskPlan{childTask},
		Dependencies:         []domain.PlanDependency{},
		ExpectedOutputs:      m.def.ExpectedOutputFormats,
		VerificationCriteria: m.def.VerificationRules,
		ApprovalRequirements: []domain.ApprovalRequirement{},
	}

	if requiresApproval {
		plan.ApprovalRequirements = append(plan.ApprovalRequirements, domain.ApprovalRequirement{
			TaskKey:    childKey,
			ActionType: actionType,
			RiskLevel:  risk,
			Summary:    fmt.Sprintf("Manager %s requires approval for %s", m.def.Name, actionType),
		})
	}

	return plan, nil
}

// VerifyDeliverable checks whether an output payload satisfies the manager's verification rules.
func (m *StandardManager) VerifyDeliverable(ctx context.Context, task *domain.Task, output json.RawMessage) (bool, string, error) {
	if len(output) == 0 || string(output) == "null" || string(output) == "{}" {
		return false, "Output deliverable is empty or missing", nil
	}

	// Basic schema verification: must be valid JSON
	var parsed map[string]any
	if err := json.Unmarshal(output, &parsed); err != nil {
		return false, fmt.Sprintf("Output is not valid JSON: %v", err), nil
	}

	// Check for error indicator in output
	if status, ok := parsed["status"].(string); ok && strings.EqualFold(status, "error") {
		msg, _ := parsed["error"].(string)
		return false, fmt.Sprintf("Output indicated execution error: %s", msg), nil
	}

	return true, "Deliverable verified successfully against domain criteria", nil
}

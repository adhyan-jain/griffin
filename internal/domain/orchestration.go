package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// OrchestrationStatus represents the high-level lifecycle state of a JJ Orchestration request.
type OrchestrationStatus string

const (
	OrchestrationStatusPlanning         OrchestrationStatus = "PLANNING"
	OrchestrationStatusDelegated        OrchestrationStatus = "DELEGATED"
	OrchestrationStatusRunning          OrchestrationStatus = "RUNNING"
	OrchestrationStatusAwaitingApproval OrchestrationStatus = "AWAITING_APPROVAL"
	OrchestrationStatusVerifying        OrchestrationStatus = "VERIFYING"
	OrchestrationStatusCompleted        OrchestrationStatus = "COMPLETED"
	OrchestrationStatusFailed           OrchestrationStatus = "FAILED"
	OrchestrationStatusCancelled        OrchestrationStatus = "CANCELLED"
)

// PlanStatus represents the validation and execution status of a structured execution plan.
type PlanStatus string

const (
	PlanStatusDraft     PlanStatus = "DRAFT"
	PlanStatusValidated PlanStatus = "VALIDATED"
	PlanStatusRejected  PlanStatus = "REJECTED"
	PlanStatusExecuted  PlanStatus = "EXECUTED"
)

// UserRequest represents an incoming user goal submitted to JJ.
type UserRequest struct {
	UserID      uuid.UUID       `json:"user_id"`
	WorkspaceID *uuid.UUID      `json:"workspace_id,omitempty"`
	RequestText string          `json:"request_text"`
	Context     json.RawMessage `json:"context,omitempty"`
	Priority    TaskPriority    `json:"priority,omitempty"`
}

// OrchestrationRecord tracks the full end-to-end orchestration flow for a user request.
type OrchestrationRecord struct {
	ID            uuid.UUID           `json:"id"`
	UserID        uuid.UUID           `json:"user_id"`
	WorkspaceID   *uuid.UUID          `json:"workspace_id,omitempty"`
	RootTaskID    uuid.UUID           `json:"root_task_id"`
	RequestText   string              `json:"request_text"`
	ManagerID     string              `json:"manager_id"`
	Status        OrchestrationStatus `json:"status"`
	FinalResponse *string             `json:"final_response,omitempty"`
	ErrorMessage  *string             `json:"error_message,omitempty"`
	CreatedAt     time.Time           `json:"created_at"`
	UpdatedAt     time.Time           `json:"updated_at"`
}

// ChildTaskPlan defines an individual decomposed child task within a structured execution plan.
type ChildTaskPlan struct {
	Key                  string            `json:"key"` // e.g. "task_1", "task_2"
	Title                string            `json:"title"`
	Goal                 string            `json:"goal"`
	ManagerDomain        string            `json:"manager_domain"`
	WorkerKey            string            `json:"worker_key,omitempty"`
	InputPayload         json.RawMessage   `json:"input_payload"`
	Priority             TaskPriority      `json:"priority"`
	CapabilitiesRequired []string          `json:"capabilities_required"`
	ApprovalRequired     bool              `json:"approval_required"`
	ActionType           string            `json:"action_type,omitempty"`
	RiskLevel            ApprovalRiskLevel `json:"risk_level,omitempty"`
}

// PlanDependency specifies that TaskKey depends on DependsOnKey.
type PlanDependency struct {
	TaskKey      string `json:"task_key"`
	DependsOnKey string `json:"depends_on_key"`
}

// ApprovalRequirement specifies human approval criteria for a planned child task.
type ApprovalRequirement struct {
	TaskKey    string            `json:"task_key"`
	ActionType string            `json:"action_type"`
	RiskLevel  ApprovalRiskLevel `json:"risk_level"`
	Summary    string            `json:"summary"`
}

// PlanPayload is the structured JSON object representing the plan decomposition.
type PlanPayload struct {
	Objective            string                `json:"objective"`
	InterpretedRequest   string                `json:"interpreted_request"`
	ManagerID            string                `json:"manager_id"`
	TaskType             string                `json:"task_type"`
	Priority             TaskPriority          `json:"priority"`
	ContextRequirements  []string              `json:"context_requirements"`
	ChildTasks           []ChildTaskPlan       `json:"child_tasks"`
	Dependencies         []PlanDependency      `json:"dependencies"`
	ExpectedOutputs      []string              `json:"expected_outputs"`
	VerificationCriteria []string              `json:"verification_criteria"`
	ApprovalRequirements []ApprovalRequirement `json:"approval_requirements"`
}

// OrchestrationPlan represents the persistent database record of an execution plan.
type OrchestrationPlan struct {
	ID                 uuid.UUID   `json:"id"`
	OrchestrationID    uuid.UUID   `json:"orchestration_id"`
	UserID             uuid.UUID   `json:"user_id"`
	Objective          string      `json:"objective"`
	InterpretedRequest string      `json:"interpreted_request"`
	ManagerID          string      `json:"manager_id"`
	TaskType           string      `json:"task_type"`
	Priority           string      `json:"priority"`
	PlanPayload        PlanPayload `json:"plan_payload"`
	Status             PlanStatus  `json:"status"`
	ValidationErrors   []string    `json:"validation_errors"`
	CreatedAt          time.Time   `json:"created_at"`
	UpdatedAt          time.Time   `json:"updated_at"`
}

// DelegationRecord represents the persistent linkage between an orchestration and an engine task.
type DelegationRecord struct {
	ID              uuid.UUID  `json:"id"`
	OrchestrationID uuid.UUID  `json:"orchestration_id"`
	UserID          uuid.UUID  `json:"user_id"`
	ParentTaskID    uuid.UUID  `json:"parent_task_id"`
	ChildTaskID     uuid.UUID  `json:"child_task_id"`
	TaskKey         string     `json:"task_key"`
	ManagerID       string     `json:"manager_id"`
	WorkerKey       string     `json:"worker_key,omitempty"`
	Status          TaskStatus `json:"status"`
	ResultSummary   *string    `json:"result_summary,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// ValidatePlan performs rigorous validation of a structured plan before persistence and delegation.
func ValidatePlan(plan *PlanPayload, allowedManagers map[string]bool) []string {
	var errs []string

	if strings.TrimSpace(plan.Objective) == "" {
		errs = append(errs, "objective cannot be empty")
	}
	if strings.TrimSpace(plan.InterpretedRequest) == "" {
		errs = append(errs, "interpreted_request cannot be empty")
	}
	if strings.TrimSpace(plan.ManagerID) == "" {
		errs = append(errs, "manager_id cannot be empty")
	} else if len(allowedManagers) > 0 && !allowedManagers[plan.ManagerID] {
		errs = append(errs, fmt.Sprintf("unknown manager_id '%s'", plan.ManagerID))
	}

	if len(plan.ChildTasks) == 0 {
		errs = append(errs, "plan must contain at least one child task")
		return errs
	}

	taskKeys := make(map[string]bool, len(plan.ChildTasks))
	for i, ct := range plan.ChildTasks {
		if strings.TrimSpace(ct.Key) == "" {
			errs = append(errs, fmt.Sprintf("child_tasks[%d]: key cannot be empty", i))
			continue
		}
		if taskKeys[ct.Key] {
			errs = append(errs, fmt.Sprintf("child_tasks[%d]: duplicate key '%s'", i, ct.Key))
		}
		taskKeys[ct.Key] = true

		if strings.TrimSpace(ct.Title) == "" {
			errs = append(errs, fmt.Sprintf("child_tasks[%s]: title cannot be empty", ct.Key))
		}
		if strings.TrimSpace(ct.Goal) == "" {
			errs = append(errs, fmt.Sprintf("child_tasks[%s]: goal cannot be empty", ct.Key))
		}
		if strings.TrimSpace(ct.ManagerDomain) == "" {
			ct.ManagerDomain = plan.ManagerID
		}

		// Validate requested capabilities
		for _, cap := range ct.CapabilitiesRequired {
			if !IsKnownCapability(cap) {
				errs = append(errs, fmt.Sprintf("child_tasks[%s]: unknown capability '%s'", ct.Key, cap))
			}
			if sensitive, _ := IsSensitiveCapability(cap); sensitive && !ct.ApprovalRequired {
				errs = append(errs, fmt.Sprintf("child_tasks[%s]: capability '%s' is sensitive and requires approval_required=true", ct.Key, cap))
			}
		}
	}

	// Validate dependencies and cycle detection
	adj := make(map[string][]string)
	inDegree := make(map[string]int)
	for k := range taskKeys {
		adj[k] = []string{}
		inDegree[k] = 0
	}

	for _, dep := range plan.Dependencies {
		if dep.TaskKey == dep.DependsOnKey {
			errs = append(errs, fmt.Sprintf("dependency loop: task '%s' cannot depend on itself", dep.TaskKey))
			continue
		}
		if !taskKeys[dep.TaskKey] {
			errs = append(errs, fmt.Sprintf("invalid dependency: task_key '%s' does not exist in child_tasks", dep.TaskKey))
			continue
		}
		if !taskKeys[dep.DependsOnKey] {
			errs = append(errs, fmt.Sprintf("invalid dependency: depends_on_key '%s' does not exist in child_tasks", dep.DependsOnKey))
			continue
		}

		adj[dep.DependsOnKey] = append(adj[dep.DependsOnKey], dep.TaskKey)
		inDegree[dep.TaskKey]++
	}

	// Kahn's algorithm for DAG cycle detection
	var queue []string
	for k, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, k)
		}
	}

	visited := 0
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		visited++

		for _, neighbor := range adj[curr] {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
			}
		}
	}

	if visited < len(taskKeys) {
		errs = append(errs, "cyclic dependency detected in child_tasks DAG")
	}

	return errs
}

package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// TaskStatus represents the lifecycle state of a task.
type TaskStatus string

const (
	TaskStatusCreated              TaskStatus = "CREATED"
	TaskStatusRouted               TaskStatus = "ROUTED"
	TaskStatusQueued               TaskStatus = "QUEUED"
	TaskStatusRunning              TaskStatus = "RUNNING"
	TaskStatusWaitingForDependency TaskStatus = "WAITING_FOR_DEPENDENCY"
	TaskStatusVerifying            TaskStatus = "VERIFYING"
	TaskStatusAwaitingApproval     TaskStatus = "AWAITING_APPROVAL"
	TaskStatusRetrying             TaskStatus = "RETRYING"
	TaskStatusCompleted            TaskStatus = "COMPLETED"
	TaskStatusFailed               TaskStatus = "FAILED"
	TaskStatusCancelled            TaskStatus = "CANCELLED"
)

// TaskPriority represents priority tier for task scheduling.
type TaskPriority string

const (
	TaskPriorityCriticalP0 TaskPriority = "CRITICAL_P0"
	TaskPriorityHighP1     TaskPriority = "HIGH_P1"
	TaskPriorityNormalP2   TaskPriority = "NORMAL_P2"
	TaskPriorityBackground TaskPriority = "BACKGROUND_P3"
)

// DefaultMaxRetries is the attempt budget for tasks a worker will execute (POST /tasks and
// orchestrated children). Coordination-only records (the JJ root task) keep 0.
const DefaultMaxRetries = 3

// TaskPriorities lists every accepted priority, most urgent first.
var TaskPriorities = []TaskPriority{
	TaskPriorityCriticalP0,
	TaskPriorityHighP1,
	TaskPriorityNormalP2,
	TaskPriorityBackground,
}

// Valid reports whether p is a known priority. The empty value is not valid on its own; callers
// default it to NORMAL_P2 (see ValidatePriority).
func (p TaskPriority) Valid() bool {
	for _, known := range TaskPriorities {
		if p == known {
			return true
		}
	}
	return false
}

// PriorityList renders the accepted priorities for error messages.
func PriorityList() string {
	allowed := make([]string, len(TaskPriorities))
	for i, known := range TaskPriorities {
		allowed[i] = string(known)
	}
	return strings.Join(allowed, ", ")
}

// ValidatePriority accepts an empty priority (it is defaulted later) or a known value, and
// otherwise returns a validation error that lists the allowed values.
func ValidatePriority(p TaskPriority) error {
	if p == "" || p.Valid() {
		return nil
	}
	return fmt.Errorf("%w: priority %q is not valid; use one of %s", ErrValidation, string(p), PriorityList())
}

// Task represents a persistent unit of work managed by Griffin.
type Task struct {
	ID              uuid.UUID       `json:"id"`
	UserID          uuid.UUID       `json:"user_id"`
	WorkspaceID     *uuid.UUID      `json:"workspace_id,omitempty"`
	ParentID        *uuid.UUID      `json:"parent_id,omitempty"`
	RootID          *uuid.UUID      `json:"root_id,omitempty"`
	AssignedAgentID *uuid.UUID      `json:"assigned_agent_id,omitempty"`
	ManagerDomain   string          `json:"manager_domain"`
	Title           string          `json:"title"`
	Goal            string          `json:"goal"`
	InputPayload    json.RawMessage `json:"input_payload"`
	OutputPayload   json.RawMessage `json:"output_payload,omitempty"`
	Status          TaskStatus      `json:"status"`
	Priority        TaskPriority    `json:"priority"`
	IdempotencyKey  *string         `json:"idempotency_key,omitempty"`

	// Distributed lease fields
	LeasedBy       *string    `json:"leased_by,omitempty"`
	LeaseExpiresAt *time.Time `json:"lease_expires_at,omitempty"`

	// Execution tracking & retries
	RetryCount    int        `json:"retry_count"`
	MaxRetries    int        `json:"max_retries"`
	FailureReason *string    `json:"failure_reason,omitempty"`
	ScheduledFor  *time.Time `json:"scheduled_for,omitempty"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Validate checks task fields for consistency and presence of required values.
func (t *Task) Validate() error {
	if strings.TrimSpace(t.Title) == "" {
		return fmt.Errorf("%w: title cannot be empty", ErrValidation)
	}
	if strings.TrimSpace(t.Goal) == "" {
		return fmt.Errorf("%w: goal cannot be empty", ErrValidation)
	}
	if strings.TrimSpace(t.ManagerDomain) == "" {
		t.ManagerDomain = "JJ"
	}
	if err := ValidatePriority(t.Priority); err != nil {
		return err
	}
	if err := ValidateRequires(t.InputPayload); err != nil {
		return err
	}
	if t.Priority == "" {
		t.Priority = TaskPriorityNormalP2
	}
	if t.MaxRetries < 0 {
		return fmt.Errorf("%w: max_retries cannot be negative", ErrValidation)
	}
	return nil
}

// IsTerminal returns true if the task has reached a terminal completion state.
func (t *Task) IsTerminal() bool {
	return t.Status == TaskStatusCompleted || t.Status == TaskStatusFailed || t.Status == TaskStatusCancelled
}

// IsValidTransition returns whether transitioning from 'from' to 'to' is allowed.
func IsValidTransition(from, to TaskStatus) bool {
	if from == to {
		return true
	}

	switch from {
	case TaskStatusCreated:
		return to == TaskStatusRouted ||
			to == TaskStatusQueued ||
			to == TaskStatusWaitingForDependency ||
			to == TaskStatusCancelled

	case TaskStatusRouted:
		return to == TaskStatusQueued ||
			to == TaskStatusWaitingForDependency ||
			to == TaskStatusCancelled

	case TaskStatusWaitingForDependency:
		return to == TaskStatusQueued ||
			to == TaskStatusAwaitingApproval ||
			to == TaskStatusCancelled

	case TaskStatusQueued:
		return to == TaskStatusRunning ||
			to == TaskStatusAwaitingApproval ||
			to == TaskStatusCancelled

	case TaskStatusRunning:
		return to == TaskStatusVerifying ||
			to == TaskStatusCompleted ||
			to == TaskStatusAwaitingApproval ||
			to == TaskStatusRetrying ||
			to == TaskStatusFailed ||
			to == TaskStatusCancelled

	case TaskStatusVerifying:
		return to == TaskStatusCompleted ||
			to == TaskStatusFailed ||
			to == TaskStatusRetrying

	case TaskStatusAwaitingApproval:
		return to == TaskStatusQueued ||
			to == TaskStatusWaitingForDependency ||
			to == TaskStatusRunning ||
			to == TaskStatusCancelled

	case TaskStatusRetrying:
		return to == TaskStatusQueued ||
			to == TaskStatusCancelled

	case TaskStatusCompleted, TaskStatusFailed, TaskStatusCancelled:
		// Terminal states cannot transition
		return false

	default:
		return false
	}
}

// TaskDependency links a dependent task to its prerequisite.
type TaskDependency struct {
	TaskID          uuid.UUID `json:"task_id"`
	DependsOnTaskID uuid.UUID `json:"depends_on_task_id"`
	CreatedAt       time.Time `json:"created_at"`
}

// TaskAttempt records a single execution run attempt of a task by a worker.
type TaskAttempt struct {
	ID               uuid.UUID  `json:"id"`
	TaskID           uuid.UUID  `json:"task_id"`
	AgentID          *uuid.UUID `json:"agent_id,omitempty"`
	WorkerID         string     `json:"worker_id"`
	AttemptNumber    int        `json:"attempt_number"`
	Status           string     `json:"status"` // 'RUNNING', 'COMPLETED', 'FAILED', 'TIMED_OUT'
	PromptTokens     int        `json:"prompt_tokens"`
	CompletionTokens int        `json:"completion_tokens"`
	DurationMs       int        `json:"duration_ms"`
	ErrorMessage     *string    `json:"error_message,omitempty"`
	StartedAt        time.Time  `json:"started_at"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

// TaskEvent represents an immutable append-only event in a task's audit ledger.
type TaskEvent struct {
	ID        int64           `json:"id"`
	TaskID    uuid.UUID       `json:"task_id"`
	EventType string          `json:"event_type"` // 'STATUS_CHANGE', 'TOOL_CALLED', 'RETRY', 'HEARTBEAT', 'LOG'
	Message   string          `json:"message"`
	Metadata  json.RawMessage `json:"metadata"`
	CreatedAt time.Time       `json:"created_at"`
}

package domain

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// TaskStatus represents the lifecycle state of a task.
type TaskStatus string

const (
	TaskStatusCreated                 TaskStatus = "CREATED"
	TaskStatusRouted                  TaskStatus = "ROUTED"
	TaskStatusQueued                  TaskStatus = "QUEUED"
	TaskStatusRunning                 TaskStatus = "RUNNING"
	TaskStatusWaitingForDependency    TaskStatus = "WAITING_FOR_DEPENDENCY"
	TaskStatusVerifying               TaskStatus = "VERIFYING"
	TaskStatusAwaitingApproval        TaskStatus = "AWAITING_APPROVAL"
	TaskStatusRetrying                TaskStatus = "RETRYING"
	TaskStatusCompleted               TaskStatus = "COMPLETED"
	TaskStatusFailed                  TaskStatus = "FAILED"
	TaskStatusCancelled               TaskStatus = "CANCELLED"
	TaskStatusBlockedAwaitingProvider TaskStatus = "BLOCKED_AWAITING_PROVIDER"
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
	ID                uuid.UUID       `json:"id"`
	UserID            uuid.UUID       `json:"user_id"`
	WorkspaceID       *uuid.UUID      `json:"workspace_id,omitempty"`
	ParentID          *uuid.UUID      `json:"parent_id,omitempty"`
	RootID            *uuid.UUID      `json:"root_id,omitempty"`
	AssignedAgentID   *uuid.UUID      `json:"assigned_agent_id,omitempty"`
	ManagerDomain     string          `json:"manager_domain"`
	Title             string          `json:"title"`
	Goal              string          `json:"goal"`
	InputPayload      json.RawMessage `json:"input_payload"`
	OutputPayload     json.RawMessage `json:"output_payload,omitempty"`
	Status            TaskStatus      `json:"status"`
	Priority          TaskPriority    `json:"priority"`
	IdempotencyKey    *string         `json:"idempotency_key,omitempty"`
	AllowPaidFallback bool            `json:"allow_paid_fallback,omitempty"`

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
	if err := ValidateMessaging(t.InputPayload); err != nil {
		return err
	}
	if err := ValidateWeb(t.InputPayload); err != nil {
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

// taskTransitions is the task lifecycle: which status a task may move to from each status, by any path. It is
// the written form of what internal/repository/task_repo.go enforces in SQL (each UPDATE names the statuses it
// accepts), and it was brought into line with that SQL on 2026-11-09 after a review found the two had drifted.
// When a transition changes, change this table, its test, and the SQL together.
//
//	claim            QUEUED | RETRYING            -> RUNNING          ClaimNextTask
//	complete         RUNNING | VERIFYING          -> COMPLETED        CompleteTask
//	fail, will retry RUNNING | VERIFYING          -> RETRYING         FailTask (retry_count + 1 < max_retries)
//	fail, final      RUNNING | VERIFYING          -> FAILED           FailTask
//	lease expired    RUNNING                      -> RETRYING|FAILED  ReapExpiredLeases
//	cancel           any non-terminal             -> CANCELLED        CancelTask
//	resume           FAILED | CANCELLED (roots)   -> QUEUED | WAITING_FOR_DEPENDENCY   ResumeTask
//	unblock          WAITING_FOR_DEPENDENCY       -> QUEUED           UnblockTaskIfEligible
//	ask the owner    RUNNING | QUEUED | WAITING   -> AWAITING_APPROVAL
//	owner answers    AWAITING_APPROVAL            -> QUEUED | WAITING_FOR_DEPENDENCY | RUNNING | CANCELLED
//
// A terminal task (COMPLETED, FAILED, CANCELLED) is finished, with one exception: a failed or cancelled
// standalone task can be resumed in place. Whether a task is such a root is decided where it is resumed (the
// SQL and the app), not by this table.
var taskTransitions = map[TaskStatus][]TaskStatus{
	TaskStatusCreated:                 {TaskStatusRouted, TaskStatusQueued, TaskStatusWaitingForDependency, TaskStatusCancelled},
	TaskStatusRouted:                  {TaskStatusQueued, TaskStatusWaitingForDependency, TaskStatusCancelled},
	TaskStatusWaitingForDependency:    {TaskStatusQueued, TaskStatusAwaitingApproval, TaskStatusCancelled},
	TaskStatusQueued:                  {TaskStatusRunning, TaskStatusAwaitingApproval, TaskStatusCancelled},
	TaskStatusRunning:                 {TaskStatusVerifying, TaskStatusCompleted, TaskStatusAwaitingApproval, TaskStatusRetrying, TaskStatusFailed, TaskStatusCancelled, TaskStatusBlockedAwaitingProvider},
	TaskStatusVerifying:               {TaskStatusCompleted, TaskStatusFailed, TaskStatusRetrying, TaskStatusCancelled},
	TaskStatusAwaitingApproval:        {TaskStatusQueued, TaskStatusWaitingForDependency, TaskStatusRunning, TaskStatusCancelled},
	TaskStatusRetrying:                {TaskStatusQueued, TaskStatusRunning, TaskStatusCancelled},
	TaskStatusBlockedAwaitingProvider: {TaskStatusQueued, TaskStatusRunning, TaskStatusCancelled, TaskStatusFailed},
	TaskStatusCompleted:               {},
	TaskStatusFailed:                  {TaskStatusQueued, TaskStatusWaitingForDependency},
	TaskStatusCancelled:               {TaskStatusQueued, TaskStatusWaitingForDependency},
}

// AllTaskStatuses lists every status, so tools and tests can be sure they have covered them all.
func AllTaskStatuses() []TaskStatus {
	out := make([]TaskStatus, 0, len(taskTransitions))
	for s := range taskTransitions {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// TaskTransitionsFrom lists where a task in status from may go next.
func TaskTransitionsFrom(from TaskStatus) []TaskStatus {
	return append([]TaskStatus(nil), taskTransitions[from]...)
}

// IsValidTransition returns whether transitioning from 'from' to 'to' is allowed.
func IsValidTransition(from, to TaskStatus) bool {
	if from == to {
		return true
	}
	for _, next := range taskTransitions[from] {
		if next == to {
			return true
		}
	}
	return false
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

var (
	// DraftFirstRegex matches user expressions requesting draft inspection or holding transmission.
	DraftFirstRegex = regexp.MustCompile(`(?i)\b(?:give me|show me|send me|let me see|can i see|read(?: out)?|tell me|check)\s+(?:the\s+)?draft\b|\b(?:draft\s+first|draft\s+only|don'?t\s+send\s+yet|do\s+not\s+send\s+yet|wait\s+before\s+sending|hold\s+on\s+before\s+sending|let\s+me\s+review\s+first|check\s+(?:the\s+)?draft\s+first|stop\s+before\s+sending)\b|\b(?:pehle|pahle)\s+(?:mujhe\s+)?draft\b|\bdraft\s+(?:dikha|dikhana|padh|batao|dekhna)\b|\b(?:abhi\s+)?(?:mat\s+bhejo|mat\s+bhejna|ruk\s+jao|rok\s+lo)\b|\bdraft\b`)
	// SteeringAddRegex matches mid-flight addition and modification requests.
	SteeringAddRegex = regexp.MustCompile(`(?i)\b(?:also\s+(?:add|mention|tell|include|say|state)|make\s+sure\s+to|don'?t\s+forget\s+to|change\s+(?:the\s+)?(?:message|email|text|it)\s+to|instead\s+of\b|add\s+that\b|aur\s+(?:ye\s+)?(?:bhi\s+)?(?:bolna|kehna|likhna|bhejna))`)
)

// SetDraftOnly marks the task as draft-only and holds outward transmission for manual approval.
func (t *Task) SetDraftOnly() {
	var p map[string]any
	if len(t.InputPayload) > 0 {
		_ = json.Unmarshal(t.InputPayload, &p)
	} else {
		p = make(map[string]any)
	}
	p["draft_only"] = true
	p["requires_approval"] = true
	t.InputPayload, _ = json.Marshal(p)
	t.UpdatedAt = time.Now().UTC()
}

// AppendSteeringNote appends a mid-flight steering directive to the task's input payload.
func (t *Task) AppendSteeringNote(note string) {
	clean := strings.TrimSpace(note)
	if clean == "" {
		return
	}
	var p map[string]any
	if len(t.InputPayload) > 0 {
		_ = json.Unmarshal(t.InputPayload, &p)
	} else {
		p = make(map[string]any)
	}
	var notes []string
	if raw, ok := p["steering_notes"].([]any); ok {
		for _, item := range raw {
			if s, ok := item.(string); ok {
				notes = append(notes, s)
			}
		}
	}
	notes = append(notes, clean)
	p["steering_notes"] = notes
	t.InputPayload, _ = json.Marshal(p)
	t.UpdatedAt = time.Now().UTC()
}

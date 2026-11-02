package domain_test

import (
	"testing"

	"github.com/griffin/core/internal/domain"
)

func TestTaskStatusTransitions(t *testing.T) {
	tests := []struct {
		name    string
		from    domain.TaskStatus
		to      domain.TaskStatus
		allowed bool
	}{
		// Valid transitions
		{"CREATED -> ROUTED", domain.TaskStatusCreated, domain.TaskStatusRouted, true},
		{"CREATED -> QUEUED", domain.TaskStatusCreated, domain.TaskStatusQueued, true},
		{"CREATED -> WAITING_FOR_DEPENDENCY", domain.TaskStatusCreated, domain.TaskStatusWaitingForDependency, true},
		{"CREATED -> CANCELLED", domain.TaskStatusCreated, domain.TaskStatusCancelled, true},

		{"ROUTED -> QUEUED", domain.TaskStatusRouted, domain.TaskStatusQueued, true},
		{"ROUTED -> WAITING_FOR_DEPENDENCY", domain.TaskStatusRouted, domain.TaskStatusWaitingForDependency, true},
		{"ROUTED -> CANCELLED", domain.TaskStatusRouted, domain.TaskStatusCancelled, true},

		{"WAITING_FOR_DEPENDENCY -> QUEUED", domain.TaskStatusWaitingForDependency, domain.TaskStatusQueued, true},
		{"WAITING_FOR_DEPENDENCY -> CANCELLED", domain.TaskStatusWaitingForDependency, domain.TaskStatusCancelled, true},

		{"QUEUED -> RUNNING", domain.TaskStatusQueued, domain.TaskStatusRunning, true},
		{"QUEUED -> CANCELLED", domain.TaskStatusQueued, domain.TaskStatusCancelled, true},

		{"RUNNING -> VERIFYING", domain.TaskStatusRunning, domain.TaskStatusVerifying, true},
		{"RUNNING -> COMPLETED", domain.TaskStatusRunning, domain.TaskStatusCompleted, true},
		{"RUNNING -> AWAITING_APPROVAL", domain.TaskStatusRunning, domain.TaskStatusAwaitingApproval, true},
		{"RUNNING -> RETRYING", domain.TaskStatusRunning, domain.TaskStatusRetrying, true},
		{"RUNNING -> FAILED", domain.TaskStatusRunning, domain.TaskStatusFailed, true},
		{"RUNNING -> CANCELLED", domain.TaskStatusRunning, domain.TaskStatusCancelled, true},

		{"VERIFYING -> COMPLETED", domain.TaskStatusVerifying, domain.TaskStatusCompleted, true},
		{"VERIFYING -> FAILED", domain.TaskStatusVerifying, domain.TaskStatusFailed, true},
		{"VERIFYING -> RETRYING", domain.TaskStatusVerifying, domain.TaskStatusRetrying, true},

		{"AWAITING_APPROVAL -> QUEUED", domain.TaskStatusAwaitingApproval, domain.TaskStatusQueued, true},
		{"AWAITING_APPROVAL -> RUNNING", domain.TaskStatusAwaitingApproval, domain.TaskStatusRunning, true},
		{"AWAITING_APPROVAL -> CANCELLED", domain.TaskStatusAwaitingApproval, domain.TaskStatusCancelled, true},

		{"RETRYING -> QUEUED", domain.TaskStatusRetrying, domain.TaskStatusQueued, true},
		{"RETRYING -> CANCELLED", domain.TaskStatusRetrying, domain.TaskStatusCancelled, true},

		// Terminal states cannot transition
		{"COMPLETED -> RUNNING", domain.TaskStatusCompleted, domain.TaskStatusRunning, false},
		{"COMPLETED -> QUEUED", domain.TaskStatusCompleted, domain.TaskStatusQueued, false},
		{"FAILED -> RUNNING", domain.TaskStatusFailed, domain.TaskStatusRunning, false},
		{"CANCELLED -> QUEUED", domain.TaskStatusCancelled, domain.TaskStatusQueued, false},

		// Invalid leaps
		{"CREATED -> COMPLETED", domain.TaskStatusCreated, domain.TaskStatusCompleted, false},
		{"QUEUED -> COMPLETED", domain.TaskStatusQueued, domain.TaskStatusCompleted, false},
		{"WAITING_FOR_DEPENDENCY -> RUNNING", domain.TaskStatusWaitingForDependency, domain.TaskStatusRunning, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := domain.IsValidTransition(tc.from, tc.to)
			if got != tc.allowed {
				t.Errorf("IsValidTransition(%s, %s) = %v; want %v", tc.from, tc.to, got, tc.allowed)
			}
		})
	}
}

func TestTaskValidation(t *testing.T) {
	task := domain.Task{
		Title:         "Test Task",
		Goal:          "Run unit tests",
		ManagerDomain: "JJ",
		Status:        domain.TaskStatusCreated,
		Priority:      domain.TaskPriorityNormalP2,
		MaxRetries:    3,
	}

	if err := task.Validate(); err != nil {
		t.Fatalf("expected valid task, got error: %v", err)
	}

	emptyTitleTask := task
	emptyTitleTask.Title = ""
	if err := emptyTitleTask.Validate(); err == nil {
		t.Fatal("expected error on empty title, got nil")
	}

	emptyGoalTask := task
	emptyGoalTask.Goal = ""
	if err := emptyGoalTask.Validate(); err == nil {
		t.Fatal("expected error on empty goal, got nil")
	}
}

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
		{"RETRYING -> RUNNING (a worker claims a retry)", domain.TaskStatusRetrying, domain.TaskStatusRunning, true},
		{"RETRYING -> CANCELLED", domain.TaskStatusRetrying, domain.TaskStatusCancelled, true},
		{"VERIFYING -> CANCELLED", domain.TaskStatusVerifying, domain.TaskStatusCancelled, true},

		// Resume: a failed or cancelled standalone task continues in place
		{"FAILED -> QUEUED (resume)", domain.TaskStatusFailed, domain.TaskStatusQueued, true},
		{"CANCELLED -> QUEUED (resume)", domain.TaskStatusCancelled, domain.TaskStatusQueued, true},
		{"FAILED -> WAITING_FOR_DEPENDENCY (resume)", domain.TaskStatusFailed, domain.TaskStatusWaitingForDependency, true},

		// Otherwise terminal states are finished
		{"COMPLETED -> RUNNING", domain.TaskStatusCompleted, domain.TaskStatusRunning, false},
		{"COMPLETED -> QUEUED", domain.TaskStatusCompleted, domain.TaskStatusQueued, false},
		{"FAILED -> RUNNING", domain.TaskStatusFailed, domain.TaskStatusRunning, false},
		{"FAILED -> COMPLETED", domain.TaskStatusFailed, domain.TaskStatusCompleted, false},
		{"CANCELLED -> RUNNING", domain.TaskStatusCancelled, domain.TaskStatusRunning, false},

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

// Every status has an entry, every target is a real status, only COMPLETED is a dead end, and the statuses a task
// can be cancelled from are exactly the non-terminal ones (what CancelTask's SQL accepts).
func TestTaskLifecycleTableIsComplete(t *testing.T) {
	all := domain.AllTaskStatuses()
	if len(all) != 11 {
		t.Fatalf("expected 11 statuses, got %d: %v", len(all), all)
	}
	known := map[domain.TaskStatus]bool{}
	for _, s := range all {
		known[s] = true
	}
	for _, from := range all {
		for _, to := range domain.TaskTransitionsFrom(from) {
			if !known[to] {
				t.Errorf("%s -> %s: not a real status", from, to)
			}
		}
		isTerminal := (&domain.Task{Status: from}).IsTerminal()
		cancellable := domain.IsValidTransition(from, domain.TaskStatusCancelled) && from != domain.TaskStatusCancelled
		if cancellable == isTerminal && from != domain.TaskStatusCancelled {
			t.Errorf("%s: cancellable=%v but terminal=%v; CancelTask accepts exactly the non-terminal statuses", from, cancellable, isTerminal)
		}
	}
	if got := domain.TaskTransitionsFrom(domain.TaskStatusCompleted); len(got) != 0 {
		t.Errorf("a completed task is finished, got %v", got)
	}
	for _, s := range []domain.TaskStatus{domain.TaskStatusFailed, domain.TaskStatusCancelled} {
		for _, to := range domain.TaskTransitionsFrom(s) {
			if to != domain.TaskStatusQueued && to != domain.TaskStatusWaitingForDependency {
				t.Errorf("%s may only be resumed, not moved to %s", s, to)
			}
		}
	}
	// the returned slice is a copy: callers cannot corrupt the table
	got := domain.TaskTransitionsFrom(domain.TaskStatusQueued)
	got[0] = domain.TaskStatusCompleted
	if domain.IsValidTransition(domain.TaskStatusQueued, domain.TaskStatusCompleted) {
		t.Error("TaskTransitionsFrom must return a copy")
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

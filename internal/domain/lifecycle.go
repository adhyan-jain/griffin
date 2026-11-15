package domain

// The orchestration lifecycle, written down (task moves are in task.go). Reference for developers and agents;
// lifecycle_test.go checks the statuses against what the database accepts.

// AllOrchestrationStatuses lists every orchestration status.
var AllOrchestrationStatuses = []OrchestrationStatus{
	OrchestrationStatusPlanning, OrchestrationStatusDelegated, OrchestrationStatusRunning, OrchestrationStatusAwaitingApproval,
	OrchestrationStatusVerifying, OrchestrationStatusCompleted, OrchestrationStatusFailed, OrchestrationStatusCancelled,
}

var orchMoves = map[OrchestrationStatus][]OrchestrationStatus{
	OrchestrationStatusPlanning:         {OrchestrationStatusDelegated, OrchestrationStatusFailed, OrchestrationStatusCancelled},
	OrchestrationStatusDelegated:        {OrchestrationStatusRunning, OrchestrationStatusAwaitingApproval, OrchestrationStatusFailed, OrchestrationStatusCancelled, OrchestrationStatusCompleted},
	OrchestrationStatusRunning:          {OrchestrationStatusAwaitingApproval, OrchestrationStatusVerifying, OrchestrationStatusCompleted, OrchestrationStatusFailed, OrchestrationStatusCancelled},
	OrchestrationStatusAwaitingApproval: {OrchestrationStatusRunning, OrchestrationStatusFailed, OrchestrationStatusCancelled, OrchestrationStatusCompleted},
	OrchestrationStatusVerifying:        {OrchestrationStatusCompleted, OrchestrationStatusFailed, OrchestrationStatusRunning},
}

// CanMoveOrchestration reports whether an orchestration may go from one status to another.
func CanMoveOrchestration(from, to OrchestrationStatus) bool {
	for _, t := range orchMoves[from] {
		if t == to {
			return true
		}
	}
	return false
}

// DeviceWaitLimit is how long a step that needs a laptop or phone may wait for one to come online before it is
// failed with a plain explanation, instead of sitting "Running" for days.
const DeviceWaitLimit = 6 * 60 * 60 // seconds

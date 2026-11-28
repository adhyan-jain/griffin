package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
)

// CodeTaskEngine is what finishing a task in code needs from the task engine.
type CodeTaskEngine interface {
	CompleteTask(ctx context.Context, taskID uuid.UUID, workerID string, output json.RawMessage) error
	CancelTask(ctx context.Context, taskID uuid.UUID, reason string) error
	CreateTask(ctx context.Context, task *domain.Task, dependencyIDs []uuid.UUID) (*domain.Task, error)
}

// RunInCodeUntil is RunInCode under a context that ends at deadline, creating follow-up tasks through eng.
func (w *TrackerLists) RunInCodeUntil(ctx context.Context, eng CodeTaskEngine, task *domain.Task, deadline time.Time) (string, CodeOutcome) {
	if w == nil || eng == nil || task == nil {
		return "", NotInCode
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	return w.RunInCode(ctx, task, eng.CancelTask, func(c context.Context, t *domain.Task) error {
		_, err := eng.CreateTask(c, t, nil)
		return err
	})
}

// FinishInCode records what RunInCode did to a task leased as workerID: an Answered task is completed with the
// answer. It returns Dropped or Answered when the task is finished, and NotInCode when it is not (code did not
// handle it, or the answer could not be saved), leaving the task leased for the caller to hand on.
func FinishInCode(ctx context.Context, eng CodeTaskEngine, task *domain.Task, workerID, answer string, how CodeOutcome) CodeOutcome {
	switch how {
	case Dropped:
		return Dropped
	case Answered:
		out, _ := json.Marshal(map[string]string{"response": answer})
		if err := eng.CompleteTask(ctx, task.ID, workerID, out); err != nil {
			slog.Warn("could not complete a task done in code", "task_id", task.ID, "error", err)
			return NotInCode
		}
		return Answered
	}
	return NotInCode
}

// RunClaimed runs a task its claimer already leased as workerID in code until deadline and finishes it. The
// scheduler tick uses it; the host claim route does the same two steps with its own budget.
func (w *TrackerLists) RunClaimed(ctx context.Context, eng CodeTaskEngine, task *domain.Task, workerID string, deadline time.Time) CodeOutcome {
	answer, how := w.RunInCodeUntil(ctx, eng, task, deadline)
	// The deadline is for the work; saving the answer gets its own context so a slow routine still records it.
	return FinishInCode(context.WithoutCancel(ctx), eng, task, workerID, answer, how)
}

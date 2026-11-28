package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
)

// CodeRoutinesWorkerID is the lease owner of a task the scheduler tick is running in code. It does not start with
// "host:", so the tick's runnable count and the host claim treat it like any other short lease.
const CodeRoutinesWorkerID = "tick:code"

// CodeQueue is the task store the tick op leases from.
type CodeQueue interface {
	LeaseNextCodeTask(ctx context.Context, workerID string, leaseDuration time.Duration, exclude []uuid.UUID) (*domain.Task, error)
	ReleaseLeasedTask(ctx context.Context, taskID uuid.UUID, workerID string) (bool, error)
}

// CodeRoutines runs, on each scheduler tick, the due tasks that plain code can finish, so routines and reminders
// happen on time without waking EC2 or waiting for a laptop. Whatever code does not answer goes back untouched.
type CodeRoutines struct {
	Queue    CodeQueue
	Engine   CodeTaskEngine
	Trackers *TrackerLists
	// Budget is the total time for one tick; zero means 18 seconds, which leaves the rest of the Lambda's 29 for
	// the other operations and the response.
	Budget time.Duration
	// MinLeft stops leasing when less than this remains; zero means 3 seconds.
	MinLeft time.Duration
	// MaxTasks caps tasks finished per tick (zero means 10); MaxLeases caps tasks looked at (zero means 40), so a
	// queue of model-only work cannot hide the code tasks behind it for ever.
	MaxTasks  int
	MaxLeases int
	Now       func() time.Time
	Logger    *slog.Logger
}

// Run is the proactive operation.
func (c *CodeRoutines) Run(ctx context.Context) error {
	if c == nil || c.Queue == nil || c.Engine == nil || c.Trackers == nil {
		return nil
	}
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	budget, minLeft, maxTasks, maxLeases := c.Budget, c.MinLeft, c.MaxTasks, c.MaxLeases
	if budget <= 0 {
		budget = 18 * time.Second
	}
	if minLeft <= 0 {
		minLeft = 3 * time.Second
	}
	if maxTasks <= 0 {
		maxTasks = 10
	}
	if maxLeases <= 0 {
		maxLeases = 40
	}
	log := c.Logger
	if log == nil {
		log = slog.Default()
	}
	deadline := now().Add(budget)
	// The lease outlives the budget (a crashed tick is reaped, not stuck) but not by much.
	lease := budget + 30*time.Second
	var skipped []uuid.UUID
	done := 0
	for leases := 0; leases < maxLeases && done < maxTasks && deadline.Sub(now()) >= minLeft; leases++ {
		if ctx.Err() != nil {
			break
		}
		task, err := c.Queue.LeaseNextCodeTask(ctx, CodeRoutinesWorkerID, lease, skipped)
		if err != nil {
			return err
		}
		if task == nil {
			break
		}
		switch c.Trackers.RunClaimed(ctx, c.Engine, task, CodeRoutinesWorkerID, deadline) {
		case Dropped, Answered:
			done++
			continue
		}
		// Not for code (or its answer could not be saved): give it back as it was, for EC2 or a host.
		skipped = append(skipped, task.ID)
		if _, err := c.Queue.ReleaseLeasedTask(context.WithoutCancel(ctx), task.ID, CodeRoutinesWorkerID); err != nil {
			log.Warn("could not release a task the tick did not run", "task_id", task.ID, "error", err)
		}
	}
	if done > 0 {
		log.Info("code routines ran on tick", "tasks", done)
	}
	return nil
}

package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
)

// JobsWorkerID is the lease owner of an agent job the scheduler tick is running. It does not start with "host:".
const JobsWorkerID = "tick:agentjob"

// JobQueue is the task store the tick op leases agent jobs from.
type JobQueue interface {
	LeaseNextAgentJob(ctx context.Context, workerID string, leaseDuration time.Duration, exclude []uuid.UUID) (*domain.Task, error)
	ReleaseLeasedTask(ctx context.Context, taskID uuid.UUID, workerID string) (bool, error)
}

// JobEngine finishes a job's task. The task engine does it and, through its notifier, records the "task" outbox row
// and pushes it, so the phone hears that the job is done (or failed).
type JobEngine interface {
	CompleteTask(ctx context.Context, taskID uuid.UUID, workerID string, output json.RawMessage) error
	FailTask(ctx context.Context, taskID uuid.UUID, workerID string, reason string, retryable bool) error
}

// JobRunner is the "agent_jobs" proactive operation: on each tick it works on the due AGENT JOB tasks for as long as
// the budget allows, then finishes each task or puts it back for the next tick.
type JobRunner struct {
	Queue  JobQueue
	Engine JobEngine
	Agent  *Agent
	// Budget is the time one tick gives to jobs; zero means 10 seconds.
	Budget time.Duration
	// MaxRounds and MaxAge cap a job's life (zero means 60 steps and 2 hours).
	MaxRounds int
	MaxAge    time.Duration
	// MaxJobs caps jobs looked at per tick; zero means 5.
	MaxJobs int
	Now     func() time.Time
	Logger  *slog.Logger
}

// Run is the proactive operation.
func (j *JobRunner) Run(ctx context.Context) error {
	if j == nil || j.Queue == nil || j.Engine == nil || j.Agent == nil {
		return nil
	}
	now := time.Now
	if j.Now != nil {
		now = j.Now
	}
	budget, maxJobs := j.Budget, j.MaxJobs
	if budget <= 0 {
		budget = 10 * time.Second
	}
	if maxJobs <= 0 {
		maxJobs = 5
	}
	log := j.Logger
	if log == nil {
		log = slog.Default()
	}
	deadline := now().Add(budget)
	lease := budget + 30*time.Second // a crashed tick is reaped, not stuck
	var skipped []uuid.UUID
	for n := 0; n < maxJobs && deadline.Sub(now()) >= minJobRound && ctx.Err() == nil; n++ {
		task, err := j.Queue.LeaseNextAgentJob(ctx, JobsWorkerID, lease, skipped)
		if err != nil {
			return err
		}
		if task == nil {
			break
		}
		skipped = append(skipped, task.ID)
		if j.runOne(ctx, task, deadline, log) {
			continue
		}
		if _, err := j.Queue.ReleaseLeasedTask(context.WithoutCancel(ctx), task.ID, JobsWorkerID); err != nil {
			log.Warn("could not release an agent job", "task_id", task.ID, "error", err)
		}
	}
	return nil
}

// runOne works on one job and reports whether the task is finished (so it must not be released).
func (j *JobRunner) runOne(ctx context.Context, task *domain.Task, deadline time.Time, log *slog.Logger) bool {
	var p domain.AgentJobPayload
	_ = json.Unmarshal(task.InputPayload, &p)
	conv, err := uuid.Parse(p.ConversationID)
	if err != nil || strings.TrimSpace(p.Goal) == "" {
		return j.finish(ctx, task, nil, "the job has no goal or conversation", log)
	}
	res, err := j.Agent.RunJob(ctx, JobInput{UserID: task.UserID, ConversationID: conv, Goal: p.Goal, Deadline: deadline, MaxRounds: j.MaxRounds, MaxAge: j.MaxAge})
	if err != nil {
		log.Warn("agent job: run failed; it will be retried", "task_id", task.ID, "error", err)
		return false
	}
	switch {
	case res.Done:
		j.postToOrigin(ctx, task, p, "Job done: "+res.Reply, res.Actions, log)
		return j.finish(ctx, task, res, "", log)
	case res.Failed != "":
		j.postToOrigin(ctx, task, p, "Job failed: "+res.Failed, res.Actions, log)
		return j.finish(ctx, task, res, res.Failed, log)
	}
	return false
}

// postToOrigin adds the result to the chat that started the job, once: its id is fixed by the task, so a retry after a
// failed completion cannot post twice.
func (j *JobRunner) postToOrigin(ctx context.Context, task *domain.Task, p domain.AgentJobPayload, text string, actions []Action, log *slog.Logger) {
	origin, err := uuid.Parse(p.OriginConversation)
	if err != nil {
		return
	}
	ctx = context.WithoutCancel(ctx)
	if c, err := j.Agent.Store.GetConversation(ctx, task.UserID, origin); err != nil || c == nil {
		return
	}
	id := uuid.NewSHA1(uuid.NameSpaceOID, []byte("agent-job-result:"+task.ID.String()))
	if err := j.Agent.Store.AppendTurn(ctx, &Turn{ID: id, ConversationID: origin, UserID: task.UserID, Role: "assistant", Text: text, Actions: actions}); err != nil {
		log.Warn("agent job: could not post the result to the chat", "task_id", task.ID, "error", err)
	}
}

// finish completes or fails the task and reports whether that worked; when it did not, the task is released and the
// next tick finishes it (the stored result is still there).
func (j *JobRunner) finish(ctx context.Context, task *domain.Task, res *JobResult, failure string, log *slog.Logger) bool {
	ctx = context.WithoutCancel(ctx)
	var err error
	if failure != "" {
		err = j.Engine.FailTask(ctx, task.ID, JobsWorkerID, failure, false)
	} else {
		out, _ := json.Marshal(map[string]string{"response": res.Reply})
		err = j.Engine.CompleteTask(ctx, task.ID, JobsWorkerID, out)
	}
	if err != nil {
		log.Warn("agent job: could not finish the task", "task_id", task.ID, "error", err)
	}
	return err == nil
}

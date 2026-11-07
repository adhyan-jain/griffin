package hostagent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/worker"
)

// Config configures an Agent.
type Config struct {
	Client   *Client
	Executor worker.TaskExecutor
	// Capabilities this host reports it can do. The owner decides which of them are actually granted.
	Capabilities []string
	Version      string

	// PresenceInterval is how often the host announces itself; it must be well under the API's live
	// window (45 s). Default 15 s.
	PresenceInterval time.Duration
	// PollInterval is the wait between claims when idle. Default 2 s.
	PollInterval time.Duration
	// LeaseRenewInterval is how often a running task's lease is renewed. Default 15 s.
	LeaseRenewInterval time.Duration
	// MaxConcurrent bounds tasks run at once. Default 1.
	MaxConcurrent int
	Logger        *slog.Logger
}

// Agent keeps a host present and runs the work it is given.
type Agent struct {
	cfg      Config
	logger   *slog.Logger
	paused   atomic.Bool
	inFlight atomic.Int32
	wg       sync.WaitGroup
}

// New builds an Agent, applying defaults.
func New(cfg Config) (*Agent, error) {
	if cfg.Client == nil || cfg.Executor == nil {
		return nil, errors.New("hostagent: client and executor are required")
	}
	if cfg.PresenceInterval <= 0 {
		cfg.PresenceInterval = 15 * time.Second
	}
	if cfg.PresenceInterval >= domain.HostLiveWindow {
		return nil, fmt.Errorf("hostagent: presence interval %s must be under the %s live window", cfg.PresenceInterval, domain.HostLiveWindow)
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 2 * time.Second
	}
	if cfg.LeaseRenewInterval <= 0 {
		cfg.LeaseRenewInterval = 15 * time.Second
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 1
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Agent{cfg: cfg, logger: cfg.Logger}, nil
}

// InFlight is the number of tasks currently running.
func (a *Agent) InFlight() int { return int(a.inFlight.Load()) }

// Paused reports whether the owner has paused this host.
func (a *Agent) Paused() bool { return a.paused.Load() }

// Run keeps the host present and works until ctx is cancelled or the token is refused. On cancellation
// it stops claiming and waits for running tasks to finish reporting.
func (a *Agent) Run(ctx context.Context) error {
	errc := make(chan error, 1)
	go func() { errc <- a.presenceLoop(ctx) }()

	// Announce before the first claim so the API knows this host is live (and what it may do).
	t := time.NewTicker(a.cfg.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			a.wg.Wait()
			return nil
		case err := <-errc:
			a.wg.Wait()
			return err
		case <-t.C:
			if a.paused.Load() || int(a.inFlight.Load()) >= a.cfg.MaxConcurrent {
				continue
			}
			claimed, err := a.cfg.Client.Claim(ctx)
			if err != nil {
				if errors.Is(err, ErrUnauthorized) {
					a.wg.Wait()
					return err
				}
				if ctx.Err() == nil {
					a.logger.Warn("claim failed; will retry", "error", err)
				}
				continue
			}
			if claimed == nil {
				continue
			}
			a.inFlight.Add(1)
			a.wg.Add(1)
			go func() {
				defer a.wg.Done()
				defer a.inFlight.Add(-1)
				a.runTask(ctx, claimed)
			}()
		}
	}
}

func (a *Agent) presenceLoop(ctx context.Context) error {
	beat := func() error {
		rep, err := a.cfg.Client.Heartbeat(ctx, a.cfg.Capabilities, a.cfg.Version)
		if err != nil {
			if errors.Is(err, ErrUnauthorized) {
				return err
			}
			if ctx.Err() == nil {
				a.logger.Warn("presence heartbeat failed; will retry", "error", err)
			}
			return nil
		}
		if was := a.paused.Swap(rep.Paused); was != rep.Paused {
			a.logger.Info("pause state changed by the owner", "paused", rep.Paused)
		}
		return nil
	}
	if err := beat(); err != nil {
		return err
	}
	t := time.NewTicker(a.cfg.PresenceInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			if err := beat(); err != nil {
				return err
			}
		}
	}
}

func (a *Agent) runTask(ctx context.Context, c *Claimed) {
	task := c.Task
	id := task.ID.String()
	a.logger.Info("host claimed task", "task_id", id, "title", task.Title)

	// Report with a context that outlives shutdown so a finished result is not lost.
	report, cancelReport := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancelReport()

	taskCtx, cancelTask := context.WithCancel(ctx)
	defer cancelTask()
	done := make(chan struct{})
	defer close(done)
	go func() { // keep the lease alive; losing it means someone else may be given the task
		t := time.NewTicker(a.cfg.LeaseRenewInterval)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				if err := a.cfg.Client.RenewLease(taskCtx, id); err != nil {
					a.logger.Error("lease renewal failed; abandoning the task", "task_id", id, "error", err)
					cancelTask()
					return
				}
			}
		}
	}()

	execTask := worker.WithDependencyOutputs(task, c.Dependencies)
	res, execErr := a.cfg.Executor.Execute(taskCtx, execTask)
	if taskCtx.Err() != nil {
		// Either the lease was lost (the API already treats the task as abandoned and reporting would be
		// refused) or the host is shutting down. In both cases report nothing: the lease expires and the
		// task is requeued, rather than a half-run step being marked failed for good.
		return
	}

	out := worker.InterpretResult(task, a.cfg.Executor, res, execErr)
	switch {
	case out.NeedsApproval != nil:
		// Approvals are not wired through the Host API yet. Fail loudly and do not retry rather than
		// leave the step stuck or let it act without the owner's approval.
		if err := a.cfg.Client.Fail(report, id, "this host cannot handle approval-gated steps yet", false); err != nil {
			a.logger.Error("could not report failure", "task_id", id, "error", err)
		}
	case out.Failed():
		a.logger.Warn("task failed", "task_id", id, "reason", out.FailReason)
		if err := a.cfg.Client.Fail(report, id, out.FailReason, out.Retryable); err != nil {
			a.logger.Error("could not report failure", "task_id", id, "error", err)
		}
	default:
		if err := a.cfg.Client.Complete(report, id, out.Output); err != nil {
			a.logger.Error("could not report completion", "task_id", id, "error", err)
			return
		}
		a.logger.Info("task completed", "task_id", id)
	}
}

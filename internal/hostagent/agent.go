package hostagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/hands/desktop"
	"github.com/griffin/core/internal/policy"
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

	// draining is a local pause (for example a low laptop battery): nothing new is claimed and running
	// tasks are asked to stop and requeue. It is separate from paused, which the owner sets in the app
	// and every heartbeat overwrites.
	draining    atomic.Bool
	drainReason atomic.Value // string
	runMu       sync.Mutex
	running     map[string]*runningTask

	polMu   sync.RWMutex
	pol     policy.Policy
	polVer  int
	polSync bool
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

// Policy returns the owner's permission policy as last received. ok is false until one has been, and
// the gateway treats that as "deny everything outward". It implements gateway.Source.
func (a *Agent) Policy() (policy.Policy, int, bool) {
	a.polMu.RLock()
	defer a.polMu.RUnlock()
	return a.pol, a.polVer, a.polSync
}

// syncPolicy refetches the policy when the owner has changed it. A policy that does not validate is
// ignored and the last good one stays in force (or nothing, which denies): the host never loosens on a
// malformed answer.
func (a *Agent) syncPolicy(ctx context.Context, serverVersion int) {
	a.polMu.RLock()
	have, synced := a.polVer, a.polSync
	a.polMu.RUnlock()
	if synced && have == serverVersion {
		return
	}
	v, err := a.cfg.Client.Policy(ctx)
	if err != nil {
		if ctx.Err() == nil {
			a.logger.Warn("could not fetch the permission policy; keeping the last one", "error", err)
		}
		return
	}
	p := v.Policy
	if err := p.Validate(time.Now().UTC()); err != nil {
		a.logger.Error("the server sent a permission policy that does not validate; ignoring it", "error", err)
		return
	}
	a.polMu.Lock()
	a.pol, a.polVer, a.polSync = p, v.Version, true
	a.polMu.Unlock()
	a.logger.Info("permission policy updated", "version", v.Version, "rules", len(p.Rules), "default", p.Default)
}

// InFlight is the number of tasks currently running.
func (a *Agent) InFlight() int { return int(a.inFlight.Load()) }

type runningTask struct {
	title  string
	cancel context.CancelFunc
}

// RunningTask names a task this host is working on right now.
type RunningTask struct {
	ID    string
	Title string
}

// RunningTasks lists what the host is doing, for a handover note.
func (a *Agent) RunningTasks() []RunningTask {
	a.runMu.Lock()
	defer a.runMu.Unlock()
	out := make([]RunningTask, 0, len(a.running))
	for id, t := range a.running {
		out = append(out, RunningTask{ID: id, Title: t.title})
	}
	return out
}

// Drain stops claiming work and asks every running task to stop. Each stopped task is reported as a
// retryable failure carrying reason, so the cloud requeues it at once instead of waiting for the lease to
// run out. It returns how many tasks were asked to stop. Resume undoes it.
func (a *Agent) Drain(reason string) int {
	a.drainReason.Store(reason)
	a.draining.Store(true)
	a.runMu.Lock()
	defer a.runMu.Unlock()
	for _, t := range a.running {
		t.cancel()
	}
	return len(a.running)
}

// Resume lets the host claim work again after a Drain.
func (a *Agent) Resume() { a.draining.Store(false) }

// Draining reports whether a local pause is in effect.
func (a *Agent) Draining() bool { return a.draining.Load() }

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
			if a.paused.Load() || a.draining.Load() || int(a.inFlight.Load()) >= a.cfg.MaxConcurrent {
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
		a.syncPolicy(ctx, rep.PolicyVersion)
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

	taskCtx, cancelTask := context.WithCancel(ctx)
	defer cancelTask()
	a.runMu.Lock()
	if a.running == nil {
		a.running = map[string]*runningTask{}
	}
	a.running[id] = &runningTask{title: task.Title, cancel: cancelTask}
	a.runMu.Unlock()
	defer func() {
		a.runMu.Lock()
		delete(a.running, id)
		a.runMu.Unlock()
	}()
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
					// A network blip must not kill a task that may take minutes of model work. Only the server saying
					// the lease is gone (or the owner revoking this host) means someone else may own the task now;
					// anything else is retried on the next tick.
					if leaseLost(err) {
						a.logger.Error("lease is gone; abandoning the task", "task_id", id, "error", err)
						cancelTask()
						return
					}
					a.logger.Warn("lease renewal failed; will retry", "task_id", id, "error", err)
				}
			}
		}
	}()

	execTask := worker.WithDependencyOutputs(task, c.Dependencies)
	execCtx := worker.WithApprovedActions(worker.WithDependencyContext(taskCtx, c.Dependencies), c.ApprovedActions)
	res, execErr := a.cfg.Executor.Execute(execCtx, execTask)

	// Report with a context that outlives shutdown so a finished result is not lost.
	// Created after execution completes so long-running tasks do not exhaust the timeout.
	report, cancelReport := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
	defer cancelReport()
	if taskCtx.Err() != nil && ctx.Err() == nil && a.draining.Load() {
		// A local pause asked this task to stop. Hand it back now, as retryable, so it does not sit until
		// the lease runs out. The reason says why, for the owner and for whoever takes it next.
		reason, _ := a.drainReason.Load().(string)
		if reason == "" {
			reason = "this device paused the task"
		}
		if err := a.cfg.Client.Fail(report, id, "paused: "+reason, true); err != nil {
			a.logger.Error("could not hand the paused task back", "task_id", id, "error", err)
		}
		return
	}
	if taskCtx.Err() != nil {
		// Either the lease was lost (the API already treats the task as abandoned and reporting would be
		// refused) or the host is shutting down. In both cases report nothing: the lease expires and the
		// task is requeued, rather than a half-run step being marked failed for good.
		return
	}

	out := worker.InterpretResult(task, a.cfg.Executor, res, execErr)
	switch {
	case out.NeedsApproval != nil:
		// Ask the owner. The step pauses in the cloud; when they answer it is requeued and claimed again,
		// and the claim then lists this action as approved. If asking fails the step is failed, not run:
		// an action nobody approved must never go ahead.
		summary := out.NeedsApproval.Summary
		if summary == "" {
			summary = "Action requires human approval"
		}
		_ = desktop.Notify(context.Background(), "Griffin: Approval Required", fmt.Sprintf("%s\nTask: %s", summary, task.Title), "critical")
		if err := a.cfg.Client.RequestApproval(report, id, out.NeedsApproval); err != nil {
			a.logger.Error("could not request approval; failing the step instead of acting", "task_id", id, "error", err)
			if ferr := a.cfg.Client.Fail(report, id, "could not ask for your approval: "+err.Error(), false); ferr != nil {
				a.logger.Error("could not report failure", "task_id", id, "error", ferr)
			}
			return
		}
		a.logger.Info("waiting for the owner's approval", "task_id", id, "action", out.NeedsApproval.Summary)
	case out.Failed():
		a.logger.Warn("task failed", "task_id", id, "reason", out.FailReason)
		if err := a.cfg.Client.Fail(report, id, out.FailReason, out.Retryable); err != nil {
			a.logger.Error("could not report failure", "task_id", id, "error", err)
		}
	case !resumeReady(task, out.Output):
		// The answer names a resume PDF that is not on disk. Reporting it as done would hand the owner a draft
		// whose main file is missing, so the step is retried instead.
		a.logger.Warn("the resume PDF the answer names does not exist; retrying", "task_id", id)
		if err := a.cfg.Client.Fail(report, id, "the tailored resume PDF was not created", true); err != nil {
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

// leaseLost reports whether a failed lease renewal means the server no longer considers this host the task's
// owner: the task is gone, was taken back, or this host's access was refused. Network errors and server
// hiccups are not that.
func leaseLost(err error) bool {
	if errors.Is(err, ErrUnauthorized) {
		return true
	}
	var ae *APIError
	if errors.As(err, &ae) {
		switch ae.Status {
		case http.StatusNotFound, http.StatusConflict, http.StatusGone, http.StatusForbidden:
			return true
		}
	}
	return false
}

// resumeReady is true unless the task is an application whose answer names a resume PDF that is missing, empty
// or not a PDF. Only files in the resumes folder count.
func resumeReady(task *domain.Task, output json.RawMessage) bool {
	if !domain.IsApplyTask(task.InputPayload) {
		return true
	}
	path := domain.ResumePDFPath(domain.OutputText(output))
	if path == "" || filepath.Dir(path) != domain.RepoResumeDir {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 5)
	n, _ := f.Read(head)
	st, serr := f.Stat()
	return serr == nil && st.Size() > 1000 && n == 5 && string(head) == "%PDF-"
}

package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// WakeupConfig defines operational parameters for the wake-up controller.
type WakeupConfig struct {
	CooldownWindow     time.Duration // Time window to debounce duplicate wake-up calls
	StoppingRetryDelay time.Duration // Interval between retries when instance is stopping
	MaxStoppingRetries int           // Maximum retries while awaiting stopping -> stopped transition
}

// DefaultWakeupConfig returns safe production defaults.
func DefaultWakeupConfig() WakeupConfig {
	return WakeupConfig{
		CooldownWindow:     30 * time.Second,
		StoppingRetryDelay: 2 * time.Second,
		MaxStoppingRetries: 5,
	}
}

// WakeupController accepts task wake-up requests and idempotently ensures the runner is active.
type WakeupController struct {
	mgr         InstanceManager
	cfg         WakeupConfig
	logger      *slog.Logger
	mu          sync.Mutex
	lastTrigger time.Time
}

// NewWakeupController creates a WakeupController.
func NewWakeupController(mgr InstanceManager, cfg WakeupConfig, logger *slog.Logger) *WakeupController {
	if cfg.CooldownWindow <= 0 {
		cfg.CooldownWindow = 30 * time.Second
	}
	if cfg.StoppingRetryDelay <= 0 {
		cfg.StoppingRetryDelay = 2 * time.Second
	}
	if cfg.MaxStoppingRetries <= 0 {
		cfg.MaxStoppingRetries = 5
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &WakeupController{
		mgr:    mgr,
		cfg:    cfg,
		logger: logger,
	}
}

// WakeOutcome says what a wake attempt did when it did not fail.
type WakeOutcome string

const (
	// WakeSignaled means a start signal was dispatched to the runner.
	WakeSignaled WakeOutcome = "signaled"
	// WakeSkipped means nothing was needed: runner already active/booting, or cooldown debounce.
	WakeSkipped WakeOutcome = "skipped"
)

// TriggerWakeup verifies runner status and idempotently signals startup.
func (c *WakeupController) TriggerWakeup(ctx context.Context) error {
	_, err := c.Wake(ctx)
	return err
}

// Wake is TriggerWakeup that also reports whether a start was signaled or skipped.
func (c *WakeupController) Wake(ctx context.Context) (WakeOutcome, error) {
	out, err := c.wake(ctx)
	if err != nil && errors.Is(err, ErrInsufficientCapacity) {
		c.logger.Error("runner wake failed: AWS InsufficientInstanceCapacity; no capacity for the worker instance, will retry on the next wake", "error", err)
	}
	return out, err
}

func (c *WakeupController) wake(ctx context.Context) (WakeOutcome, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 1. Debounce within cooldown window
	if !c.lastTrigger.IsZero() && time.Since(c.lastTrigger) < c.cfg.CooldownWindow {
		c.logger.Debug("Wakeup request debounced by cooldown window",
			"elapsed", time.Since(c.lastTrigger),
			"cooldown", c.cfg.CooldownWindow,
		)
		return WakeSkipped, nil
	}

	// 2. Query runner state
	state, err := c.mgr.GetRunnerState(ctx)
	if err != nil {
		c.logger.Warn("Failed to inspect runner state; proceeding with start attempt", "error", err)
	}

	c.logger.Info("Wakeup controller evaluating runner", "current_state", state)

	switch state {
	case RunnerStateRunning, RunnerStatePending:
		// Already active or starting
		c.lastTrigger = time.Now()
		c.logger.Info("Runner is already active or booting; no start action required", "state", state)
		return WakeSkipped, nil

	case RunnerStateStopping:
		// A stopping instance cannot be started immediately by AWS EC2.
		// Await transition to stopped with backoff.
		c.logger.Warn("Runner is currently stopping; awaiting transition to stopped state")
		for i := 0; i < c.cfg.MaxStoppingRetries; i++ {
			select {
			case <-ctx.Done():
				return WakeSkipped, ctx.Err()
			case <-time.After(c.cfg.StoppingRetryDelay):
			}

			s, err := c.mgr.GetRunnerState(ctx)
			if err == nil && s == RunnerStateStopped {
				state = RunnerStateStopped
				break
			}
		}

		if state != RunnerStateStopped {
			return WakeSkipped, fmt.Errorf("%w: timeout waiting for runner to stop before restarting", ErrRunnerStopping)
		}
		fallthrough

	case RunnerStateStopped, RunnerStateUnknown:
		c.logger.Info("Signaling runner startup")
		if err := c.mgr.StartRunner(ctx); err != nil {
			return WakeSkipped, fmt.Errorf("trigger runner start: %w", classifyStartError(err))
		}
		c.lastTrigger = time.Now()
		c.logger.Info("Runner startup signal dispatched successfully")
		return WakeSignaled, nil

	default:
		c.logger.Info("Unknown state, attempting start", "state", state)
		if err := c.mgr.StartRunner(ctx); err != nil {
			return WakeSkipped, fmt.Errorf("trigger runner start: %w", classifyStartError(err))
		}
		c.lastTrigger = time.Now()
		return WakeSignaled, nil
	}
}

// ResetCooldown forces the next TriggerWakeup call to evaluate regardless of elapsed time.
func (c *WakeupController) ResetCooldown() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastTrigger = time.Time{}
}

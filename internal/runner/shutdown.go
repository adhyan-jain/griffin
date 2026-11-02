package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// MonitorState represents the idle shutdown controller lifecycle phase.
type MonitorState string

const (
	MonitorStateActive   MonitorState = "ACTIVE"
	MonitorStateIdle     MonitorState = "IDLE"
	MonitorStateDraining MonitorState = "DRAINING"
	MonitorStateStopping MonitorState = "STOPPING"
	MonitorStateStopped  MonitorState = "STOPPED"
)

// TaskCounter queries runnable and in-flight work.
type TaskCounter interface {
	CountRunnableTasks(ctx context.Context) (int64, error)
}

// WorkerDrainer controls worker task intake and tracks active executions.
type WorkerDrainer interface {
	Pause()
	Resume()
	ActiveCount() int
}

// Stopper issues the instance stop command to the operating system or AWS API.
type Stopper interface {
	StopInstance(ctx context.Context) error
}

// MockStopper records shutdown calls for hermetic testing.
type MockStopper struct {
	mu        sync.Mutex
	StopCalls int
	Stopped   bool
}

// StopInstance records the shutdown invocation.
func (s *MockStopper) StopInstance(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.StopCalls++
	s.Stopped = true
	return nil
}

// ShutdownBinary is the absolute path granted in infra/sudoers.d/griffin-shutdown.
const ShutdownBinary = "/usr/sbin/shutdown"

// ShutdownCommand is the exact argv used to power the host off. `sudo -n` fails immediately
// instead of prompting for a password, and the absolute path must match the sudoers entry.
func ShutdownCommand() []string {
	return []string{"sudo", "-n", ShutdownBinary, "-h", "now"}
}

// ErrShutdownFailed wraps a failed host shutdown command.
var ErrShutdownFailed = errors.New("host shutdown command failed")

// CommandRunner executes a command and returns combined output (injectable for tests).
type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

func execRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// SystemStopper powers off the Linux host (the EC2 instance then ends up `stopped`, because the
// instance is launched with --instance-initiated-shutdown-behavior stop).
type SystemStopper struct {
	logger *slog.Logger
	run    CommandRunner
}

// NewSystemStopper creates a SystemStopper that really runs the shutdown command.
func NewSystemStopper(logger *slog.Logger) *SystemStopper {
	return NewSystemStopperWithRunner(logger, execRunner)
}

// NewSystemStopperWithRunner creates a SystemStopper with an injected command runner.
func NewSystemStopperWithRunner(logger *slog.Logger, run CommandRunner) *SystemStopper {
	if logger == nil {
		logger = slog.Default()
	}
	if run == nil {
		run = execRunner
	}
	return &SystemStopper{logger: logger, run: run}
}

// StopInstance runs `sudo -n /usr/sbin/shutdown -h now`.
func (s *SystemStopper) StopInstance(ctx context.Context) error {
	argv := ShutdownCommand()
	s.logger.Info("Executing host system poweroff", "cmd", argv)
	out, err := s.run(ctx, argv[0], argv[1:]...)
	if err != nil {
		return fmt.Errorf("%w: %v: %s", ErrShutdownFailed, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// MockWorkerDrainer is a thread-safe drainer mock.
type MockWorkerDrainer struct {
	mu          sync.Mutex
	Paused      bool
	activeCount int
	PauseCalls  int
	ResumeCalls int
}

func (d *MockWorkerDrainer) Pause() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.Paused = true
	d.PauseCalls++
}

func (d *MockWorkerDrainer) Resume() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.Paused = false
	d.ResumeCalls++
}

func (d *MockWorkerDrainer) ActiveCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.activeCount
}

func (d *MockWorkerDrainer) SetActiveCount(count int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.activeCount = count
}

// IdleShutdownConfig defines timeout and interval settings for the idle monitor.
type IdleShutdownConfig struct {
	IdleTimeout   time.Duration // Time with zero runnable work before entering draining
	CheckInterval time.Duration // Frequency of queue polling
	DrainTimeout  time.Duration // Maximum window to wait for in-flight tasks to complete
}

// DefaultIdleShutdownConfig returns recommended production defaults.
func DefaultIdleShutdownConfig() IdleShutdownConfig {
	return IdleShutdownConfig{
		IdleTimeout:   300 * time.Second, // 5 minutes idle
		CheckInterval: 5 * time.Second,
		DrainTimeout:  30 * time.Second,
	}
}

// IdleShutdownMonitor supervises the runner host, drains workers, and safely stops the VM.
type IdleShutdownMonitor struct {
	taskCounter TaskCounter
	drainer     WorkerDrainer
	stopper     Stopper
	cfg         IdleShutdownConfig
	logger      *slog.Logger

	mu          sync.Mutex
	state       MonitorState
	idleStarted time.Time
}

// NewIdleShutdownMonitor creates an IdleShutdownMonitor.
func NewIdleShutdownMonitor(
	counter TaskCounter,
	drainer WorkerDrainer,
	stopper Stopper,
	cfg IdleShutdownConfig,
	logger *slog.Logger,
) *IdleShutdownMonitor {
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 300 * time.Second
	}
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = 5 * time.Second
	}
	if cfg.DrainTimeout <= 0 {
		cfg.DrainTimeout = 30 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &IdleShutdownMonitor{
		taskCounter: counter,
		drainer:     drainer,
		stopper:     stopper,
		cfg:         cfg,
		logger:      logger,
		state:       MonitorStateActive,
	}
}

// State returns current monitor state.
func (m *IdleShutdownMonitor) State() MonitorState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

// CheckOnce executes a single polling iteration. Returns true if the instance was stopped.
func (m *IdleShutdownMonitor) CheckOnce(ctx context.Context) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 1. Query runnable tasks from the database
	count, err := m.taskCounter.CountRunnableTasks(ctx)
	if err != nil {
		m.logger.Error("Failed to query runnable tasks count", "error", err)
		return false, fmt.Errorf("query runnable tasks: %w", err)
	}

	// 2. Runnable tasks exist -> maintain or restore ACTIVE state
	if count > 0 {
		if m.state == MonitorStateDraining {
			m.logger.Warn("Runnable tasks detected during draining; aborting shutdown and resuming workers",
				"count", count,
			)
			m.drainer.Resume()
		} else if m.state == MonitorStateIdle {
			m.logger.Info("Runnable tasks arrived during idle countdown; resetting to ACTIVE",
				"count", count,
			)
		}
		m.state = MonitorStateActive
		return false, nil
	}

	// 3. Zero runnable tasks detected
	switch m.state {
	case MonitorStateActive:
		m.state = MonitorStateIdle
		m.idleStarted = time.Now()
		m.logger.Info("Queue is empty; entering IDLE countdown",
			"idle_timeout", m.cfg.IdleTimeout,
		)
		return false, nil

	case MonitorStateIdle:
		elapsed := time.Since(m.idleStarted)
		if elapsed < m.cfg.IdleTimeout {
			m.logger.Debug("Idle countdown in progress",
				"elapsed", elapsed,
				"timeout", m.cfg.IdleTimeout,
			)
			return false, nil
		}

		// Idle duration exceeded -> enter DRAINING state
		m.state = MonitorStateDraining
		m.logger.Info("Idle timeout reached; pausing worker claims and entering DRAINING state",
			"elapsed", elapsed,
		)
		m.drainer.Pause()
		fallthrough

	case MonitorStateDraining:
		// Check for in-flight tasks
		active := m.drainer.ActiveCount()
		if active > 0 {
			m.logger.Info("Awaiting in-flight tasks completion before stopping",
				"active_count", active,
			)
			return false, nil
		}

		// In-flight count is 0. Perform CRITICAL pre-shutdown atomic recheck.
		m.logger.Info("In-flight tasks drained; executing pre-shutdown database verification check")
		recheckCount, err := m.taskCounter.CountRunnableTasks(ctx)
		if err != nil {
			m.logger.Error("Pre-shutdown recheck failed; aborting shutdown for safety", "error", err)
			m.drainer.Resume()
			m.state = MonitorStateActive
			return false, fmt.Errorf("pre-shutdown recheck: %w", err)
		}

		if recheckCount > 0 {
			m.logger.Warn("Simultaneous task arrival detected during pre-shutdown window; aborting shutdown and resuming workers",
				"count", recheckCount,
			)
			m.drainer.Resume()
			m.state = MonitorStateActive
			return false, nil
		}

		// Recheck confirmed 0 tasks. Enforce shutdown.
		m.state = MonitorStateStopping
		m.logger.Info("Pre-shutdown check confirmed 0 tasks; initiating instance shutdown")

		if err := m.stopper.StopInstance(ctx); err != nil {
			m.logger.Error("Stopper execution failed; resuming workers", "error", err)
			m.drainer.Resume()
			m.state = MonitorStateActive
			return false, fmt.Errorf("stop instance: %w", err)
		}

		m.state = MonitorStateStopped
		m.logger.Info("Instance shutdown command executed successfully")
		return true, nil

	default:
		return false, nil
	}
}

// Run executes the monitoring loop until context cancellation or successful shutdown.
func (m *IdleShutdownMonitor) Run(ctx context.Context) error {
	m.logger.Info("Starting IdleShutdownMonitor loop",
		"idle_timeout", m.cfg.IdleTimeout,
		"check_interval", m.cfg.CheckInterval,
	)

	ticker := time.NewTicker(m.cfg.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("IdleShutdownMonitor context cancelled; exiting loop")
			return ctx.Err()

		case <-ticker.C:
			stopped, err := m.CheckOnce(ctx)
			if err != nil {
				m.logger.Error("Error during idle shutdown check", "error", err)
			}
			if stopped {
				return nil
			}
		}
	}
}

package runner

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockTaskCounter struct {
	mu          sync.Mutex
	count       int64
	err         error
	recheckHook func() // optional hook to mutate count during recheck
}

func (m *mockTaskCounter) CountRunnableTasks(ctx context.Context) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.recheckHook != nil {
		m.recheckHook()
	}
	return m.count, m.err
}

func (m *mockTaskCounter) setCount(c int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.count = c
}

func TestIdleShutdown_CountdownAndStop(t *testing.T) {
	counter := &mockTaskCounter{count: 0}
	drainer := &MockWorkerDrainer{}
	stopper := &MockStopper{}

	cfg := IdleShutdownConfig{
		IdleTimeout:   50 * time.Millisecond,
		CheckInterval: 10 * time.Millisecond,
		DrainTimeout:  50 * time.Millisecond,
	}

	monitor := NewIdleShutdownMonitor(counter, drainer, stopper, cfg, nil)

	// Iteration 1: ACTIVE -> IDLE
	stopped, err := monitor.CheckOnce(context.Background())
	require.NoError(t, err)
	assert.False(t, stopped)
	assert.Equal(t, MonitorStateIdle, monitor.State())
	assert.False(t, drainer.Paused)

	// Wait for idle timeout to elapse
	time.Sleep(60 * time.Millisecond)

	// Iteration 2: IDLE -> DRAINING -> STOPPED (since ActiveCount=0 and recheck=0)
	stopped, err = monitor.CheckOnce(context.Background())
	require.NoError(t, err)
	assert.True(t, stopped)
	assert.Equal(t, MonitorStateStopped, monitor.State())
	assert.True(t, drainer.Paused)
	assert.Equal(t, 1, stopper.StopCalls)
}

func TestIdleShutdown_ResetOnActiveTasks(t *testing.T) {
	counter := &mockTaskCounter{count: 0}
	drainer := &MockWorkerDrainer{}
	stopper := &MockStopper{}

	cfg := IdleShutdownConfig{
		IdleTimeout:   100 * time.Millisecond,
		CheckInterval: 10 * time.Millisecond,
	}

	monitor := NewIdleShutdownMonitor(counter, drainer, stopper, cfg, nil)

	// Iteration 1: ACTIVE -> IDLE
	_, err := monitor.CheckOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, MonitorStateIdle, monitor.State())

	// Task arrives before timeout expires
	counter.setCount(2)

	// Iteration 2: IDLE -> ACTIVE
	_, err = monitor.CheckOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, MonitorStateActive, monitor.State())
	assert.False(t, stopper.Stopped)
	assert.Equal(t, 0, stopper.StopCalls)
}

func TestIdleShutdown_DrainingWaitsForInFlightTasks(t *testing.T) {
	counter := &mockTaskCounter{count: 0}
	drainer := &MockWorkerDrainer{activeCount: 1} // 1 in-flight task
	stopper := &MockStopper{}

	cfg := IdleShutdownConfig{
		IdleTimeout: 10 * time.Millisecond,
	}

	monitor := NewIdleShutdownMonitor(counter, drainer, stopper, cfg, nil)

	// Enter IDLE
	_, _ = monitor.CheckOnce(context.Background())
	time.Sleep(20 * time.Millisecond)

	// Enter DRAINING: activeCount is 1, so must NOT stop yet
	stopped, err := monitor.CheckOnce(context.Background())
	require.NoError(t, err)
	assert.False(t, stopped)
	assert.Equal(t, MonitorStateDraining, monitor.State())
	assert.True(t, drainer.Paused)
	assert.Equal(t, 0, stopper.StopCalls)

	// In-flight task finishes
	drainer.SetActiveCount(0)

	// Next check: in-flight 0, recheck 0 -> STOPPED
	stopped, err = monitor.CheckOnce(context.Background())
	require.NoError(t, err)
	assert.True(t, stopped)
	assert.Equal(t, MonitorStateStopped, monitor.State())
	assert.Equal(t, 1, stopper.StopCalls)
}

func TestIdleShutdown_SimultaneousTaskArrivalDuringDrainingAbortsShutdown(t *testing.T) {
	counter := &mockTaskCounter{count: 0}
	drainer := &MockWorkerDrainer{}
	stopper := &MockStopper{}

	cfg := IdleShutdownConfig{
		IdleTimeout: 10 * time.Millisecond,
	}

	monitor := NewIdleShutdownMonitor(counter, drainer, stopper, cfg, nil)

	// Enter IDLE
	_, _ = monitor.CheckOnce(context.Background())
	time.Sleep(20 * time.Millisecond)

	// Configure hook: when CountRunnableTasks is called during the pre-shutdown recheck,
	// simulate a new task arriving simultaneously!
	recheckTriggered := false
	counter.recheckHook = func() {
		if recheckTriggered {
			counter.count = 1 // New task arrived!
		}
		recheckTriggered = true
	}

	stopped, err := monitor.CheckOnce(context.Background())
	require.NoError(t, err)
	assert.False(t, stopped, "Must not stop instance when task arrives during pre-shutdown window")
	assert.Equal(t, MonitorStateActive, monitor.State(), "State must revert to ACTIVE")
	assert.False(t, drainer.Paused, "Workers must be resumed")
	assert.Equal(t, 0, stopper.StopCalls, "StopInstance must NOT be called")
}

func TestIdleShutdown_ErrorOnRecheckAbortsShutdownForSafety(t *testing.T) {
	counter := &mockTaskCounter{count: 0}
	drainer := &MockWorkerDrainer{}
	stopper := &MockStopper{}

	cfg := IdleShutdownConfig{
		IdleTimeout: 10 * time.Millisecond,
	}

	monitor := NewIdleShutdownMonitor(counter, drainer, stopper, cfg, nil)

	// Enter IDLE
	_, _ = monitor.CheckOnce(context.Background())
	time.Sleep(20 * time.Millisecond)

	// Fail specifically on the second (pre-shutdown recheck) call
	callNum := 0
	counter.recheckHook = func() {
		callNum++
		if callNum == 2 {
			counter.err = errors.New("database connection glitch")
		}
	}

	stopped, err := monitor.CheckOnce(context.Background())
	assert.Error(t, err)
	assert.False(t, stopped)
	assert.Equal(t, MonitorStateActive, monitor.State())
	assert.False(t, drainer.Paused, "Workers must be resumed if check fails")
	assert.Equal(t, 0, stopper.StopCalls)
}

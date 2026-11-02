package runner

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWakeupController_StartStoppedInstance(t *testing.T) {
	mockMgr := NewMockInstanceManager(RunnerStateStopped)
	ctrl := NewWakeupController(mockMgr, WakeupConfig{
		CooldownWindow: 10 * time.Second,
	}, nil)

	err := ctrl.TriggerWakeup(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 1, mockMgr.StartCalls)
	state, _ := mockMgr.GetRunnerState(context.Background())
	assert.Equal(t, RunnerStateRunning, state)
}

func TestWakeupController_NoOpWhenAlreadyRunning(t *testing.T) {
	mockMgr := NewMockInstanceManager(RunnerStateRunning)
	ctrl := NewWakeupController(mockMgr, WakeupConfig{
		CooldownWindow: 10 * time.Second,
	}, nil)

	err := ctrl.TriggerWakeup(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 0, mockMgr.StartCalls, "Should not call StartRunner if already running")
}

func TestWakeupController_DebounceCooldown(t *testing.T) {
	mockMgr := NewMockInstanceManager(RunnerStateStopped)
	ctrl := NewWakeupController(mockMgr, WakeupConfig{
		CooldownWindow: 1 * time.Second,
	}, nil)

	// First trigger starts runner
	err := ctrl.TriggerWakeup(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, mockMgr.StartCalls)

	// Set state back to stopped in mock
	mockMgr.SetState(RunnerStateStopped)

	// Immediate second trigger within cooldown is debounced
	err = ctrl.TriggerWakeup(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, mockMgr.StartCalls, "Should be debounced by cooldown window")

	// Reset cooldown manually or wait
	ctrl.ResetCooldown()
	err = ctrl.TriggerWakeup(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, mockMgr.StartCalls, "Should start after cooldown cleared")
}

func TestWakeupController_StoppingTransitionRetry(t *testing.T) {
	mockMgr := NewMockInstanceManager(RunnerStateStopping)
	ctrl := NewWakeupController(mockMgr, WakeupConfig{
		CooldownWindow:     10 * time.Second,
		StoppingRetryDelay: 20 * time.Millisecond,
		MaxStoppingRetries: 5,
	}, nil)

	// Asynchronously transition from stopping to stopped after 50ms
	go func() {
		time.Sleep(50 * time.Millisecond)
		mockMgr.SetState(RunnerStateStopped)
	}()

	err := ctrl.TriggerWakeup(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 1, mockMgr.StartCalls)
	state, _ := mockMgr.GetRunnerState(context.Background())
	assert.Equal(t, RunnerStateRunning, state)
}

func TestWakeupController_StoppingTimeout(t *testing.T) {
	mockMgr := NewMockInstanceManager(RunnerStateStopping)
	ctrl := NewWakeupController(mockMgr, WakeupConfig{
		CooldownWindow:     10 * time.Second,
		StoppingRetryDelay: 10 * time.Millisecond,
		MaxStoppingRetries: 2,
	}, nil)

	err := ctrl.TriggerWakeup(context.Background())
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrRunnerStopping)
	assert.Equal(t, 0, mockMgr.StartCalls)
}

func TestWakeupController_ConcurrentTriggers(t *testing.T) {
	mockMgr := NewMockInstanceManager(RunnerStateStopped)
	ctrl := NewWakeupController(mockMgr, WakeupConfig{
		CooldownWindow: 10 * time.Second,
	}, nil)

	var wg sync.WaitGroup
	workers := 10
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			_ = ctrl.TriggerWakeup(context.Background())
		}()
	}

	wg.Wait()

	// Exactly 1 start call should have succeeded; remainder debounced
	assert.Equal(t, 1, mockMgr.StartCalls)
}

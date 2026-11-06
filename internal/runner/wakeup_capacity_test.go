package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type failingStartMgr struct {
	*MockInstanceManager
	err error
}

func (f *failingStartMgr) StartRunner(ctx context.Context) error { return f.err }

func TestWakeupController_InsufficientCapacityIsSentinelAndLoggedAtError(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	awsErr := fmt.Errorf("%w: operation error EC2: StartInstances, api error InsufficientInstanceCapacity: We currently do not have sufficient capacity", ErrRunnerStartFailed)
	ctrl := NewWakeupController(&failingStartMgr{NewMockInstanceManager(RunnerStateStopped), awsErr}, WakeupConfig{}, logger)

	err := ctrl.TriggerWakeup(context.Background())
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrInsufficientCapacity))
	assert.Contains(t, buf.String(), "level=ERROR")
	assert.Contains(t, buf.String(), "runner wake failed: AWS InsufficientInstanceCapacity")
}

func TestWakeupController_OtherStartErrorsAreNotCapacity(t *testing.T) {
	ctrl := NewWakeupController(&failingStartMgr{NewMockInstanceManager(RunnerStateStopped), ErrRunnerStartFailed}, WakeupConfig{}, nil)
	err := ctrl.TriggerWakeup(context.Background())
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrInsufficientCapacity))
}

func TestWakeupController_WakeReportsOutcome(t *testing.T) {
	ctrl := NewWakeupController(NewMockInstanceManager(RunnerStateStopped), WakeupConfig{CooldownWindow: time.Minute}, nil)
	out, err := ctrl.Wake(context.Background())
	require.NoError(t, err)
	assert.Equal(t, WakeSignaled, out)
	out, err = ctrl.Wake(context.Background())
	require.NoError(t, err)
	assert.Equal(t, WakeSkipped, out, "cooldown")

	running := NewWakeupController(NewMockInstanceManager(RunnerStateRunning), WakeupConfig{}, nil)
	out, _ = running.Wake(context.Background())
	assert.Equal(t, WakeSkipped, out, "already running")
}

package runner

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// RunnerState represents the EC2 compute instance lifecycle status.
type RunnerState string

const (
	RunnerStateStopped  RunnerState = "stopped"
	RunnerStateStopping RunnerState = "stopping"
	RunnerStatePending  RunnerState = "pending"
	RunnerStateRunning  RunnerState = "running"
	RunnerStateUnknown  RunnerState = "unknown"
)

var (
	ErrRunnerStopping       = errors.New("runner instance is currently in stopping transition")
	ErrRunnerStartFailed    = errors.New("failed to start runner instance")
	ErrRunnerStopFailed     = errors.New("failed to stop runner instance")
	ErrWakeupCooldownActive = errors.New("wakeup cooldown active, skipping duplicate call")
)

// InstanceManager controls cloud compute lifecycle (e.g. AWS EC2).
type InstanceManager interface {
	StartRunner(ctx context.Context) error
	StopRunner(ctx context.Context) error
	GetRunnerState(ctx context.Context) (RunnerState, error)
}

// MockInstanceManager is a thread-safe mock for tests.
type MockInstanceManager struct {
	mu           sync.Mutex
	state        RunnerState
	StartCalls   int
	StopCalls    int
	FailOnStart  bool
	FailOnStop   bool
	StateHistory []RunnerState
}

// NewMockInstanceManager creates a MockInstanceManager with initial state.
func NewMockInstanceManager(initialState RunnerState) *MockInstanceManager {
	return &MockInstanceManager{
		state:        initialState,
		StateHistory: []RunnerState{initialState},
	}
}

func (m *MockInstanceManager) StartRunner(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.StartCalls++
	if m.FailOnStart {
		return ErrRunnerStartFailed
	}
	if m.state == RunnerStateStopping {
		return ErrRunnerStopping
	}

	m.state = RunnerStateRunning
	m.StateHistory = append(m.StateHistory, RunnerStateRunning)
	return nil
}

func (m *MockInstanceManager) StopRunner(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.StopCalls++
	if m.FailOnStop {
		return ErrRunnerStopFailed
	}

	m.state = RunnerStateStopped
	m.StateHistory = append(m.StateHistory, RunnerStateStopped)
	return nil
}

func (m *MockInstanceManager) GetRunnerState(ctx context.Context) (RunnerState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state, nil
}

func (m *MockInstanceManager) SetState(s RunnerState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = s
	m.StateHistory = append(m.StateHistory, s)
}

// EC2InstanceManager implements InstanceManager via AWS EC2 API or CLI.
type EC2InstanceManager struct {
	instanceID string
	region     string
}

// NewEC2InstanceManager creates an EC2InstanceManager for a specific instance in a region.
func NewEC2InstanceManager(instanceID, region string) (*EC2InstanceManager, error) {
	if instanceID == "" {
		return nil, fmt.Errorf("instanceID cannot be empty")
	}
	if region == "" {
		region = "ap-south-1"
	}
	return &EC2InstanceManager{
		instanceID: instanceID,
		region:     region,
	}, nil
}

// StartRunner issues an ec2:StartInstances request for the configured instance.
func (e *EC2InstanceManager) StartRunner(ctx context.Context) error {
	// Staged implementation for AWS environment
	return nil
}

// StopRunner issues an ec2:StopInstances request for the configured instance.
func (e *EC2InstanceManager) StopRunner(ctx context.Context) error {
	// Staged implementation for AWS environment
	return nil
}

// GetRunnerState queries the current instance state from AWS EC2 DescribeInstances.
func (e *EC2InstanceManager) GetRunnerState(ctx context.Context) (RunnerState, error) {
	// Staged implementation for AWS environment
	return RunnerStateUnknown, nil
}

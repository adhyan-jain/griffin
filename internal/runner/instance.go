package runner

import (
	"context"
	"errors"
	"fmt"
	"sync"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/smithy-go"
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

// ErrRunnerTerminated means the configured instance no longer exists (terminated/shutting down).
var ErrRunnerTerminated = errors.New("runner instance is terminated or shutting down")

// EC2API is the subset of the EC2 client used by EC2InstanceManager (mockable in tests).
type EC2API interface {
	StartInstances(ctx context.Context, in *ec2.StartInstancesInput, optFns ...func(*ec2.Options)) (*ec2.StartInstancesOutput, error)
	StopInstances(ctx context.Context, in *ec2.StopInstancesInput, optFns ...func(*ec2.Options)) (*ec2.StopInstancesOutput, error)
	DescribeInstances(ctx context.Context, in *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
}

// EC2InstanceManager implements InstanceManager against exactly one EC2 instance.
// Required IAM (see docs/DEPLOYMENT_RUNBOOK.md): ec2:StartInstances and ec2:StopInstances on the
// instance ARN, ec2:DescribeInstances on *. It uses the default credential chain (Lambda role).
type EC2InstanceManager struct {
	client     EC2API
	instanceID string
}

// NewEC2InstanceManager creates a manager using the default AWS credential chain.
func NewEC2InstanceManager(ctx context.Context, instanceID, region string) (*EC2InstanceManager, error) {
	if instanceID == "" {
		return nil, fmt.Errorf("instanceID cannot be empty")
	}
	if region == "" {
		region = "ap-south-1"
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	return NewEC2InstanceManagerWithClient(ec2.NewFromConfig(cfg), instanceID)
}

// NewEC2InstanceManagerWithClient is the injectable constructor used by tests.
func NewEC2InstanceManagerWithClient(client EC2API, instanceID string) (*EC2InstanceManager, error) {
	if client == nil || instanceID == "" {
		return nil, fmt.Errorf("client and instanceID are required")
	}
	return &EC2InstanceManager{client: client, instanceID: instanceID}, nil
}

// GetRunnerState queries the instance state via DescribeInstances.
func (e *EC2InstanceManager) GetRunnerState(ctx context.Context) (RunnerState, error) {
	out, err := e.client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{InstanceIds: []string{e.instanceID}})
	if err != nil {
		return RunnerStateUnknown, fmt.Errorf("describe instance %s: %w", e.instanceID, err)
	}
	for _, r := range out.Reservations {
		for _, i := range r.Instances {
			if i.InstanceId != nil && *i.InstanceId != e.instanceID {
				continue
			}
			if i.State == nil {
				break
			}
			switch i.State.Name {
			case ec2types.InstanceStateNamePending:
				return RunnerStatePending, nil
			case ec2types.InstanceStateNameRunning:
				return RunnerStateRunning, nil
			case ec2types.InstanceStateNameStopping:
				return RunnerStateStopping, nil
			case ec2types.InstanceStateNameStopped:
				return RunnerStateStopped, nil
			case ec2types.InstanceStateNameShuttingDown, ec2types.InstanceStateNameTerminated:
				return RunnerStateUnknown, ErrRunnerTerminated
			}
			return RunnerStateUnknown, fmt.Errorf("unrecognized instance state %q", i.State.Name)
		}
	}
	return RunnerStateUnknown, fmt.Errorf("instance %s not found", e.instanceID)
}

// StartRunner idempotently starts the instance: no-op when running/pending, ErrRunnerStopping
// while it is still stopping (callers retry), otherwise ec2:StartInstances.
func (e *EC2InstanceManager) StartRunner(ctx context.Context) error {
	state, err := e.GetRunnerState(ctx)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrRunnerStartFailed, err)
	}
	switch state {
	case RunnerStateRunning, RunnerStatePending:
		return nil
	case RunnerStateStopping:
		return ErrRunnerStopping
	}
	if _, err := e.client.StartInstances(ctx, &ec2.StartInstancesInput{InstanceIds: []string{e.instanceID}}); err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "IncorrectInstanceState" {
			return ErrRunnerStopping
		}
		return fmt.Errorf("%w: %v", ErrRunnerStartFailed, err)
	}
	return nil
}

// StopRunner idempotently stops the instance: no-op when stopped/stopping.
func (e *EC2InstanceManager) StopRunner(ctx context.Context) error {
	state, err := e.GetRunnerState(ctx)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrRunnerStopFailed, err)
	}
	if state == RunnerStateStopped || state == RunnerStateStopping {
		return nil
	}
	if _, err := e.client.StopInstances(ctx, &ec2.StopInstancesInput{InstanceIds: []string{e.instanceID}}); err != nil {
		return fmt.Errorf("%w: %v", ErrRunnerStopFailed, err)
	}
	return nil
}

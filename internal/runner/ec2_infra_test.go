package runner_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/smithy-go"

	"github.com/griffin/core/internal/runner"
)

const instanceID = "i-0123456789abcdef0"

type fakeEC2 struct {
	state      ec2types.InstanceStateName
	describeEr error
	startErr   error
	stopErr    error
	noInstance bool
	starts     int
	stops      int
	started    []string
	stopped    []string
}

func (f *fakeEC2) DescribeInstances(_ context.Context, in *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	if f.describeEr != nil {
		return nil, f.describeEr
	}
	if f.noInstance {
		return &ec2.DescribeInstancesOutput{}, nil
	}
	id := in.InstanceIds[0]
	return &ec2.DescribeInstancesOutput{Reservations: []ec2types.Reservation{{Instances: []ec2types.Instance{
		{InstanceId: &id, State: &ec2types.InstanceState{Name: f.state}},
	}}}}, nil
}

func (f *fakeEC2) StartInstances(_ context.Context, in *ec2.StartInstancesInput, _ ...func(*ec2.Options)) (*ec2.StartInstancesOutput, error) {
	f.starts++
	f.started = in.InstanceIds
	if f.startErr != nil {
		return nil, f.startErr
	}
	f.state = ec2types.InstanceStateNamePending
	return &ec2.StartInstancesOutput{}, nil
}

func (f *fakeEC2) StopInstances(_ context.Context, in *ec2.StopInstancesInput, _ ...func(*ec2.Options)) (*ec2.StopInstancesOutput, error) {
	f.stops++
	f.stopped = in.InstanceIds
	if f.stopErr != nil {
		return nil, f.stopErr
	}
	f.state = ec2types.InstanceStateNameStopping
	return &ec2.StopInstancesOutput{}, nil
}

func newMgr(t *testing.T, f *fakeEC2) *runner.EC2InstanceManager {
	t.Helper()
	m, err := runner.NewEC2InstanceManagerWithClient(f, instanceID)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestEC2StateMapping(t *testing.T) {
	cases := map[ec2types.InstanceStateName]runner.RunnerState{
		ec2types.InstanceStateNamePending:  runner.RunnerStatePending,
		ec2types.InstanceStateNameRunning:  runner.RunnerStateRunning,
		ec2types.InstanceStateNameStopping: runner.RunnerStateStopping,
		ec2types.InstanceStateNameStopped:  runner.RunnerStateStopped,
	}
	for in, want := range cases {
		got, err := newMgr(t, &fakeEC2{state: in}).GetRunnerState(context.Background())
		if err != nil || got != want {
			t.Errorf("%s: got %s err %v want %s", in, got, err, want)
		}
	}
	for _, dead := range []ec2types.InstanceStateName{ec2types.InstanceStateNameTerminated, ec2types.InstanceStateNameShuttingDown} {
		if _, err := newMgr(t, &fakeEC2{state: dead}).GetRunnerState(context.Background()); !errors.Is(err, runner.ErrRunnerTerminated) {
			t.Errorf("%s: want ErrRunnerTerminated, got %v", dead, err)
		}
	}
	if _, err := newMgr(t, &fakeEC2{noInstance: true}).GetRunnerState(context.Background()); err == nil {
		t.Error("missing instance must be an error")
	}
	if _, err := newMgr(t, &fakeEC2{describeEr: errors.New("boom")}).GetRunnerState(context.Background()); err == nil {
		t.Error("describe failure must surface")
	}
}

func TestEC2StartRunner(t *testing.T) {
	ctx := context.Background()

	f := &fakeEC2{state: ec2types.InstanceStateNameStopped}
	if err := newMgr(t, f).StartRunner(ctx); err != nil {
		t.Fatal(err)
	}
	if f.starts != 1 || len(f.started) != 1 || f.started[0] != instanceID {
		t.Fatalf("StartInstances calls=%d ids=%v", f.starts, f.started)
	}

	for _, s := range []ec2types.InstanceStateName{ec2types.InstanceStateNameRunning, ec2types.InstanceStateNamePending} {
		f := &fakeEC2{state: s}
		if err := newMgr(t, f).StartRunner(ctx); err != nil || f.starts != 0 {
			t.Errorf("%s: must be an idempotent no-op (err=%v starts=%d)", s, err, f.starts)
		}
	}

	f = &fakeEC2{state: ec2types.InstanceStateNameStopping}
	if err := newMgr(t, f).StartRunner(ctx); !errors.Is(err, runner.ErrRunnerStopping) || f.starts != 0 {
		t.Errorf("stopping: want ErrRunnerStopping without a start call, got %v starts=%d", err, f.starts)
	}

	// race: instance flipped to stopping between describe and start
	f = &fakeEC2{state: ec2types.InstanceStateNameStopped, startErr: &smithy.GenericAPIError{Code: "IncorrectInstanceState", Message: "x"}}
	if err := newMgr(t, f).StartRunner(ctx); !errors.Is(err, runner.ErrRunnerStopping) {
		t.Errorf("IncorrectInstanceState: want ErrRunnerStopping, got %v", err)
	}

	f = &fakeEC2{state: ec2types.InstanceStateNameStopped, startErr: &smithy.GenericAPIError{Code: "UnauthorizedOperation", Message: "denied"}}
	if err := newMgr(t, f).StartRunner(ctx); !errors.Is(err, runner.ErrRunnerStartFailed) {
		t.Errorf("api failure: want ErrRunnerStartFailed, got %v", err)
	}

	if err := newMgr(t, &fakeEC2{state: ec2types.InstanceStateNameTerminated}).StartRunner(ctx); !errors.Is(err, runner.ErrRunnerStartFailed) {
		t.Errorf("terminated: want ErrRunnerStartFailed, got %v", err)
	}
}

func TestEC2StopRunner(t *testing.T) {
	ctx := context.Background()

	f := &fakeEC2{state: ec2types.InstanceStateNameRunning}
	if err := newMgr(t, f).StopRunner(ctx); err != nil || f.stops != 1 || f.stopped[0] != instanceID {
		t.Fatalf("stop running: err=%v stops=%d", err, f.stops)
	}
	for _, s := range []ec2types.InstanceStateName{ec2types.InstanceStateNameStopped, ec2types.InstanceStateNameStopping} {
		f := &fakeEC2{state: s}
		if err := newMgr(t, f).StopRunner(ctx); err != nil || f.stops != 0 {
			t.Errorf("%s: must be a no-op (err=%v stops=%d)", s, err, f.stops)
		}
	}
	f = &fakeEC2{state: ec2types.InstanceStateNameRunning, stopErr: errors.New("denied")}
	if err := newMgr(t, f).StopRunner(ctx); !errors.Is(err, runner.ErrRunnerStopFailed) {
		t.Errorf("want ErrRunnerStopFailed, got %v", err)
	}
}

func TestEC2ManagerValidation(t *testing.T) {
	if _, err := runner.NewEC2InstanceManagerWithClient(nil, instanceID); err == nil {
		t.Error("nil client must be rejected")
	}
	if _, err := runner.NewEC2InstanceManagerWithClient(&fakeEC2{}, ""); err == nil {
		t.Error("empty instance id must be rejected")
	}
}

// The wake-up controller drives the real manager end to end (still against a fake EC2 API).
func TestWakeupControllerStartsStoppedInstance(t *testing.T) {
	f := &fakeEC2{state: ec2types.InstanceStateNameStopped}
	c := runner.NewWakeupController(newMgr(t, f), runner.WakeupConfig{}, nil)
	if err := c.TriggerWakeup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.starts != 1 {
		t.Fatalf("want 1 StartInstances call, got %d", f.starts)
	}
	c.ResetCooldown()
	if err := c.TriggerWakeup(context.Background()); err != nil || f.starts != 1 {
		t.Fatalf("second wake must be idempotent (pending): err=%v starts=%d", err, f.starts)
	}
}

// ---------- shutdown ----------

func TestSystemStopperRunsExactSudoCommand(t *testing.T) {
	var gotName string
	var gotArgs []string
	s := runner.NewSystemStopperWithRunner(nil, func(_ context.Context, name string, args ...string) ([]byte, error) {
		gotName, gotArgs = name, args
		return nil, nil
	})
	if err := s.StopInstance(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := append([]string{gotName}, gotArgs...)
	want := []string{"sudo", "-n", "/usr/sbin/shutdown", "-h", "now"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("argv = %v, want %v", got, want)
	}
}

func TestSystemStopperSurfacesFailureOutput(t *testing.T) {
	s := runner.NewSystemStopperWithRunner(nil, func(context.Context, string, ...string) ([]byte, error) {
		return []byte("sudo: a password is required\n"), errors.New("exit status 1")
	})
	err := s.StopInstance(context.Background())
	if !errors.Is(err, runner.ErrShutdownFailed) || !strings.Contains(err.Error(), "a password is required") {
		t.Fatalf("want ErrShutdownFailed with sudo output, got %v", err)
	}
}

// ---------- deployment files stay coherent with the code ----------

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSudoersMatchesShutdownCommand(t *testing.T) {
	sudoers := read(t, "../../infra/sudoers.d/griffin-shutdown")
	var rules []string
	for _, l := range strings.Split(sudoers, "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			rules = append(rules, l)
		}
	}
	argv := runner.ShutdownCommand() // sudo -n <bin> -h now
	want := "griffin ALL=(root) NOPASSWD: " + strings.Join(argv[2:], " ")
	if len(rules) != 1 || rules[0] != want {
		t.Fatalf("sudoers rules = %q, want exactly %q", rules, want)
	}
}

func TestSystemdUnit(t *testing.T) {
	u := read(t, "../../infra/systemd/griffin-worker.service")
	for _, must := range []string{
		"User=griffin", "WorkingDirectory=/opt/griffin", "EnvironmentFile=/etc/griffin/worker.env",
		"ExecStartPre=+/usr/local/bin/griffin-fetch-env.sh", "ExecStart=/opt/griffin/griffin-server",
		"DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1001/bus", "Requires=user@1001.service", "Restart=on-failure",
	} {
		if !strings.Contains(u, must) {
			t.Errorf("unit is missing %q", must)
		}
	}
	if strings.Contains(u, "NoNewPrivileges=true") {
		t.Error("NoNewPrivileges=true would break `sudo shutdown`")
	}
	for _, secretish := range []string{"DATABASE_URL=", "SUPABASE_JWT_SECRET=", "WORKER_API_KEY=", "GEMINI_API_KEY"} {
		if strings.Contains(u, secretish) {
			t.Errorf("unit must not embed %s", secretish)
		}
	}
}

func TestShellScriptsParse(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	for _, f := range []string{"infra/install-worker.sh", "infra/helpers/griffin-fetch-env.sh", "infra/user-data.sh",
		"infra/provision-ec2.sh", "scripts/package-lambda.sh", "scripts/package-worker.sh"} {
		if out, err := exec.Command(bash, "-n", "../../"+f).CombinedOutput(); err != nil {
			t.Errorf("%s: %v\n%s", f, err, out)
		}
	}
}

func TestFetchEnvScriptFailsClosedAndNeverEchoesSecrets(t *testing.T) {
	s := read(t, "../../infra/helpers/griffin-fetch-env.sh")
	for _, must := range []string{"DATABASE_URL", "SUPABASE_JWT_SECRET", "WORKER_API_KEY", "mv -f", "chmod 0640", "--with-decryption"} {
		if !strings.Contains(s, must) {
			t.Errorf("fetch-env script missing %q", must)
		}
	}
	if strings.Contains(s, "set -x") || strings.Contains(s, "echo \"$") {
		t.Error("fetch-env script must not trace or echo values")
	}
}

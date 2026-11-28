package hands

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/hands/shell"
)

type fakeShellRunner struct{ ran []shell.Cmd }

func (f *fakeShellRunner) Run(_ context.Context, c shell.Cmd) (shell.Result, error) {
	f.ran = append(f.ran, c)
	return shell.Result{Stdout: "hi\n"}, nil
}
func (f *fakeShellRunner) Start(shell.Cmd) error           { return nil }
func (f *fakeShellRunner) LookPath(string) (string, error) { return "", os.ErrNotExist }

func shellTask(t *testing.T, payload map[string]any) *domain.Task {
	t.Helper()
	b, err := json.Marshal(payload)
	require.NoError(t, err)
	return &domain.Task{ID: uuid.New(), InputPayload: b}
}

func TestExecutorRunsShellStepAndReturnsText(t *testing.T) {
	r := &fakeShellRunner{}
	e := &Executor{Shell: &shell.Service{Runner: r, AllowShell: true}}
	task := &domain.Task{ID: uuid.New(), InputPayload: domain.ShellPayload(domain.ShellAction{Op: domain.ShellOpRun, Command: "echo hi", TimeoutS: 5})}
	res, err := e.Execute(context.Background(), task)
	require.NoError(t, err)
	assert.Equal(t, "SUCCESS", res.Status)
	assert.Contains(t, res.TextOutput, "hi")
	assert.Contains(t, domain.OutputText(res.Output), "Exit code 0")
	require.Len(t, r.ran, 1)
}

func TestExecutorShellRefusedWithoutOptInOrDeclaredCapability(t *testing.T) {
	run := domain.ShellAction{Op: domain.ShellOpRun, Command: "id", TimeoutS: 5}
	// No shell service on this host.
	res, _ := (&Executor{}).Execute(context.Background(), &domain.Task{InputPayload: domain.ShellPayload(run)})
	assert.Equal(t, "FAILED", res.Status)
	// The step does not declare the shell capability (only desktop), so it must not run.
	r := &fakeShellRunner{}
	e := &Executor{Shell: &shell.Service{Runner: r, AllowShell: true}}
	res, _ = e.Execute(context.Background(), shellTask(t, map[string]any{"requires": []string{"desktop"}, "shell": run}))
	assert.Equal(t, "FAILED", res.Status)
	// Shell switched off on the service: refused even when declared.
	e = &Executor{Shell: &shell.Service{Runner: r, AllowShell: false}}
	res, _ = e.Execute(context.Background(), &domain.Task{InputPayload: domain.ShellPayload(run)})
	assert.Equal(t, "FAILED", res.Status)
	assert.Empty(t, r.ran)
}

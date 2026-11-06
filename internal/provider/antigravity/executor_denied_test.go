package antigravity

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/worker"
)

func runMockAgy(t *testing.T, body string) (*worker.ExecutionResult, error) {
	t.Helper()
	script := createMockScript(t, body)
	e, err := NewAntigravityExecutor(ExecutorConfig{
		BinaryPath:       script,
		WorkspaceRoot:    t.TempDir(),
		ExecutionTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	res, err := e.Execute(context.Background(), &domain.Task{ID: uuid.New(), Goal: "Collect technical facts"})
	return res, err
}

func TestExecutor_DeniedActionsAndEmptyResponse(t *testing.T) {
	res, err := runMockAgy(t, `
echo '{"conversation_id":"c","status":"SUCCESS","response":"","usage":{},"denied_actions":[{"action":"command","display_name":"RunCommand"}]}'
echo 'jetski: no output produced' >&2
exit 0
`)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrToolPermissionDenied)
	assert.Contains(t, err.Error(), "RunCommand")
	assert.Contains(t, err.Error(), "headless")
	require.NotNil(t, res)
	assert.Equal(t, "FAILED", res.Status)
	assert.True(t, res.Retryable)
	assert.ErrorIs(t, res.Error, ErrToolPermissionDenied)
}

func TestExecutor_DeniedActionsWithResponseSucceeds(t *testing.T) {
	res, err := runMockAgy(t, `
echo '{"conversation_id":"c","status":"SUCCESS","response":"here is text","usage":{},"denied_actions":[{"action":"command","display_name":"RunCommand"}]}'
exit 0
`)
	require.NoError(t, err)
	assert.Equal(t, "SUCCESS", res.Status)
	assert.Equal(t, "here is text", res.TextOutput)
}

func TestExecutor_PlainTextSucceeds(t *testing.T) {
	res, err := runMockAgy(t, `
echo '{"conversation_id":"c","status":"SUCCESS","response":"Hi there.","usage":{}}'
exit 0
`)
	require.NoError(t, err)
	assert.Equal(t, "SUCCESS", res.Status)
	assert.JSONEq(t, `{"response":"Hi there."}`, string(res.Output))
}

func TestExecutor_EmptyWithoutDenialsIsErrEmptyResult(t *testing.T) {
	res, err := runMockAgy(t, `
echo '{"conversation_id":"c","status":"SUCCESS","response":"","usage":{}}'
exit 0
`)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrEmptyResult)
	assert.Equal(t, "FAILED", res.Status)
	assert.True(t, res.Retryable)
}

func TestExecutor_MalformedJSON(t *testing.T) {
	res, err := runMockAgy(t, `
echo 'not json at all'
exit 0
`)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidOutput)
	assert.Equal(t, "FAILED", res.Status)
}

func TestExecutor_NonZeroExit(t *testing.T) {
	res, err := runMockAgy(t, `
echo 'boom' >&2
exit 3
`)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrExecutionFailed)
	assert.Equal(t, "FAILED", res.Status)
}

func TestExecutor_EmptyStdout(t *testing.T) {
	res, err := runMockAgy(t, `exit 0`)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidOutput)
	assert.Equal(t, "FAILED", res.Status)
}

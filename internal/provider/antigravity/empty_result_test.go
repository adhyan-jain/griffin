package antigravity

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
)

// A zero-exit agy run with no usable result must never be reported as SUCCESS.
func TestAntigravityExecutor_EmptyResultIsFailure(t *testing.T) {
	for name, body := range map[string]string{
		"empty response":      `{"status":"SUCCESS","response":""}`,
		"whitespace response": `{"status":"SUCCESS","response":"  \n "}`,
		"missing response":    `{"status":"SUCCESS"}`,
	} {
		t.Run(name, func(t *testing.T) {
			script := createMockScript(t, "echo '"+body+"'\nexit 0\n")
			exec, err := NewAntigravityExecutor(ExecutorConfig{
				BinaryPath:       script,
				WorkspaceRoot:    t.TempDir(),
				ExecutionTimeout: 5 * time.Second,
			})
			require.NoError(t, err)

			res, err := exec.Execute(context.Background(), &domain.Task{ID: uuid.New(), Title: "t", Goal: "g"})
			require.ErrorIs(t, err, ErrEmptyResult)
			require.NotNil(t, res)
			assert.Equal(t, "FAILED", res.Status)
			assert.True(t, res.Retryable)
			assert.ErrorIs(t, res.Error, ErrEmptyResult)
			assert.Empty(t, res.Output)
		})
	}
}

package antigravity

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
)

func TestExecutor_LogsVersionExitCodeAndDeniedActionsWithoutPrompt(t *testing.T) {
	script := createMockScript(t, `
if [ "$1" = "--version" ]; then echo "agy 1.5.1"; exit 0; fi
echo '{"conversation_id":"c","status":"SUCCESS","response":"","usage":{},"denied_actions":[{"action":"command","display_name":"RunCommand"}]}'
exit 0
`)
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	e, err := NewAntigravityExecutor(ExecutorConfig{BinaryPath: script, WorkspaceRoot: t.TempDir(), ExecutionTimeout: 5 * time.Second, Logger: logger})
	require.NoError(t, err)

	_, err = e.Execute(context.Background(), &domain.Task{ID: uuid.New(), Goal: "SECRET-GOAL-TEXT-123"})
	require.Error(t, err)

	out := buf.String()
	assert.Contains(t, out, "agy 1.5.1")
	assert.Contains(t, out, "RunCommand")
	assert.Contains(t, out, "exit_code=0")
	assert.NotContains(t, out, "SECRET-GOAL-TEXT-123")
	assert.NotContains(t, out, "You are JJ")
}

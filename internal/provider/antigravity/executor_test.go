package antigravity

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
)

// createMockScript writes an executable bash script into a temporary directory.
func createMockScript(t *testing.T, content string) string {
	t.Helper()
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "mock_agy.sh")
	err := os.WriteFile(scriptPath, []byte("#!/bin/bash\n"+content), 0755)
	require.NoError(t, err)
	return scriptPath
}

func TestAntigravityExecutor_Success(t *testing.T) {
	script := createMockScript(t, `
echo '{"conversation_id":"conv-test-1","status":"SUCCESS","response":"Hello world","num_turns":1,"structured_output":{"greeting":"hello"},"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}'
exit 0
`)

	wsRoot := t.TempDir()
	exec, err := NewAntigravityExecutor(ExecutorConfig{
		BinaryPath:       script,
		WorkspaceRoot:    wsRoot,
		ExecutionTimeout: 5 * time.Second,
	})
	require.NoError(t, err)

	assert.Equal(t, "antigravity", exec.Name())

	healthy, hErr := exec.IsHealthy(context.Background())
	assert.NoError(t, hErr)
	assert.True(t, healthy)

	task := &domain.Task{
		ID:           uuid.New(),
		Title:        "Greeting Task",
		Goal:         "Say hello",
		InputPayload: json.RawMessage(`{"prompt":"Say hello"}`),
	}

	res, err := exec.Execute(context.Background(), task)
	require.NoError(t, err)
	require.NotNil(t, res)

	assert.Equal(t, "SUCCESS", res.Status)
	assert.Equal(t, "Hello world", res.TextOutput)
	assert.Equal(t, "conv-test-1", *res.ConversationID)
	assert.Equal(t, 15, res.Usage.TotalTokens)
	assert.Equal(t, 10, res.PromptTokens)
	assert.Equal(t, 5, res.CompletionTokens)

	var structOut map[string]string
	err = json.Unmarshal(res.Output, &structOut)
	require.NoError(t, err)
	assert.Equal(t, "hello", structOut["greeting"])
}

func TestAntigravityExecutor_ArgumentPassing(t *testing.T) {
	// Mock script dumps received arguments to stdout in a valid JSON response
	script := createMockScript(t, `
python3 -c '
import sys, json
args = " ".join(sys.argv[1:])
print(json.dumps({
    "conversation_id": "conv-arg-test",
    "status": "SUCCESS",
    "response": args,
    "usage": {}
}))
' "$@"
exit 0
`)

	wsRoot := t.TempDir()
	exec, err := NewAntigravityExecutor(ExecutorConfig{
		BinaryPath:       script,
		WorkspaceRoot:    wsRoot,
		ExecutionTimeout: 5 * time.Second,
	})
	require.NoError(t, err)

	task := &domain.Task{
		ID:    uuid.New(),
		Title: "Arg Test Task",
		Goal:  "Default Goal",
		InputPayload: json.RawMessage(`{
			"prompt": "Custom prompt text with special chars: \"quoted\" and $VAR",
			"conversation_id": "conv-continuation-uuid",
			"model": "gemini-2.5-pro",
			"json_schema": {"type":"object"}
		}`),
	}

	res, err := exec.Execute(context.Background(), task)
	require.NoError(t, err)
	require.NotNil(t, res)

	// Verify all arguments were passed as separate argv tokens
	assert.Contains(t, res.TextOutput, "-p Custom prompt text with special chars: \"quoted\" and $VAR")
	assert.Contains(t, res.TextOutput, "--output-format json")
	assert.Contains(t, res.TextOutput, "--disable-slash-commands")
	assert.Contains(t, res.TextOutput, "--conversation conv-continuation-uuid")
	assert.Contains(t, res.TextOutput, "--model gemini-2.5-pro")
	assert.Contains(t, res.TextOutput, "--json-schema {\"type\":\"object\"}")
}

func TestAntigravityExecutor_AuthFailure(t *testing.T) {
	script := createMockScript(t, `
echo "error: authentication required. Please run 'agy auth login'" >&2
exit 1
`)

	wsRoot := t.TempDir()
	exec, err := NewAntigravityExecutor(ExecutorConfig{
		BinaryPath:       script,
		WorkspaceRoot:    wsRoot,
		ExecutionTimeout: 5 * time.Second,
	})
	require.NoError(t, err)

	task := &domain.Task{
		ID:   uuid.New(),
		Goal: "Test Auth Error",
	}

	res, err := exec.Execute(context.Background(), task)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrAuthenticationRequired)
	assert.False(t, res.Retryable, "Auth errors must not trigger retries")

	// Executor must be flagged unhealthy
	healthy, hErr := exec.IsHealthy(context.Background())
	assert.False(t, healthy)
	assert.ErrorIs(t, hErr, ErrAuthenticationRequired)
}

func TestAntigravityExecutor_ProcessTimeoutAndKill(t *testing.T) {
	// Script traps SIGTERM and sleeps until hard killed by SIGKILL
	script := createMockScript(t, `
trap '' TERM
sleep 30
exit 0
`)

	wsRoot := t.TempDir()
	exec, err := NewAntigravityExecutor(ExecutorConfig{
		BinaryPath:          script,
		WorkspaceRoot:       wsRoot,
		ExecutionTimeout:    100 * time.Millisecond,
		GracefulTermination: 100 * time.Millisecond,
	})
	require.NoError(t, err)

	task := &domain.Task{
		ID:   uuid.New(),
		Goal: "Timeout Task",
	}

	start := time.Now()
	res, err := exec.Execute(context.Background(), task)
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrProcessTimeout)
	require.NotNil(t, res)
	assert.Equal(t, "TIMED_OUT", res.Status)
	assert.True(t, res.Retryable, "Timeouts are retryable")
	assert.Less(t, elapsed, 2*time.Second, "Subprocess must be terminated quickly")
}

func TestAntigravityExecutor_ProcessCancellation(t *testing.T) {
	script := createMockScript(t, `
sleep 30
exit 0
`)

	wsRoot := t.TempDir()
	exec, err := NewAntigravityExecutor(ExecutorConfig{
		BinaryPath:          script,
		WorkspaceRoot:       wsRoot,
		ExecutionTimeout:    10 * time.Second,
		GracefulTermination: 100 * time.Millisecond,
	})
	require.NoError(t, err)

	task := &domain.Task{
		ID:   uuid.New(),
		Goal: "Cancel Task",
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	res, err := exec.Execute(ctx, task)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrProcessCancelled)
	require.NotNil(t, res)
	assert.Equal(t, "CANCELLED", res.Status)
	assert.False(t, res.Retryable)
}

func TestAntigravityExecutor_BoundedConcurrency(t *testing.T) {
	// Concurrency = 1. Launch 2 executions concurrently.
	// Script records start and end timestamps into a shared log file to verify no overlap.
	logFile := filepath.Join(t.TempDir(), "execution.log")

	script := createMockScript(t, fmt.Sprintf(`
echo "START $(date +%%s%%N)" >> "%s"
sleep 0.15
echo "END $(date +%%s%%N)" >> "%s"
echo '{"conversation_id":"c","status":"SUCCESS","response":"ok","usage":{}}'
exit 0
`, logFile, logFile))

	wsRoot := t.TempDir()
	exec, err := NewAntigravityExecutor(ExecutorConfig{
		BinaryPath:       script,
		WorkspaceRoot:    wsRoot,
		MaxConcurrent:    1,
		ExecutionTimeout: 5 * time.Second,
	})
	require.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(2)

	for i := 0; i < 2; i++ {
		go func(idx int) {
			defer wg.Done()
			task := &domain.Task{
				ID:   uuid.New(),
				Goal: fmt.Sprintf("Concurrent task %d", idx),
			}
			_, _ = exec.Execute(context.Background(), task)
		}(i)
	}

	wg.Wait()

	// Read log file lines
	content, err := os.ReadFile(logFile)
	require.NoError(t, err)

	lines := splitLines(string(content))
	require.Len(t, lines, 4, "Expected 2 starts and 2 ends")

	// Order must be START, END, START, END (strictly serialized)
	assert.Contains(t, lines[0], "START")
	assert.Contains(t, lines[1], "END")
	assert.Contains(t, lines[2], "START")
	assert.Contains(t, lines[3], "END")
}

func TestAntigravityExecutor_BinaryNotFound(t *testing.T) {
	wsRoot := t.TempDir()
	exec, err := NewAntigravityExecutor(ExecutorConfig{
		BinaryPath:    "/nonexistent/binary/path/to/agy",
		WorkspaceRoot: wsRoot,
	})
	require.NoError(t, err)

	healthy, hErr := exec.IsHealthy(context.Background())
	assert.False(t, healthy)
	assert.ErrorIs(t, hErr, ErrBinaryNotFound)

	task := &domain.Task{
		ID:   uuid.New(),
		Goal: "Missing Binary",
	}

	res, err := exec.Execute(context.Background(), task)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrBinaryNotFound)
	require.NotNil(t, res)
	assert.False(t, res.Retryable)
}

func TestAntigravityLiveIntegration(t *testing.T) {
	if os.Getenv("GRIFFIN_ENABLE_LIVE_AGY_TEST") != "true" {
		t.Skip("Skipping live Antigravity CLI integration test (set GRIFFIN_ENABLE_LIVE_AGY_TEST=true to enable)")
	}

	binaryPath, err := exec.LookPath("agy")
	if err != nil {
		t.Skip("Antigravity binary 'agy' not found in PATH")
	}

	execInstance, err := NewAntigravityExecutor(ExecutorConfig{
		BinaryPath:       binaryPath,
		WorkspaceRoot:    t.TempDir(),
		ExecutionTimeout: 60 * time.Second,
	})
	require.NoError(t, err)

	task := &domain.Task{
		ID:    uuid.New(),
		Title: "Live Integration Probe",
		Goal:  "Answer in exactly one word: What is the capital of France?",
		InputPayload: json.RawMessage(`{
			"prompt": "Answer in exactly one word: What is the capital of France?",
			"json_schema": {
				"type": "object",
				"properties": {
					"city": {"type": "string"}
				},
				"required": ["city"]
			}
		}`),
	}

	res, err := execInstance.Execute(context.Background(), task)
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "SUCCESS", res.Status)
	assert.NotEmpty(t, res.Output)
	assert.NotNil(t, res.ConversationID)
}

func splitLines(s string) []string {
	var result []string
	var current []rune
	for _, r := range s {
		if r == '\n' {
			if len(current) > 0 {
				result = append(result, string(current))
				current = nil
			}
		} else {
			current = append(current, r)
		}
	}
	if len(current) > 0 {
		result = append(result, string(current))
	}
	return result
}

package claudecli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/griffin/core/internal/domain"
)

// mockCLI writes a fake `claude` that behaves according to MOCK_MODE.
func mockCLI(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "claude")
	script := `#!/bin/sh
case "$MOCK_MODE" in
  auth)  echo '{"is_error":true,"result":"Not logged in. Please run /login"}'; exit 1 ;;
  rate)  echo '{"is_error":true,"result":"Usage limit reached"}'; exit 1 ;;
  empty) echo '{"is_error":false,"result":"  "}' ;;
  bad)   echo 'not json' ;;
  hang)  sleep 30 ;;
  env)   echo "{\"is_error\":false,\"result\":\"key=[$ANTHROPIC_API_KEY] bedrock=[$CLAUDE_CODE_USE_BEDROCK] keep=[$KEEP_ME]\"}" ;;
  *)     echo '{"is_error":false,"result":"**Done.** Here is the summary.","session_id":"s-1","usage":{"input_tokens":11,"output_tokens":7}}' ;;
esac
`
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func newExec(t *testing.T, timeout time.Duration) *Executor {
	t.Helper()
	e, err := New(Config{BinaryPath: mockCLI(t), WorkspaceRoot: t.TempDir(), Timeout: timeout})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func task() *domain.Task { return &domain.Task{Title: "Draft", Goal: "Summarise the notes"} }

func TestExecute_Success(t *testing.T) {
	t.Setenv("MOCK_MODE", "ok")
	res, err := newExec(t, 5*time.Second).Execute(context.Background(), task())
	if err != nil || res.Status != "SUCCESS" || res.ProviderName != "claude" {
		t.Fatalf("unexpected: %+v %v", res, err)
	}
	var out map[string]string
	if json.Unmarshal(res.Output, &out) != nil || !strings.Contains(out["response"], "Done.") {
		t.Fatalf("output must be {response}: %s", res.Output)
	}
	if res.PromptTokens != 11 || res.CompletionTokens != 7 || res.ConversationID == nil || *res.ConversationID != "s-1" {
		t.Fatalf("usage or session not captured: %+v", res)
	}
}

func TestExecute_NotSignedInMarksUnhealthy(t *testing.T) {
	t.Setenv("MOCK_MODE", "auth")
	e := newExec(t, 5*time.Second)
	_, err := e.Execute(context.Background(), task())
	if !errors.Is(err, domain.ErrAuthenticationRequired) {
		t.Fatalf("want ErrAuthenticationRequired, got %v", err)
	}
	if ok, _ := e.IsHealthy(context.Background()); ok {
		t.Fatal("must be unhealthy after an auth failure")
	}
	e.ResetHealth()
	if ok, _ := e.IsHealthy(context.Background()); !ok {
		t.Fatal("ResetHealth should restore it")
	}
}

func TestExecute_FailureModes(t *testing.T) {
	for mode, want := range map[string]error{"rate": ErrRateLimited, "empty": ErrEmptyResult, "bad": ErrExecution} {
		t.Setenv("MOCK_MODE", mode)
		res, err := newExec(t, 5*time.Second).Execute(context.Background(), task())
		if !errors.Is(err, want) || res.Status != "FAILED" || !res.Retryable {
			t.Errorf("%s: want retryable %v, got %+v %v", mode, want, res, err)
		}
	}
}

func TestExecute_TimeoutKillsTheProcess(t *testing.T) {
	t.Setenv("MOCK_MODE", "hang")
	start := time.Now()
	_, err := newExec(t, 300*time.Millisecond).Execute(context.Background(), task())
	if err == nil || time.Since(start) > 10*time.Second {
		t.Fatalf("a hung CLI must be killed on timeout: err=%v after %s", err, time.Since(start))
	}
}

func TestExecute_NeverPassesApiKeys(t *testing.T) {
	t.Setenv("MOCK_MODE", "env")
	t.Setenv("ANTHROPIC_API_KEY", "sk-should-not-leak")
	t.Setenv("CLAUDE_CODE_USE_BEDROCK", "1")
	t.Setenv("KEEP_ME", "yes")
	res, err := newExec(t, 5*time.Second).Execute(context.Background(), task())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.TextOutput, "sk-should-not-leak") || !strings.Contains(res.TextOutput, "key=[]") || !strings.Contains(res.TextOutput, "bedrock=[]") {
		t.Fatalf("API-key and cloud variables must be stripped: %s", res.TextOutput)
	}
	if !strings.Contains(res.TextOutput, "keep=[yes]") {
		t.Fatalf("unrelated variables must survive: %s", res.TextOutput)
	}
}

func TestNew_MissingBinary(t *testing.T) {
	if _, err := New(Config{BinaryPath: "/definitely/not/here/claude"}); !errors.Is(err, ErrBinaryNotFound) {
		t.Fatalf("want ErrBinaryNotFound, got %v", err)
	}
}

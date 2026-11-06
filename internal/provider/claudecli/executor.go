// Package claudecli runs a task through the Claude Code CLI (`claude -p`), using whatever account is
// signed in on the worker machine. It is optional and disabled by default (CLAUDE_ENABLED).
//
// It never uses an API key: every ANTHROPIC_* and cloud-provider variable is removed from the child
// process, so a stray key in the environment cannot turn this into pay-per-token billing. If the CLI
// is not signed in, the executor reports domain.ErrAuthenticationRequired and marks itself
// unhealthy; the router then runs the step on agy instead.
package claudecli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/worker"
)

var (
	ErrBinaryNotFound = errors.New("claude CLI binary not found")
	ErrEmptyResult    = errors.New("claude exited 0 but returned no result")
	ErrRateLimited    = errors.New("claude rate or usage limit reached")
	ErrExecution      = errors.New("claude execution failed")
)

// Config configures the executor.
type Config struct {
	BinaryPath    string // default "claude"
	MaxConcurrent int    // default 2; Claude steps run beside the serialized agy ones
	Timeout       time.Duration
	WorkspaceRoot string
	Model         string // optional
	Logger        *slog.Logger
}

// Executor implements worker.TaskExecutor.
type Executor struct {
	cfg     Config
	bin     string
	sem     chan struct{}
	healthy atomic.Bool
	logger  *slog.Logger
}

// New resolves the binary and returns an executor, or ErrBinaryNotFound.
func New(cfg Config) (*Executor, error) {
	if cfg.BinaryPath == "" {
		cfg.BinaryPath = "claude"
	}
	bin, err := exec.LookPath(cfg.BinaryPath)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrBinaryNotFound, cfg.BinaryPath)
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 2
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Minute
	}
	if cfg.WorkspaceRoot == "" {
		cfg.WorkspaceRoot = os.TempDir()
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	e := &Executor{cfg: cfg, bin: bin, sem: make(chan struct{}, cfg.MaxConcurrent), logger: cfg.Logger}
	e.healthy.Store(true)
	return e, nil
}

func (e *Executor) Name() string                            { return "claude" }
func (e *Executor) IsHealthy(context.Context) (bool, error) { return e.healthy.Load(), nil }

// ResetHealth is called after the operator signs in again.
func (e *Executor) ResetHealth() { e.healthy.Store(true) }

const preamble = `You are a specialist inside Griffin, an AI workforce. Do the one step you are given.
Answer in clear Markdown: a one-line answer first, then short sections, bullets for lists, tables for comparisons.
You have NO tools: do not run commands, open files or browse. If you lack a fact, say so; never invent sources or URLs.`

type taskPrompt struct {
	Prompt string `json:"prompt"`
}

func buildPrompt(task *domain.Task) string {
	body := task.Goal
	var p taskPrompt
	if len(task.InputPayload) > 0 && json.Unmarshal(task.InputPayload, &p) == nil && p.Prompt != "" {
		body = p.Prompt
	}
	return preamble + "\n\n" + body
}

// childEnv drops every variable that could switch Claude to API-key or cloud billing.
func childEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "ANTHROPIC_") || k == "CLAUDE_CODE_USE_BEDROCK" || k == "CLAUDE_CODE_USE_VERTEX" {
			continue
		}
		out = append(out, kv)
	}
	return out
}

type cliResult struct {
	IsError   bool   `json:"is_error"`
	Subtype   string `json:"subtype"`
	Result    string `json:"result"`
	SessionID string `json:"session_id"`
	Usage     struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func looksUnauthenticated(s string) bool {
	s = strings.ToLower(s)
	for _, m := range []string{"not logged in", "please run /login", "run /login", "oauth token has expired", "invalid api key", "authentication_error", "please log in"} {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

func looksRateLimited(s string) bool {
	s = strings.ToLower(s)
	for _, m := range []string{"rate limit", "usage limit", "quota", "overloaded", "too many requests"} {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// Execute runs one step in a throwaway workspace.
func (e *Executor) Execute(ctx context.Context, task *domain.Task) (*worker.ExecutionResult, error) {
	fail := func(execID uuid.UUID, err error, retryable bool) (*worker.ExecutionResult, error) {
		return &worker.ExecutionResult{ExecutionID: execID, ProviderName: "claude", Status: "FAILED", Error: err, Retryable: retryable}, err
	}
	execID := uuid.New()

	select {
	case e.sem <- struct{}{}:
		defer func() { <-e.sem }()
	case <-ctx.Done():
		return fail(execID, ctx.Err(), false)
	}

	ws, err := os.MkdirTemp(e.cfg.WorkspaceRoot, "claude-task-*")
	if err != nil {
		return fail(execID, err, false)
	}
	defer func() { _ = os.RemoveAll(ws) }()

	args := []string{"-p", buildPrompt(task), "--output-format", "json"}
	if e.cfg.Model != "" {
		args = append(args, "--model", e.cfg.Model)
	}

	procCtx, cancel := context.WithTimeout(ctx, e.cfg.Timeout)
	defer cancel()
	cmd := exec.CommandContext(procCtx, e.bin, args...)
	cmd.Dir = ws
	cmd.Env = childEnv(os.Environ())
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	start := time.Now()
	runErr := cmd.Run()

	var res cliResult
	parseErr := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &res)
	detail := strings.TrimSpace(res.Result + " " + stderr.String())

	switch {
	case looksUnauthenticated(detail):
		e.healthy.Store(false)
		e.logger.Error("Claude CLI is not signed in; steps planned for it will run on agy until `claude` is signed in on the worker")
		return fail(execID, domain.ErrAuthenticationRequired, false)
	case procCtx.Err() != nil:
		return fail(execID, fmt.Errorf("%w: timed out or cancelled: %v", ErrExecution, procCtx.Err()), true)
	case looksRateLimited(detail) && (runErr != nil || res.IsError):
		return fail(execID, ErrRateLimited, true)
	case runErr != nil || res.IsError:
		return fail(execID, fmt.Errorf("%w: %s", ErrExecution, truncate(detail, 300)), true)
	case parseErr != nil:
		return fail(execID, fmt.Errorf("%w: unparseable output", ErrExecution), true)
	case strings.TrimSpace(res.Result) == "":
		return fail(execID, ErrEmptyResult, true)
	}

	text := strings.TrimSpace(res.Result)
	out, _ := json.Marshal(map[string]string{"response": text})
	r := &worker.ExecutionResult{
		ExecutionID:      execID,
		ProviderName:     "claude",
		Status:           "SUCCESS",
		Output:           out,
		TextOutput:       text,
		Duration:         time.Since(start),
		PromptTokens:     res.Usage.InputTokens,
		CompletionTokens: res.Usage.OutputTokens,
	}
	if res.SessionID != "" {
		conv := res.SessionID
		r.ConversationID = &conv
	}
	return r, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

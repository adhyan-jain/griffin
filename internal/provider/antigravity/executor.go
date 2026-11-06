package antigravity

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
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/worker"
)

// ExecutorConfig holds configuration parameters for AntigravityExecutor.
type ExecutorConfig struct {
	BinaryPath                string
	MaxConcurrent             int
	ExecutionTimeout          time.Duration
	GracefulTermination       time.Duration
	WorkspaceRoot             string
	RetainWorkspacesOnFailure bool
	DefaultModel              string
	VersionProbeTimeout       time.Duration // timeout for `agy --version` at construction (default 5s)
	Logger                    *slog.Logger
}

// AntigravityExecutor executes tasks by invoking the Google Antigravity CLI (agy).
type AntigravityExecutor struct {
	cfg       ExecutorConfig
	workspace *WorkspaceManager
	sem       chan struct{}
	logger    *slog.Logger
	facts     RuntimeFacts

	// Health and authentication telemetry
	healthy       atomic.Bool
	healthMu      sync.RWMutex
	lastHealthErr error
}

// NewAntigravityExecutor creates and initializes an AntigravityExecutor.
func NewAntigravityExecutor(cfg ExecutorConfig) (*AntigravityExecutor, error) {
	if cfg.BinaryPath == "" {
		cfg.BinaryPath = "agy"
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 1
	}
	if cfg.MaxConcurrent > 2 {
		cfg.MaxConcurrent = 2 // Hard bounded limit to protect host resources
	}
	if cfg.ExecutionTimeout <= 0 {
		cfg.ExecutionTimeout = 300 * time.Second
	}
	if cfg.GracefulTermination <= 0 {
		cfg.GracefulTermination = 5 * time.Second
	}
	if cfg.VersionProbeTimeout <= 0 {
		cfg.VersionProbeTimeout = 5 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	wsMgr, err := NewWorkspaceManager(cfg.WorkspaceRoot, cfg.RetainWorkspacesOnFailure)
	if err != nil {
		return nil, fmt.Errorf("antigravity executor workspace: %w", err)
	}

	exec := &AntigravityExecutor{
		cfg:       cfg,
		workspace: wsMgr,
		sem:       make(chan struct{}, cfg.MaxConcurrent),
		logger:    cfg.Logger,
	}
	exec.healthy.Store(true)

	model := cfg.DefaultModel
	if model == "" {
		model = "default"
	}
	exec.facts = RuntimeFacts{Name: providerLabel, Version: detectVersion(cfg.BinaryPath, cfg.VersionProbeTimeout), Model: model}
	cfg.Logger.Info("Antigravity runtime detected", "version", exec.facts.Version, "model", exec.facts.Model)

	return exec, nil
}

// RuntimeFacts returns the cached provider runtime description.
func (e *AntigravityExecutor) RuntimeFacts() RuntimeFacts {
	return e.facts
}

// Name returns the provider name.
func (e *AntigravityExecutor) Name() string {
	return "antigravity"
}

// IsHealthy reports whether the executor is functional and authenticated.
func (e *AntigravityExecutor) IsHealthy(ctx context.Context) (bool, error) {
	e.healthMu.RLock()
	lastErr := e.lastHealthErr
	e.healthMu.RUnlock()

	if lastErr != nil {
		return false, lastErr
	}

	// Verify the binary exists in PATH or at specified path
	_, err := exec.LookPath(e.cfg.BinaryPath)
	if err != nil {
		return false, fmt.Errorf("%w: %s", ErrBinaryNotFound, e.cfg.BinaryPath)
	}

	return e.healthy.Load(), nil
}

// MarkUnhealthy explicitly records a health/auth error.
func (e *AntigravityExecutor) MarkUnhealthy(err error) {
	e.healthy.Store(false)
	e.healthMu.Lock()
	e.lastHealthErr = err
	e.healthMu.Unlock()
}

// ResetHealth clears any recorded health errors.
func (e *AntigravityExecutor) ResetHealth() {
	e.healthy.Store(true)
	e.healthMu.Lock()
	e.lastHealthErr = nil
	e.healthMu.Unlock()
}

func deniedNames(d []DeniedAction) string {
	names := make([]string, 0, len(d))
	for _, a := range d {
		n := a.DisplayName
		if n == "" {
			n = a.Action
		}
		names = append(names, n)
	}
	return strings.Join(names, ", ")
}

type inputEnvelope struct {
	ConversationID string          `json:"conversation_id"`
	Model          string          `json:"model"`
	JSONSchema     json.RawMessage `json:"json_schema"`
}

// Execute claims a concurrency token, provisions an isolated scratchpad, invokes the CLI, and reaps process groups.
func (e *AntigravityExecutor) Execute(ctx context.Context, task *domain.Task) (*worker.ExecutionResult, error) {
	// 1. Acquire bounded concurrency token
	select {
	case e.sem <- struct{}{}:
		defer func() { <-e.sem }()
	case <-ctx.Done():
		return &worker.ExecutionResult{
			ProviderName: "antigravity",
			Status:       "FAILED",
			Error:        ctx.Err(),
			Retryable:    false,
		}, ctx.Err()
	}

	execID := uuid.New()
	start := time.Now()

	// 2. Provision isolated scratch workspace
	wsDir, err := e.workspace.CreateWorkspace(task, execID)
	if err != nil {
		return &worker.ExecutionResult{
			ExecutionID:  execID,
			ProviderName: "antigravity",
			Status:       "FAILED",
			Error:        err,
			Retryable:    false,
		}, err
	}

	failed := true
	defer func() {
		_ = e.workspace.CleanupWorkspace(wsDir, failed)
	}()

	// 3. Extract prompt, conversation continuation, model, schema from payload or task definition
	var conversationID string
	var model string
	var jsonSchema []byte

	if len(task.InputPayload) > 0 {
		var env inputEnvelope
		if err := json.Unmarshal(task.InputPayload, &env); err == nil {
			if env.ConversationID != "" {
				conversationID = env.ConversationID
			}
			if env.Model != "" {
				model = env.Model
			}
			if len(env.JSONSchema) > 0 && string(env.JSONSchema) != "null" {
				jsonSchema = env.JSONSchema
			}
		}
	}

	if model == "" {
		model = e.cfg.DefaultModel
	}

	// 4. Construct argv arguments without shell interpolation
	prompt := buildPrompt(task, task.RetryCount+1, e.facts)
	args := []string{
		"-p", prompt,
		"--output-format", "json",
		"--disable-slash-commands",
	}

	if conversationID != "" {
		args = append(args, "--conversation", conversationID)
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if len(jsonSchema) > 0 {
		args = append(args, "--json-schema", string(jsonSchema))
	}

	// 5. Setup subprocess with isolated process group
	procCtx, cancelProc := context.WithTimeout(ctx, e.cfg.ExecutionTimeout)
	defer cancelProc()

	cmd := exec.Command(e.cfg.BinaryPath, args...)
	cmd.Dir = wsDir
	cmd.Env = os.Environ()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	e.logger.Info("Starting Antigravity subprocess",
		"task_id", task.ID,
		"exec_id", execID,
		"binary", e.cfg.BinaryPath,
		"workspace", wsDir,
		"conversation_id", conversationID,
		"agy_version", e.facts.Version,
		"model", e.facts.Model,
	)

	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOENT) {
			e.MarkUnhealthy(ErrBinaryNotFound)
			return &worker.ExecutionResult{
				ExecutionID:  execID,
				ProviderName: "antigravity",
				Status:       "FAILED",
				Error:        ErrBinaryNotFound,
				Retryable:    false,
			}, ErrBinaryNotFound
		}
		return &worker.ExecutionResult{
			ExecutionID:  execID,
			ProviderName: "antigravity",
			Status:       "FAILED",
			Error:        err,
			Retryable:    false,
		}, err
	}

	pgid := cmd.Process.Pid

	// 6. Asynchronous wait with process tree termination
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	var waitErr error
	select {
	case <-procCtx.Done():
		e.logger.Warn("Antigravity process context cancelled or timed out; terminating process group",
			"task_id", task.ID,
			"pgid", pgid,
		)

		// Send SIGTERM to entire process group (-pgid)
		_ = syscall.Kill(-pgid, syscall.SIGTERM)

		// Await graceful termination or enforce SIGKILL
		select {
		case <-done:
			// Terminated gracefully
		case <-time.After(e.cfg.GracefulTermination):
			e.logger.Error("Antigravity process did not exit within graceful period; killing with SIGKILL",
				"task_id", task.ID,
				"pgid", pgid,
			)
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
			<-done // Ensure child is reaped to avoid zombie
		}

		duration := time.Since(start)
		if ctx.Err() != nil {
			return &worker.ExecutionResult{
				ExecutionID:  execID,
				ProviderName: "antigravity",
				Status:       "CANCELLED",
				Duration:     duration,
				Error:        ErrProcessCancelled,
				Retryable:    false,
			}, ErrProcessCancelled
		}

		return &worker.ExecutionResult{
			ExecutionID:  execID,
			ProviderName: "antigravity",
			Status:       "TIMED_OUT",
			Duration:     duration,
			Error:        ErrProcessTimeout,
			Retryable:    true,
		}, ErrProcessTimeout

	case waitErr = <-done:
		// Process completed
	}

	duration := time.Since(start)

	// Determine exit code
	exitCode := 0
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	// 7. Parse structured response and handle errors
	parsed, parseErr := ParseOutput(stdoutBuf.Bytes(), stderrBuf.Bytes(), exitCode)
	if parseErr != nil {
		if errors.Is(parseErr, ErrAuthenticationRequired) {
			e.MarkUnhealthy(ErrAuthenticationRequired)
			e.logger.Error("Antigravity authentication failure encountered", "task_id", task.ID, "error", parseErr)
			return &worker.ExecutionResult{
				ExecutionID:  execID,
				ProviderName: "antigravity",
				Status:       "FAILED",
				Duration:     duration,
				Error:        ErrAuthenticationRequired,
				Retryable:    false,
			}, ErrAuthenticationRequired
		}

		if errors.Is(parseErr, ErrRateLimitExceeded) {
			e.logger.Warn("Antigravity rate limit or quota exceeded", "task_id", task.ID, "error", parseErr)
			return &worker.ExecutionResult{
				ExecutionID:  execID,
				ProviderName: "antigravity",
				Status:       "FAILED",
				Duration:     duration,
				Error:        ErrRateLimitExceeded,
				Retryable:    true,
			}, ErrRateLimitExceeded
		}

		e.logger.Error("Antigravity execution failed to produce valid output",
			"task_id", task.ID,
			"exit_code", exitCode,
			"stderr", truncateRunes(stderrBuf.String(), 500),
			"error", parseErr,
		)
		return &worker.ExecutionResult{
			ExecutionID:  execID,
			ProviderName: "antigravity",
			Status:       "FAILED",
			Duration:     duration,
			Error:        parseErr,
			Retryable:    false,
		}, parseErr
	}

	hasContent := len(parsed.StructuredOutput) > 0 || strings.TrimSpace(parsed.ResponseText) != ""
	if len(parsed.DeniedActions) > 0 {
		names := deniedNames(parsed.DeniedActions)
		if !hasContent {
			cause := fmt.Errorf("%w: model attempted a tool the headless runner cannot approve (denied: %s)", ErrToolPermissionDenied, names)
			e.logger.Warn("Antigravity produced no output because tool actions were denied",
				"task_id", task.ID, "exit_code", exitCode, "agy_version", e.facts.Version, "denied_actions", names)
			return &worker.ExecutionResult{
				ExecutionID:  execID,
				ProviderName: "antigravity",
				Status:       "FAILED",
				Duration:     duration,
				Error:        cause,
				Retryable:    true,
			}, cause
		}
		e.logger.Warn("Antigravity attempted denied tool actions but produced a response",
			"task_id", task.ID, "denied_actions", names)
	} else if !hasContent {
		cause := fmt.Errorf("%w: empty response and no structured output", ErrEmptyResult)
		e.logger.Warn("Antigravity returned an empty result", "task_id", task.ID, "exit_code", exitCode)
		return &worker.ExecutionResult{
			ExecutionID:  execID,
			ProviderName: "antigravity",
			Status:       "FAILED",
			Duration:     duration,
			Error:        cause,
			Retryable:    true,
		}, cause
	}

	// Reset health on verified success
	e.ResetHealth()
	failed = false

	var outPayload json.RawMessage
	if len(parsed.StructuredOutput) > 0 {
		outPayload = parsed.StructuredOutput
	} else if strings.TrimSpace(parsed.ResponseText) != "" {
		encoded, _ := json.Marshal(map[string]string{"response": parsed.ResponseText})
		outPayload = encoded
	}

	if len(outPayload) == 0 {
		e.logger.Error("Antigravity exited 0 but returned no result", "task_id", task.ID, "exec_id", execID, "duration", duration)
		return &worker.ExecutionResult{
			ExecutionID:  execID,
			ProviderName: "antigravity",
			Status:       "FAILED",
			Duration:     duration,
			Error:        ErrEmptyResult,
			Retryable:    true,
		}, ErrEmptyResult
	}

	var convID *string
	if parsed.ConversationID != "" {
		cID := parsed.ConversationID
		convID = &cID
	}

	totalTokens := parsed.Usage["total_tokens"]
	inputTokens := parsed.Usage["input_tokens"]
	outputTokens := parsed.Usage["output_tokens"]

	e.logger.Info("Antigravity execution succeeded",
		"task_id", task.ID,
		"exec_id", execID,
		"duration", duration,
		"total_tokens", totalTokens,
		"exit_code", exitCode,
		"agy_version", e.facts.Version,
		"denied_actions", deniedNames(parsed.DeniedActions),
	)

	return &worker.ExecutionResult{
		ExecutionID:      execID,
		ProviderName:     "antigravity",
		ConversationID:   convID,
		Status:           "SUCCESS",
		Output:           outPayload,
		TextOutput:       parsed.ResponseText,
		Duration:         duration,
		PromptTokens:     inputTokens,
		CompletionTokens: outputTokens,
		Usage: &worker.UsageMetadata{
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
			TotalTokens:  totalTokens,
		},
	}, nil
}

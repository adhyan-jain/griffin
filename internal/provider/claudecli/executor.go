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
	"path/filepath"
	"sort"
	"strconv"
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
	Model         string // default model alias for ordinary steps (optional)
	// LeadModel and Effort apply to lead steps (see domain.ClaudeStepOptions). Empty means the CLI's default.
	LeadModel string
	Effort    string
	// LeadBudgetUSD is the spend limit for one lead step (default 0.50). BudgetCapUSD is the most a step may
	// ask for in its own options (default 2.00): the payload can lower the limit, never raise it past this.
	LeadBudgetUSD float64
	BudgetCapUSD  float64
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
	if cfg.LeadBudgetUSD <= 0 {
		cfg.LeadBudgetUSD = 0.50
	}
	if cfg.BudgetCapUSD <= 0 {
		cfg.BudgetCapUSD = 2.00
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
You have NO tools: do not run commands, open files or browse. If you lack a fact, say so; never invent sources or URLs.
If you find no relevant information, say so in one plain sentence (for example: I couldn't find any information about this.) instead of returning nothing.`

type taskPrompt struct {
	Prompt string `json:"prompt"`
}

// leadPreamble is for a step where Claude runs the whole job with its own sub-agents.
const leadPreamble = `You are the lead of a small team inside Griffin, an AI workforce. Do the whole job you are given.
Plan briefly, then hand each independent part to the sub-agent best suited to it, choosing the cheapest one that can do that part well, and run independent parts at the same time. Do the final checking and writing yourself.
You may read and search the web; you cannot run commands, change files or contact anyone. If you lack a fact, say so; never invent sources or URLs, and cite the address of every source you used.
Answer in clear Markdown: a one-line answer first, then short sections, bullets for lists, tables for comparisons.
If you find no relevant information, say so in one plain sentence instead of returning nothing.`

// roster is the team a lead can delegate to. Models are aliases, so each follows Anthropic's current model
// of that tier. The lead decides who to use; these are the choices and their tools, and every tool is read-only.
const roster = `{
  "researcher": {
    "description": "Finds and reads sources and returns facts with their web addresses. Use for lookups, fetching pages and gathering evidence. Fast and cheap: run several at once for independent questions.",
    "prompt": "You gather facts for the lead. Search, open the most relevant pages, and report only what the sources say, with each source's address. Say plainly what you could not find.",
    "model": "haiku",
    "tools": ["WebSearch", "WebFetch", "Read", "Grep", "Glob"]
  },
  "analyst": {
    "description": "Compares, reasons over and drafts from material that has already been gathered. Use for analysis, comparisons and first drafts.",
    "prompt": "You analyse the material the lead gives you and return a clear, well-organised result. Do not invent facts that are not in the material.",
    "model": "sonnet",
    "tools": ["Read", "Grep", "Glob"]
  },
  "critic": {
    "description": "Checks a conclusion or draft for mistakes, unsupported claims and gaps. Use once, at the end, for answers that matter.",
    "prompt": "You review the lead's draft. List concrete errors, unsupported claims and missing points, most important first. If it is sound, say so briefly.",
    "model": "opus",
    "tools": ["Read", "Grep", "Glob"]
  }
}`

// leadTools is everything a lead step may use: read-only tools and the right to delegate.
const leadTools = "Agent,WebSearch,WebFetch,Read,Grep,Glob"

func buildPrompt(task *domain.Task) string {
	body := task.Goal
	var p taskPrompt
	if len(task.InputPayload) > 0 && json.Unmarshal(task.InputPayload, &p) == nil && p.Prompt != "" {
		body = p.Prompt
	}
	return preamble + "\n\n" + body
}

func buildLeadPrompt(task *domain.Task) string {
	return leadPreamble + "\n\n" + strings.TrimPrefix(buildPrompt(task), preamble+"\n\n")
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
	TotalCostUSD float64 `json:"total_cost_usd"`
	NumTurns     int     `json:"num_turns"`
	// ModelUsage is keyed by the models that actually ran, the lead's and its sub-agents'.
	ModelUsage    map[string]json.RawMessage `json:"modelUsage"`
	SubagentStats struct {
		Spawned int            `json:"spawned"`
		Failed  int            `json:"failed"`
		ByType  map[string]int `json:"by_type"`
	} `json:"subagent_stats"`
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

	opts := domain.ParseClaudeOptions(task.InputPayload)
	args := []string{"-p", buildPrompt(task), "--output-format", "json"}
	model := e.cfg.Model
	if opts.Model != "" {
		model = opts.Model
	}
	effort := ""
	if opts.Lead {
		args[1] = buildLeadPrompt(task)
		if opts.Model == "" && e.cfg.LeadModel != "" {
			model = e.cfg.LeadModel
		}
		effort = opts.Effort
		if effort == "" {
			effort = e.cfg.Effort
		}
		budget := e.cfg.LeadBudgetUSD
		if opts.Budget > 0 {
			budget = opts.Budget
		}
		if budget > e.cfg.BudgetCapUSD {
			budget = e.cfg.BudgetCapUSD // a step may lower its limit, never raise it past the owner's cap
		}
		rosterPath := filepath.Join(ws, "agents.json")
		if err := os.WriteFile(rosterPath, []byte(roster), 0o600); err != nil {
			return fail(execID, err, false)
		}
		// dontAsk refuses anything not listed, so a lead can use these read-only tools and nothing else, and
		// bypassPermissions is never used.
		args = append(args, "--agents", rosterPath, "--allowedTools", leadTools, "--permission-mode", "dontAsk",
			"--max-budget-usd", strconv.FormatFloat(budget, 'f', 2, 64))
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if effort != "" {
		args = append(args, "--effort", effort)
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
	var out []byte
	if opts.Lead {
		// Record who did the work and what it cost, so Attempts can show it and the owner can tune the budget.
		models := make([]string, 0, len(res.ModelUsage))
		for m := range res.ModelUsage {
			models = append(models, m)
		}
		sort.Strings(models)
		out, _ = json.Marshal(map[string]any{"response": text, "claude": map[string]any{
			"lead": true, "cost_usd": res.TotalCostUSD, "turns": res.NumTurns, "models": models, "effort": effort,
			"subagents": map[string]any{"spawned": res.SubagentStats.Spawned, "failed": res.SubagentStats.Failed, "by_type": res.SubagentStats.ByType},
		}})
	} else {
		out, _ = json.Marshal(map[string]string{"response": text})
	}
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

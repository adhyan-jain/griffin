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
	"github.com/griffin/core/internal/worker"
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
  lead)  printf '%s\n' "$@" > "$ARGS_FILE"; echo '{"is_error":false,"result":"Lead answer.","session_id":"s-2","total_cost_usd":0.1234,"num_turns":5,"usage":{"input_tokens":50,"output_tokens":20},"modelUsage":{"claude-sonnet-5-5":{},"claude-haiku-5-5":{}},"subagent_stats":{"spawned":3,"failed":1,"by_type":{"researcher":2,"critic":1}}}' ;;
  args)  printf '%s\n' "$@" > "$ARGS_FILE"; echo '{"is_error":false,"result":"plain"}' ;;
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

func leadTask(payload string) *domain.Task {
	return &domain.Task{Title: "Compare laptops", Goal: "Compare the best laptops under 60000", InputPayload: json.RawMessage(payload)}
}

// runRecording runs a step against the fake CLI and returns the arguments it was given.
func runRecording(t *testing.T, cfg Config, mode string, tk *domain.Task) ([]string, *worker.ExecutionResult) {
	t.Helper()
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	t.Setenv("MOCK_MODE", mode)
	t.Setenv("ARGS_FILE", argsFile)
	cfg.BinaryPath, cfg.WorkspaceRoot, cfg.Timeout = mockCLI(t), t.TempDir(), 10*time.Second
	e, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.Execute(context.Background(), tk)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	raw, _ := os.ReadFile(argsFile)
	return strings.Split(strings.TrimRight(string(raw), "\n"), "\n"), res
}

func argAfter(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func TestLeadStepGetsARosterReadOnlyToolsAndABudget(t *testing.T) {
	args, res := runRecording(t, Config{LeadModel: "sonnet", Effort: "medium"}, "lead", leadTask(`{"claude":{"lead":true}}`))

	if argAfter(args, "--permission-mode") != "dontAsk" || argAfter(args, "--allowedTools") != "Agent,WebSearch,WebFetch,Read,Grep,Glob" {
		t.Fatalf("a lead may only use the listed read-only tools: %v", args)
	}
	for _, a := range args {
		if strings.Contains(a, "bypassPermissions") || strings.Contains(a, "dangerously") || a == "Bash" || a == "Write" || a == "Edit" {
			t.Fatalf("a lead must never get write tools or permission bypass: %q", a)
		}
	}
	if argAfter(args, "--max-budget-usd") != "0.50" || argAfter(args, "--model") != "sonnet" || argAfter(args, "--effort") != "medium" {
		t.Fatalf("defaults should apply: %v", args)
	}
	// the fake CLI prints one argument per line, so a multi-line prompt spans several entries
	if all := strings.Join(args, "\n"); !strings.HasPrefix(args[1], "You are the lead of a small team") || !strings.Contains(all, "Compare the best laptops under 60000") {
		t.Fatalf("the lead prompt must carry the job: %.120s", all)
	}
	// the roster file is valid, has the three agents and gives each its own model
	path := argAfter(args, "--agents")
	if path == "" {
		t.Fatalf("no --agents: %v", args)
	}
	var team map[string]struct {
		Model string   `json:"model"`
		Tools []string `json:"tools"`
	}
	// the workspace is deleted after the run, so the roster is checked from its source
	if err := json.Unmarshal([]byte(roster), &team); err != nil {
		t.Fatalf("the roster must be valid JSON: %v", err)
	}
	if team["researcher"].Model != "haiku" || team["analyst"].Model != "sonnet" || team["critic"].Model != "opus" || len(team) != 3 {
		t.Fatalf("unexpected roster: %+v", team)
	}
	for name, a := range team {
		for _, tool := range a.Tools {
			if tool == "Bash" || tool == "Write" || tool == "Edit" || tool == "Agent" {
				t.Errorf("%s must not have %s", name, tool)
			}
		}
	}

	// what happened is recorded
	var out struct {
		Response string `json:"response"`
		Claude   struct {
			Lead      bool     `json:"lead"`
			Cost      float64  `json:"cost_usd"`
			Turns     int      `json:"turns"`
			Models    []string `json:"models"`
			Effort    string   `json:"effort"`
			Subagents struct {
				Spawned, Failed int
				ByType          map[string]int `json:"by_type"`
			} `json:"subagents"`
		} `json:"claude"`
	}
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatal(err)
	}
	if out.Response != "Lead answer." || !out.Claude.Lead || out.Claude.Cost != 0.1234 || out.Claude.Turns != 5 || out.Claude.Effort != "medium" {
		t.Fatalf("telemetry: %s", res.Output)
	}
	if strings.Join(out.Claude.Models, ",") != "claude-haiku-5-5,claude-sonnet-5-5" || out.Claude.Subagents.Spawned != 3 || out.Claude.Subagents.ByType["researcher"] != 2 {
		t.Fatalf("who ran: %s", res.Output)
	}
}

func TestLeadBudgetCanBeLoweredButNeverRaisedPastTheOwnersCap(t *testing.T) {
	cases := map[string]string{
		`{"claude":{"lead":true,"budget_usd":0.10}}`:   "0.10",
		`{"claude":{"lead":true,"budget_usd":50}}`:     "2.00", // capped
		`{"claude":{"lead":true,"budget_usd":-3}}`:     "0.50", // invalid: default
		`{"claude":{"lead":true,"budget_usd":"lots"}}`: "0.50",
		`{"claude":{"lead":true}}`:                     "0.50",
	}
	for payload, want := range cases {
		args, _ := runRecording(t, Config{}, "lead", leadTask(payload))
		if got := argAfter(args, "--max-budget-usd"); got != want {
			t.Errorf("%s: budget %s, want %s", payload, got, want)
		}
	}
	args, _ := runRecording(t, Config{LeadBudgetUSD: 0.25, BudgetCapUSD: 0.40}, "lead", leadTask(`{"claude":{"lead":true,"budget_usd":1}}`))
	if argAfter(args, "--max-budget-usd") != "0.40" {
		t.Errorf("the owner's cap must hold: %v", args)
	}
}

func TestLeadModelAndEffortComeFromTheStepOnlyWhenValid(t *testing.T) {
	args, _ := runRecording(t, Config{LeadModel: "sonnet", Effort: "low"}, "lead", leadTask(`{"claude":{"lead":true,"model":"OPUS","effort":"high"}}`))
	if argAfter(args, "--model") != "opus" || argAfter(args, "--effort") != "high" {
		t.Errorf("a valid step choice wins: %v", args)
	}
	args, _ = runRecording(t, Config{LeadModel: "sonnet", Effort: "low"}, "lead", leadTask(`{"claude":{"lead":true,"model":"gpt-9; rm -rf /","effort":"ultra"}}`))
	if argAfter(args, "--model") != "sonnet" || argAfter(args, "--effort") != "low" {
		t.Errorf("an unknown model or effort must never reach the command line: %v", args)
	}
}

func TestOrdinaryStepsAreUnchanged(t *testing.T) {
	args, res := runRecording(t, Config{Model: "haiku", Effort: "high"}, "args", &domain.Task{Title: "t", Goal: "Summarise"})
	for _, flag := range []string{"--agents", "--allowedTools", "--permission-mode", "--max-budget-usd", "--effort"} {
		if argAfter(args, flag) != "" {
			t.Errorf("an ordinary step must not get %s: %v", flag, args)
		}
	}
	if argAfter(args, "--model") != "haiku" || !strings.HasPrefix(args[1], "You are a specialist inside Griffin") {
		t.Errorf("ordinary steps keep their prompt and model: %v", args)
	}
	if string(res.Output) != `{"response":"plain"}` {
		t.Errorf("ordinary output is just the response, got %s", res.Output)
	}
}

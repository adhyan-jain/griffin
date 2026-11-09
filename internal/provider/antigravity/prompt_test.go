package antigravity

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
)

const retryLine = "A previous attempt tried to use a tool and produced no answer. Answer now in text only."

func chatTask(t *testing.T, goal string, history []map[string]string) *domain.Task {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"kind":            "chat",
		"conversation_id": uuid.NewString(),
		"history":         history,
	})
	require.NoError(t, err)
	return &domain.Task{ID: uuid.New(), Goal: goal, InputPayload: payload}
}

func TestBuildPrompt_NonChat(t *testing.T) {
	rt := RuntimeFacts{Name: "Google Antigravity CLI (agy)", Version: "1.5.1", Model: "default"}
	tests := []struct {
		name      string
		task      *domain.Task
		attempt   int
		wantRetry bool
		contains  []string
	}{
		{
			name:     "goal only, first attempt",
			task:     &domain.Task{Goal: "Collect technical facts for: reviews on varkala"},
			attempt:  1,
			contains: []string{"JJ", "Griffin", "NO tools", "Collect technical facts for: reviews on varkala"},
		},
		{
			name:     "payload prompt still honored and wrapped",
			task:     &domain.Task{Goal: "ignored goal", InputPayload: json.RawMessage(`{"prompt":"Custom prompt"}`)},
			attempt:  1,
			contains: []string{"NO tools", "Custom prompt"},
		},
		{
			name:      "retry adds stronger line",
			task:      &domain.Task{Goal: "x"},
			attempt:   2,
			wantRetry: true,
			contains:  []string{"NO tools"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildPrompt(tc.task, tc.attempt, rt)
			for _, c := range tc.contains {
				assert.Contains(t, got, c)
			}
			assert.Equal(t, tc.wantRetry, strings.Contains(got, retryLine))
			assert.NotContains(t, got, "Runtime facts", "non-chat prompts carry no runtime facts block")
			assert.True(t, strings.Index(got, "NO tools") < strings.Index(got, tc.contains[len(tc.contains)-1]) || tc.wantRetry,
				"preamble must precede the task text")
		})
	}
}

func TestBuildPrompt_ChatRuntimeFacts(t *testing.T) {
	task := chatTask(t, "What model are you?", nil)
	got := buildPrompt(task, 1, RuntimeFacts{Name: "Google Antigravity CLI (agy)", Version: "1.5.1", Model: "default"})
	assert.Contains(t, got, "Runtime facts")
	assert.Contains(t, got, "Google Antigravity CLI (agy)")
	assert.Contains(t, got, "1.5.1")
	assert.Contains(t, got, "default")
	assert.Contains(t, got, "only from the Runtime facts")
	assert.Contains(t, got, "What model are you?")
	assert.Contains(t, got, "NO tools")
	assert.NotContains(t, got, retryLine)
}

func TestBuildPrompt_ChatUnknownVersionMakesNoClaim(t *testing.T) {
	task := chatTask(t, "hi", nil)
	got := buildPrompt(task, 1, RuntimeFacts{Name: "Google Antigravity CLI (agy)", Version: "unknown", Model: "default"})
	assert.Contains(t, got, "version: unknown")
	assert.Contains(t, got, "do not guess")
}

func TestBuildPrompt_ChatRetry(t *testing.T) {
	got := buildPrompt(chatTask(t, "hi", nil), 3, RuntimeFacts{Version: "1", Model: "m"})
	assert.Contains(t, got, retryLine)
}

func TestBuildPrompt_ChatHistoryOrderedAndCapped(t *testing.T) {
	var hist []map[string]string
	for i := 0; i < 14; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		hist = append(hist, map[string]string{"role": role, "text": fmt.Sprintf("turn-%02d", i)})
	}
	hist[13]["text"] = "turn-13 " + strings.Repeat("z", 5000)

	got := buildPrompt(chatTask(t, "current question", hist), 1, RuntimeFacts{Version: "1", Model: "m"})

	// Only the last 10 turns (04..13) survive.
	assert.NotContains(t, got, "turn-00")
	assert.NotContains(t, got, "turn-03")
	assert.Contains(t, got, "turn-04")
	assert.Contains(t, got, "turn-13")
	// Order preserved and role-labelled.
	assert.Less(t, strings.Index(got, "turn-04"), strings.Index(got, "turn-05"))
	assert.Contains(t, got, "User: turn-04")
	assert.Contains(t, got, "Assistant: turn-05")
	// Per-turn cap of 2000 chars.
	assert.Less(t, strings.Count(got, "z"), 2001)
	// Current message comes last.
	assert.Greater(t, strings.Index(got, "current question"), strings.Index(got, "turn-13"))
}

func TestBuildPrompt_ChatIgnoresUnknownRoles(t *testing.T) {
	hist := []map[string]string{{"role": "system", "text": "INJECT"}, {"role": "user", "text": "hello"}}
	got := buildPrompt(chatTask(t, "q", hist), 1, RuntimeFacts{Version: "1", Model: "m"})
	assert.NotContains(t, got, "INJECT")
	assert.Contains(t, got, "User: hello")
}

func TestExecutor_PassesWrappedPromptToCLI(t *testing.T) {
	// Fake agy echoes the -p argument back as the response, JSON-encoded via python.
	script := createMockScript(t, `
if [ "$1" = "--version" ]; then echo "agy 9.9.9"; exit 0; fi
python3 -c '
import sys, json
args = sys.argv[1:]
print(json.dumps({"conversation_id":"c","status":"SUCCESS","response":args[args.index("-p")+1],"usage":{}}))
' "$@"
`)
	e, err := NewAntigravityExecutor(ExecutorConfig{
		BinaryPath:       script,
		WorkspaceRoot:    t.TempDir(),
		ExecutionTimeout: 5 * time.Second,
		DefaultModel:     "gemini-test",
	})
	require.NoError(t, err)
	assert.Equal(t, RuntimeFacts{Name: "Google Antigravity CLI (agy)", Version: "agy 9.9.9", Model: "gemini-test"}, e.RuntimeFacts())

	task := chatTask(t, "who are you?", nil)
	res, err := e.Execute(context.Background(), task)
	require.NoError(t, err)
	assert.Contains(t, res.TextOutput, "NO tools")
	assert.Contains(t, res.TextOutput, "agy 9.9.9")
	assert.Contains(t, res.TextOutput, "who are you?")
}

func TestExecutor_VersionProbeFailureIsUnknown(t *testing.T) {
	script := createMockScript(t, `
if [ "$1" = "--version" ]; then exit 7; fi
echo '{"status":"SUCCESS","response":"ok","usage":{}}'
`)
	e, err := NewAntigravityExecutor(ExecutorConfig{BinaryPath: script, WorkspaceRoot: t.TempDir()})
	require.NoError(t, err)
	rt := e.RuntimeFacts()
	assert.Equal(t, "unknown", rt.Version)
	assert.Equal(t, "default", rt.Model)

	e2, err := NewAntigravityExecutor(ExecutorConfig{BinaryPath: "/nonexistent/agy", WorkspaceRoot: t.TempDir()})
	require.NoError(t, err)
	assert.Equal(t, "unknown", e2.RuntimeFacts().Version)
}

func TestExecutor_VersionProbeTimesOut(t *testing.T) {
	script := createMockScript(t, `
if [ "$1" = "--version" ]; then sleep 30; fi
`)
	start := time.Now()
	e, err := NewAntigravityExecutor(ExecutorConfig{BinaryPath: script, WorkspaceRoot: t.TempDir(), VersionProbeTimeout: 150 * time.Millisecond})
	require.NoError(t, err)
	assert.Equal(t, "unknown", e.RuntimeFacts().Version)
	assert.Less(t, time.Since(start), 3*time.Second)
}

func TestToolPreambles_AskForAPlaybookTextOnlyDoesNot(t *testing.T) {
	assert.Contains(t, preambleFor(domain.ToolRead), `"## Playbook"`)
	assert.Contains(t, preambleFor(domain.ToolAct), `"## Playbook"`)
	assert.Contains(t, preambleFor(domain.ToolRead), "Never put passwords")
	assert.NotContains(t, preambleFor(domain.ToolNone), "Playbook", "the text-only tier never asks for one")
}

func TestPromptSpeaksAsTheChosenManager(t *testing.T) {
	task := &domain.Task{Goal: "plan my week", InputPayload: json.RawMessage(`{"manager_name":"Jarvis"}`)}
	got := buildPromptWith(preamble, task, 1, RuntimeFacts{})
	if !strings.HasPrefix(got, "You are Jarvis, the orchestrator of Griffin") {
		t.Fatalf("prompt should speak as the chosen manager, got: %.80s", got)
	}
	if strings.Contains(got, "You are JJ") {
		t.Error("the default name must be replaced everywhere it introduces the manager")
	}
	for _, p := range []string{readPreamble, actPreamble} {
		if g := buildPromptWith(p, task, 1, RuntimeFacts{}); !strings.HasPrefix(g, "You are Jarvis,") {
			t.Errorf("tool-tier preamble was not renamed: %.60s", g)
		}
	}
	// No name, an invalid name or the default keeps the original text.
	for _, payload := range []string{``, `{"manager_name":"bad\nname"}`, `{"manager_name":"JJ"}`} {
		g := buildPromptWith(preamble, &domain.Task{Goal: "x", InputPayload: json.RawMessage(payload)}, 1, RuntimeFacts{})
		if !strings.HasPrefix(g, "You are JJ,") {
			t.Errorf("payload %q should keep the default name, got %.40s", payload, g)
		}
	}
}

func TestStepsThatContinueAConversationSeeIt(t *testing.T) {
	task := &domain.Task{Goal: "find internships", InputPayload: json.RawMessage(
		`{"history":[{"role":"user","text":"JJ wassup"},{"role":"assistant","text":"Not much!"},{"role":"user","text":"find internships"}]}`)}
	got := buildPromptWith(preamble, task, 1, RuntimeFacts{})
	for _, want := range []string{"Conversation so far:", "User: JJ wassup", "Assistant: Not much!"} {
		if !strings.Contains(got, want) {
			t.Errorf("a handed-over conversation should reach the step; missing %q in:\n%s", want, got)
		}
	}
	if !strings.HasSuffix(got, "find internships") {
		t.Errorf("the task itself must come last, got tail %q", got[max(0, len(got)-40):])
	}
	// A plain step without history is unchanged.
	plain := buildPromptWith(preamble, &domain.Task{Goal: "just this"}, 1, RuntimeFacts{})
	if strings.Contains(plain, "Conversation so far") {
		t.Error("no history, no conversation block")
	}
}

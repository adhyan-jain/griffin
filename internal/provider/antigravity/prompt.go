package antigravity

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/worker"
)

const (
	maxChatTurns     = 10
	maxChatTurnChars = 2000
	providerLabel    = "Google Antigravity CLI (agy)"
)

// RuntimeFacts describes the provider runtime so the model can answer
// "what are you running on" questions from facts instead of guessing.
type RuntimeFacts = worker.RuntimeFacts

const preamble = `You are JJ, the orchestrator of Griffin, an AI workforce.
Answer directly in plain text (Markdown is allowed).
You have NO tools: do not run shell commands, do not inspect files or the workspace, do not browse.
If the request needs facts you cannot verify, say so explicitly and do not invent sources or URLs.
Keep to the requested format.
If you find no relevant information, say so in one plain sentence (for example: I couldn't find any information about this.) instead of returning nothing.`

// readPreamble is used for the read tier: research with tools, change nothing.
const readPreamble = `You are JJ, the orchestrator of Griffin, an AI workforce.
You may use READ-ONLY tools for this step: search the web, open public pages, read files in your workspace.
You must NOT change anything: do not log in, submit forms, send messages, post, buy, install, delete or run commands that change state outside your scratch workspace.
Cite every source with its URL. If you could not verify a fact, say so plainly; never invent sources.
If you find no relevant information, say so in one plain sentence (for example: I couldn't find any information about this.) instead of returning nothing.
Answer in clear Markdown: a one-line answer first, then short sections, bullets for lists, tables for comparisons.
When you finish, if you worked out a reusable procedure (which sites, steps and pitfalls), add a final section headed exactly "## Playbook" with short numbered steps so the next similar task is faster. Never put passwords, tokens, cookies, keys or personal data in it.`

// actPreamble is used for the act tier, which only runs after the user approved this exact task.
const actPreamble = `You are JJ, the orchestrator of Griffin, an AI workforce.
The user approved this specific task, so you may act on their behalf, but only to do exactly what the task describes and nothing more.
Stop and report instead of continuing if you meet anything unexpected: a payment, a CAPTCHA, a login you were not given credentials for, or a request for personal data the task did not mention.
Never invent credentials, and never enter secrets you were not explicitly given for this task.
Report precisely what you did, with links, in clear Markdown. If you did not complete something, say so.
When you finish, if you worked out a reusable procedure (which sites, steps and pitfalls), add a final section headed exactly "## Playbook" with short numbered steps so the next similar task is faster. Never put passwords, tokens, cookies, keys or personal data in it.`

// preambleFor picks the preamble for a tool tier. The default is the text-only preamble.
func preambleFor(mode domain.ToolMode) string {
	switch mode {
	case domain.ToolRead:
		return readPreamble
	case domain.ToolAct:
		return actPreamble
	default:
		return preamble
	}
}

const retryPreamble = "A previous attempt tried to use a tool and produced no answer. Answer now in text only."

type chatTurn struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

type chatEnvelope struct {
	Kind    string     `json:"kind"`
	Prompt  string     `json:"prompt"`
	History []chatTurn `json:"history"`
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// buildPrompt wraps every task in a text-only preamble. Chat tasks additionally
// get a grounded runtime facts block and the role-labelled conversation history.
func buildPrompt(task *domain.Task, attemptNumber int, rt RuntimeFacts) string {
	return buildPromptWith(preamble, task, attemptNumber, rt)
}

// buildPromptWith is buildPrompt with an explicit preamble (the tool tier decides which).
func buildPromptWith(pre string, task *domain.Task, attemptNumber int, rt RuntimeFacts) string {
	var env chatEnvelope
	if len(task.InputPayload) > 0 {
		_ = json.Unmarshal(task.InputPayload, &env)
	}
	body := task.Goal
	if env.Prompt != "" {
		body = env.Prompt
	}

	var b strings.Builder
	b.WriteString(pre)
	b.WriteString("\n")
	if attemptNumber > 1 {
		b.WriteString(retryPreamble)
		b.WriteString("\n")
	}

	if env.Kind != "chat" {
		b.WriteString("\n")
		b.WriteString(body)
		return b.String()
	}

	version, model := rt.Version, rt.Model
	if version == "" {
		version = "unknown"
	}
	if model == "" {
		model = "default"
	}
	name := rt.Name
	if name == "" {
		name = providerLabel
	}
	fmt.Fprintf(&b, "\nRuntime facts:\n- provider: %s\n- version: %s\n- model: %s\n", name, version, model)
	b.WriteString("State the provider, version and model only from the Runtime facts block. If a value is unknown, say you do not know; do not guess.\n")

	turns := env.History
	if len(turns) > maxChatTurns {
		turns = turns[len(turns)-maxChatTurns:]
	}
	var hist []string
	for _, t := range turns {
		var label string
		switch t.Role {
		case "user":
			label = "User"
		case "assistant":
			label = "Assistant"
		default:
			continue
		}
		hist = append(hist, fmt.Sprintf("%s: %s", label, truncateRunes(t.Text, maxChatTurnChars)))
	}
	if len(hist) > 0 {
		b.WriteString("\nConversation so far:\n")
		b.WriteString(strings.Join(hist, "\n"))
		b.WriteString("\n")
	}
	b.WriteString("\nCurrent user message:\n")
	b.WriteString(body)
	return b.String()
}

// detectVersion runs `<binary> --version` once with a bounded timeout.
// Any failure yields "unknown"; it never panics or blocks beyond the timeout.
func detectVersion(binary string, timeout time.Duration) string {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "--version")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 500 * time.Millisecond
	out, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	line := strings.TrimSpace(strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0])
	if line == "" {
		return "unknown"
	}
	return truncateRunes(line, 100)
}

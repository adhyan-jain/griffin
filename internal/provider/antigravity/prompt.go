package antigravity

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"os"
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
You may use READ-ONLY tools for this step: search the web, open public pages, read files in your workspace, inspect git repositories and local directories.
You must NOT change anything outside: do not log in, submit forms, send messages, post, buy, install, delete or run commands that change state outside your scratch workspace.
Cite every source with its URL or file path. If you could not verify a fact, say so plainly; never invent sources.
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

type ImageData struct {
	MimeType string `json:"mime_type"`
	Data     string `json:"data"`
}

type chatEnvelope struct {
	Kind    string      `json:"kind"`
	Prompt  string      `json:"prompt"`
	History []chatTurn  `json:"history"`
	Images  []ImageData `json:"images,omitempty"`
	// Role is a team role written for this request (see service.DynamicPlanner).
	Role struct {
		Name         string `json:"name"`
		Instructions string `json:"instructions"`
	} `json:"role"`
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// oneRunRule stops the model ending a run with "I started a background job and will wait for it": nothing picks
// that up, so the run's final answer is all the owner ever gets.
const oneRunRule = "Do the whole task in this one run. Never start background jobs, subagents or anything you would come back to later, and never end with \"I am waiting\": your final answer is all the owner gets, so it must hold the finished result."

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
	// The preambles are written for the default name; speak as the manager the owner chose.
	if name := domain.ManagerNameFrom(task.InputPayload); name != domain.DefaultManagerName {
		pre = strings.Replace(pre, "You are "+domain.DefaultManagerName+",", "You are "+name+",", 1)
	}

	var b strings.Builder
	b.WriteString(pre)
	b.WriteString("\n")
	b.WriteString(oneRunRule)
	b.WriteString("\n")
	if role := roleLine(env.Role.Name, env.Role.Instructions); role != "" {
		b.WriteString(role)
		b.WriteString("\n")
	}
	if len(env.Images) > 0 {
		fmt.Fprintf(&b, "Attached images: %d image(s) provided in workspace as attached_image_*.png.\n", len(env.Images))
	}
	if attemptNumber > 1 {
		b.WriteString(retryPreamble)
		b.WriteString("\n")
	}

	if isCareerTask(task) {
		appendCareerContext(&b, task)
	}

	if notes := task.SteeringNotes(); len(notes) > 0 {
		b.WriteString("\nOwner Mid-Flight Steering Directives:\n")
		for _, n := range notes {
			fmt.Fprintf(&b, "- %s\n", n)
		}
		b.WriteString("You must follow these directives strictly.\n")
	}

	if env.Kind != "chat" {
		// A step that continues a conversation (for example a voice chat that became work) must see it.
		if hist := historyBlock(env.History); hist != "" {
			b.WriteString("\n")
			b.WriteString(hist)
		}
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

	if hist := historyBlock(env.History); hist != "" {
		b.WriteString("\n")
		b.WriteString(hist)
	}
	b.WriteString("\nCurrent user message:\n")
	b.WriteString(body)
	return b.String()
}

// roleLine tells a worker which part of the team it is. Both fields were written by the planner for one
// request, so they are length-limited and stripped of control characters before they reach a prompt.
func roleLine(name, instructions string) string {
	clean := func(s string, max int) string {
		s = strings.Join(strings.Fields(strings.Map(func(r rune) rune {
			if r < 32 || r == 127 {
				return -1
			}
			return r
		}, s)), " ")
		return truncateRunes(s, max)
	}
	n, i := clean(name, 40), clean(instructions, 600)
	if n == "" {
		return ""
	}
	if i == "" {
		return "Your role on this team: " + n + "."
	}
	return "Your role on this team: " + n + ". " + i
}

// historyBlock renders the role-labelled conversation so far, or "" when there is none.
func historyBlock(all []chatTurn) string {
	turns := all
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
	if len(hist) == 0 {
		return ""
	}
	return "Conversation so far:\n" + strings.Join(hist, "\n") + "\n"
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

// CandidateContext supplies the verified owner-profile block for career prompts. It is set at
// startup from the stored profile; when nil or empty, no personal claims are injected at all
// (fail closed), so the model cannot be fed unverified facts from source code.
var CandidateContext func(userID uuid.UUID) string

// Hard constraints that are style rules, not facts about the owner.
const careerStyleConstraints = `- Strictly ZERO emojis in all code, text, commits, and summaries.
- Never state the owner's age.`

type jobPayloadView struct {
	Company          string            `json:"company"`
	Role             string            `json:"role"`
	Tier             string            `json:"tier"`
	JobLink          string            `json:"job_link"`
	PortalURL        string            `json:"portal_url"`
	BaseResume       string            `json:"base_resume"`
	ResumeSent       string            `json:"resume_sent"`
	BulletDiff       string            `json:"bullet_diff"`
	CoverNote        string            `json:"cover_note"`
	ScreeningAnswers map[string]string `json:"screening_answers"`
	Location         string            `json:"location"`
	Stipend          string            `json:"stipend"`
}

func isCareerTask(task *domain.Task) bool {
	if task == nil {
		return false
	}
	if strings.EqualFold(task.ManagerDomain, "career") {
		return true
	}
	combined := strings.ToLower(task.Title + " " + task.Goal)
	return strings.Contains(combined, "resume") || strings.Contains(combined, "apply") || strings.Contains(combined, "internship") || strings.Contains(combined, "job application")
}

func appendCareerContext(b *strings.Builder, task *domain.Task) {
	if !isCareerTask(task) {
		return
	}
	b.WriteString("\n")
	if CandidateContext != nil {
		if ctxBlock := CandidateContext(task.UserID); ctxBlock != "" {
			b.WriteString(ctxBlock)
			b.WriteString("\n")
		}
	}
	b.WriteString(careerStyleConstraints)
	b.WriteString("\n")

	if len(task.InputPayload) == 0 {
		return
	}

	var job jobPayloadView
	if err := json.Unmarshal(task.InputPayload, &job); err != nil {
		return
	}

	portal := job.PortalURL
	if portal == "" {
		portal = job.JobLink
	}
	resumePath := job.ResumeSent
	if resumePath == "" {
		resumePath = job.BaseResume
	}

	hasJobDetails := job.Company != "" || job.Role != "" || portal != "" || resumePath != "" || job.BulletDiff != "" || job.CoverNote != "" || len(job.ScreeningAnswers) > 0
	if !hasJobDetails {
		return
	}

	b.WriteString("\nApplication Package Details:\n")
	if job.Company != "" {
		fmt.Fprintf(b, "- Target Company: %s\n", job.Company)
	}
	if job.Role != "" {
		fmt.Fprintf(b, "- Target Role: %s\n", job.Role)
	}
	if job.Tier != "" {
		fmt.Fprintf(b, "- Tier: %s\n", job.Tier)
	}
	if portal != "" {
		fmt.Fprintf(b, "- Official Portal URL: %s\n", portal)
	}
	if job.Location != "" {
		fmt.Fprintf(b, "- Location: %s\n", job.Location)
	}
	if job.Stipend != "" {
		fmt.Fprintf(b, "- Stipend: %s\n", job.Stipend)
	}
	if resumePath != "" {
		fmt.Fprintf(b, "- Tailored Resume Path: %s\n", resumePath)
	}
	if job.BulletDiff != "" {
		fmt.Fprintf(b, "- Bullet Highlights:\n%s\n", job.BulletDiff)
	}
	if job.CoverNote != "" {
		fmt.Fprintf(b, "- Cover Pitch Note:\n%s\n", job.CoverNote)
	}
	if len(job.ScreeningAnswers) > 0 {
		b.WriteString("- Screening Answers:\n")
		for q, a := range job.ScreeningAnswers {
			fmt.Fprintf(b, "  * %s: %s\n", q, a)
		}
	}

	if resumePath != "" {
		if data, err := os.ReadFile(resumePath); err == nil && len(data) > 0 {
			b.WriteString("\nAttached Tailored Resume Document:\n```markdown\n")
			b.Write(data)
			b.WriteString("\n```\n")
		}
	}
}

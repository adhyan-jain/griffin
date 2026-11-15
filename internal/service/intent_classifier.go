package service

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/provider/gemini"
)

// IntentKind represents the classified intent of a user message.
type IntentKind string

const (
	IntentNone          IntentKind = ""
	IntentListTasks     IntentKind = "list_tasks"
	IntentListApprovals IntentKind = "list_approvals"
	IntentApprove       IntentKind = "approve"
	IntentReject        IntentKind = "reject"
	IntentCancelTasks   IntentKind = "cancel_tasks"
	IntentDeleteTasks   IntentKind = "delete_tasks"
	IntentSteerTask     IntentKind = "steer_task"
	IntentAutomatedCall IntentKind = "automated_call"
	IntentCreateTasks   IntentKind = "create_tasks"
	IntentObjective     IntentKind = "objective"
	IntentConversation  IntentKind = "conversation"
)

// ExtractedTask represents an individual task decomposed from user input.
type ExtractedTask struct {
	Title         string              `json:"title"`
	Goal          string              `json:"goal"`
	ManagerDomain string              `json:"manager_domain,omitempty"`
	Priority      domain.TaskPriority `json:"priority,omitempty"`
	Reason        string              `json:"reason,omitempty"`
}

// ParsedIntent is the structured outcome of classification and task decomposition.
type ParsedIntent struct {
	Kind     IntentKind      `json:"intent"`
	Scope    string          `json:"scope,omitempty"`    // "all", "active", "done", "failed"
	Ref      string          `json:"ref,omitempty"`      // specific task reference or recipient
	Target   string          `json:"target,omitempty"`   // spoken message or target string
	Steering string          `json:"steering,omitempty"` // steering note or instruction
	Tasks    []ExtractedTask `json:"tasks,omitempty"`    // decomposed individual tasks
	Reason   string          `json:"reason,omitempty"`
}

// ToTaskCommand converts the parsed intent to a TaskCommand when applicable.
func (p ParsedIntent) ToTaskCommand() (TaskCommand, bool) {
	switch p.Kind {
	case IntentListTasks:
		scope := p.Scope
		if scope == "" {
			scope = "all"
		}
		return TaskCommand{Kind: CmdListTasks, Scope: scope, Ref: p.Ref}, true
	case IntentListApprovals:
		return TaskCommand{Kind: CmdListApprovals, Scope: p.Scope, Ref: p.Ref}, true
	case IntentApprove:
		return TaskCommand{Kind: CmdApprove, Scope: p.Scope, Ref: p.Ref}, true
	case IntentReject:
		return TaskCommand{Kind: CmdReject, Scope: p.Scope, Ref: p.Ref}, true
	case IntentCancelTasks:
		scope := p.Scope
		if scope == "" && p.Ref == "" {
			scope = "active"
		}
		return TaskCommand{Kind: CmdCancelTasks, Scope: scope, Ref: p.Ref}, true
	case IntentDeleteTasks:
		scope := p.Scope
		if scope == "" && p.Ref == "" {
			scope = "done"
		}
		return TaskCommand{Kind: CmdDeleteTasks, Scope: scope, Ref: p.Ref}, true
	case IntentSteerTask:
		ref := p.Ref
		if ref == "" {
			ref = p.Steering
		}
		return TaskCommand{Kind: CmdSteerTask, Scope: p.Scope, Ref: ref}, true
	case IntentAutomatedCall:
		return TaskCommand{Kind: CmdAutomatedCall, Ref: p.Ref, Target: p.Target}, true
	}
	return TaskCommand{}, false
}

// ClassifierBrain is the fast language model interface.
type ClassifierBrain interface {
	Enabled() bool
	Chat(ctx context.Context, system string, turns []gemini.Turn) (string, error)
}

// SmartClassifier classifies user requests using Gemini Flash Lite with graceful regex fallback.
type SmartClassifier struct {
	Brain ClassifierBrain
}

// NewSmartClassifier constructs a SmartClassifier.
func NewSmartClassifier(brain ClassifierBrain) *SmartClassifier {
	return &SmartClassifier{Brain: brain}
}

const classifierSystemPrompt = `You are the primary decision-making and intent classification engine for Griffin personal assistant.
Analyze the user utterance and return structured JSON. Consult your full semantic understanding rather than relying on keywords or rigid patterns.

DECISION MAKING & TASK DECOMPOSITION INSTRUCTIONS:
1. INDEPENDENT TASKS DECOMPOSITION:
   - When the user asks to perform or create 2 or more independent tasks that do not interact with each other (e.g. "1. Research X 2. Buy groceries 3. Fix auth bug", "research laptops and also message mom on whatsapp saying I'll be late and check open github issues", "make a task to buy groceries and another task to clean disk"):
     You MUST set "intent": "create_tasks" and decompose them into separate, distinct task objects in the "tasks" array!
     NEVER merge independent tasks that do not interact with each other. Each task must stand on its own.
   - For every task item in "tasks":
     * "title": Short, descriptive summary (max 60 chars, e.g. "Research quantum computing", "Buy groceries")
     * "goal": Complete, standalone instructions for what needs to be accomplished
     * "manager_domain": Choose the best specialist manager:
       - "research": Deep research, web lookup, analysis, comparisons, fact checking, reports
       - "engineering": Code, GitHub, git repos, bug fixing, test writing, debugging, refactoring
       - "personal_ops": Everyday personal tasks, shopping, WhatsApp messages, reminders, browser navigation, calls
       - "career": Job applications, resumes, tailoring, cover letters, internships
       - "hackathon": Hackathons, bounties, grants, project submissions
       - "freelance": Freelance gigs, client proposals, contracts, project bidding
       - "learning": Curriculums, study roadmaps, tutorials, DSA exercises
       - "finance": Cloud spending, expenses, subscriptions, budgets
       - "product_ideas": Product design, specs, PRDs, feature ideation
       - "networking": Outreach, CRM, follow-up messages
       - "tech_intelligence": AI models, release monitoring, benchmarks, tech news
     * "priority": "CRITICAL_P0" | "HIGH_P1" | "NORMAL_P2" | "BACKGROUND_P3" (default "NORMAL_P2")

2. SINGLE TASK / WORK:
   - If the user requests a single objective or asks to create a task ("make a task to buy milk", "research postgres skip locked", "send a whatsapp to boss"):
     Set "intent": "create_tasks" (or "objective") and provide exactly 1 item in the "tasks" array with its title, goal, manager_domain, and priority.

3. COMMANDS & CONTROLS:
   - "list_tasks": Checking, viewing, or listing open/past tasks or status ("what are my tasks", "tasks batao", "kya chal raha hai", "open tasks"). scope: "all"|"active"|"done"|"failed".
   - "list_approvals": Checking pending approvals ("any approvals", "what needs approval").
   - "approve": Approving an action or draft ("approve it", "haan bhej do", "confirm").
   - "reject": Rejecting an action or draft ("reject it", "don't send that", "cancel the send").
   - "cancel_tasks": Cancelling active tasks ("cancel everything", "stop the research").
   - "delete_tasks": Cleaning up finished tasks ("clean up done tasks").
   - "steer_task": Adding notes or steering in-flight tasks ("give me the draft first", "pehle draft dikha", "also mention Friday"). steering: note text.
   - "automated_call": Outbound voice phone call ("make an automated call to mom and tell her...", "call rahul saying..."). ref: recipient, target: message.

4. CONVERSATION:
   - "conversation": General questions, coding help answered directly, explanations, greetings, chit-chat ("hello", "what is a goroutine", "tell me a joke").

Return raw JSON only without markdown formatting:
{
  "intent": "create_tasks|objective|list_tasks|list_approvals|approve|reject|cancel_tasks|delete_tasks|steer_task|automated_call|conversation",
  "scope": "all|active|done|failed",
  "ref": "...",
  "target": "...",
  "steering": "...",
  "tasks": [
    {
      "title": "...",
      "goal": "...",
      "manager_domain": "...",
      "priority": "NORMAL_P2"
    }
  ],
  "reason": "..."
}`

// Classify determines the intent of text.
// When an LLM Brain is available, it consults the LLM first for intelligent semantic decision-making.
// Only when the LLM is unconfigured, disabled, or unavailable does it fall back to deterministic regexes.
func (c *SmartClassifier) Classify(ctx context.Context, text string) ParsedIntent {
	clean := strings.TrimSpace(text)
	if clean == "" {
		return ParsedIntent{Kind: IntentConversation, Reason: "empty"}
	}

	// 1. Fast deterministic database commands (list tasks, approvals, approve, reject, cancel, delete).
	// These read or act on existing DB state and do not require LLM decomposition or reasoning.
	if cmd := ParseTaskCommand(clean); cmd.Kind != CmdNone && cmd.Kind != CmdAutomatedCall {
		return ParsedIntent{
			Kind:   IntentKind(cmd.Kind),
			Scope:  cmd.Scope,
			Ref:    cmd.Ref,
			Target: cmd.Target,
			Reason: "regex-task-command",
		}
	}

	// 2. If LLM Brain is available, ALWAYS consult the LLM for intelligent decision-making,
	// task creation, multi-task decomposition, and domain routing.
	if c.Brain != nil && c.Brain.Enabled() {
		intent, err := c.classifyWithBrain(ctx, clean)
		if err == nil && intent.Kind != IntentNone {
			return intent
		}
	}

	// 3. Deterministic regex fallback when LLM is unavailable or offline.
	return c.classifyWithRegex(clean)
}

func (c *SmartClassifier) classifyWithBrain(ctx context.Context, text string) (ParsedIntent, error) {
	callCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	turns := []gemini.Turn{
		{Role: "user", Text: text},
	}

	raw, err := c.Brain.Chat(callCtx, classifierSystemPrompt, turns)
	if err != nil {
		return ParsedIntent{}, err
	}

	// Clean any markdown code blocks from model output
	clean := strings.TrimSpace(raw)
	clean = strings.TrimPrefix(clean, "```json")
	clean = strings.TrimPrefix(clean, "```")
	clean = strings.TrimSuffix(clean, "```")
	clean = strings.TrimSpace(clean)

	var out ParsedIntent
	if err := json.Unmarshal([]byte(clean), &out); err != nil {
		return ParsedIntent{}, err
	}

	switch out.Kind {
	case IntentListTasks, IntentListApprovals, IntentApprove, IntentReject,
		IntentCancelTasks, IntentDeleteTasks, IntentSteerTask, IntentAutomatedCall,
		IntentCreateTasks, IntentObjective, IntentConversation:
		return out, nil
	}

	return ParsedIntent{}, nil
}

var (
	reLineNumberedItem = regexp.MustCompile(`(?m)^\s*(?:(?:\d+[\.\)]|[-*•]|(?:task|item)\s+\d+[:\.]?))\s*(.+?)\s*$`)
	reCompoundTasks    = regexp.MustCompile(`(?i)^(?:please\s+)?(?:make|create|add)\s+(?:a\s+)?(?:new\s+)?task\s+(?:to\s+|for\s+)?(.+?)(?:,\s*and\s+(?:also\s+)?|\s+and\s+(?:another\s+task\s+(?:to\s+|for\s+)?|also\s+(?:make\s+(?:a\s+)?task\s+(?:to\s+|for\s+)?)?))(.+)$`)
	reSingleTask       = regexp.MustCompile(`(?i)^(?:please\s+)?(?:make|create|add|schedule)\s+(?:a\s+)?(?:new\s+)?task\s+(?:to\s+|for\s+)?(.+)$`)
)

func extractTasksRegex(text string) []ExtractedTask {
	// 1. Check for numbered or bulleted lines (at least 2 items)
	lines := reLineNumberedItem.FindAllStringSubmatch(text, -1)
	if len(lines) >= 2 {
		var tasks []ExtractedTask
		for _, m := range lines {
			item := strings.TrimSpace(m[1])
			if item == "" {
				continue
			}
			mgr := classifyIntent(item)
			tasks = append(tasks, ExtractedTask{
				Title:         truncateRunes(item, 60),
				Goal:          item,
				ManagerDomain: mgr,
				Priority:      domain.TaskPriorityNormalP2,
			})
		}
		if len(tasks) >= 2 {
			return tasks
		}
	}

	// 2. Check for compound sentence: "make a task to X and another task to Y"
	if m := reCompoundTasks.FindStringSubmatch(text); len(m) == 3 {
		t1 := strings.TrimSpace(m[1])
		t2 := strings.TrimSpace(m[2])
		if t1 != "" && t2 != "" {
			return []ExtractedTask{
				{
					Title:         truncateRunes(t1, 60),
					Goal:          t1,
					ManagerDomain: classifyIntent(t1),
					Priority:      domain.TaskPriorityNormalP2,
				},
				{
					Title:         truncateRunes(t2, 60),
					Goal:          t2,
					ManagerDomain: classifyIntent(t2),
					Priority:      domain.TaskPriorityNormalP2,
				},
			}
		}
	}

	// 3. Single explicit task command: "make a task to X"
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "task list") && !strings.Contains(lower, "list of task") {
		if m := reSingleTask.FindStringSubmatch(text); len(m) == 2 {
			goal := strings.TrimSpace(m[1])
			if goal != "" {
				return []ExtractedTask{
					{
						Title:         truncateRunes(goal, 60),
						Goal:          goal,
						ManagerDomain: classifyIntent(goal),
						Priority:      domain.TaskPriorityNormalP2,
					},
				}
			}
		}
	}

	return nil
}

func (c *SmartClassifier) classifyWithRegex(text string) ParsedIntent {
	// 1. Check for multi-task or explicit task creation patterns
	if tasks := extractTasksRegex(text); len(tasks) > 0 {
		return ParsedIntent{
			Kind:   IntentCreateTasks,
			Tasks:  tasks,
			Reason: "regex-task-creation",
		}
	}

	// 2. Check for automated call command
	if strings.Contains(strings.ToLower(text), "call") {
		cmd := ParseTaskCommand(text)
		if cmd.Kind == CmdAutomatedCall {
			return ParsedIntent{
				Kind:   IntentAutomatedCall,
				Ref:    cmd.Ref,
				Target: cmd.Target,
				Reason: "regex-automated-call",
			}
		}
	}

	// 3. Task commands
	if cmd := ParseTaskCommand(text); cmd.Kind != CmdNone {
		return ParsedIntent{
			Kind:   IntentKind(cmd.Kind),
			Scope:  cmd.Scope,
			Ref:    cmd.Ref,
			Target: cmd.Target,
			Reason: "regex-task-command",
		}
	}

	// 4. Deliverable or multi-step work
	kind, reason := ClassifyMessageWithReason(text)
	if kind == MessageKindObjective {
		return ParsedIntent{
			Kind: IntentObjective,
			Tasks: []ExtractedTask{
				{
					Title:         truncateRunes(text, 60),
					Goal:          text,
					ManagerDomain: classifyIntent(text),
					Priority:      domain.TaskPriorityNormalP2,
				},
			},
			Reason: reason,
		}
	}

	return ParsedIntent{
		Kind:   IntentConversation,
		Reason: reason,
	}
}

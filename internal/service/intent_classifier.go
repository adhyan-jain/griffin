package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

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
	IntentConversation  IntentKind = "conversation"
	IntentObjective     IntentKind = "objective"
)

// ParsedIntent is the structured outcome of classification.
type ParsedIntent struct {
	Kind     IntentKind `json:"intent"`
	Scope    string     `json:"scope,omitempty"`    // "all", "active", "done", "failed"
	Ref      string     `json:"ref,omitempty"`      // specific task reference or target
	Steering string     `json:"steering,omitempty"` // steering note or instruction
	Reason   string     `json:"reason,omitempty"`
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

const classifierSystemPrompt = `You are the fast intent classifier for Griffin personal assistant.
Classify the user utterance into exactly one of these intents:
- list_tasks: asking to view, list, check, or hear their current or past tasks or status (e.g. 'give me the list of task', 'what are my tasks', 'tasks batao', 'kya chal raha hai', 'show my task', 'what's on my plate', 'fetch tasks')
- list_approvals: asking to see pending approvals ('any approvals', 'what needs approval', 'approvals batao')
- approve: approving an action or task ('approve it', 'haan bhej do', 'theek hai confirm karo', 'yes send it')
- reject: rejecting an action or task ('reject it', 'cancel the send', 'don't send that', 'deny')
- cancel_tasks: cancelling a running task ('cancel everything', 'stop the research')
- delete_tasks: clearing/deleting tasks ('clean up finished tasks', 'delete old tasks')
- steer_task: modifying, appending to, or adding instructions to an ongoing or in-flight task ('give me the draft first', 'pehle draft dikha', 'also mention X', 'dont send yet', 'hold on before sending', 'change the message to Y')
- conversation: questions, explanations, greetings, small talk ('hello', 'what is a goroutine', 'how does python work', 'tell me a joke')
- objective: requesting NEW multi-step work, actions on devices, or deliverables ('research top 5 laptops', 'send a whatsapp to mom', 'write a report on linux', 'find remote jobs')

Return raw JSON only without markdown formatting:
{"intent": "...", "scope": "all|active|done|failed", "ref": "...", "steering": "...", "reason": "..."}`

// Classify determines the intent of text.
func (c *SmartClassifier) Classify(ctx context.Context, text string) ParsedIntent {
	clean := strings.TrimSpace(text)
	if clean == "" {
		return ParsedIntent{Kind: IntentConversation, Reason: "empty"}
	}

	// 1. Fast regex check for unambiguous task commands (0ms, 0 token cost).
	if cmd := ParseTaskCommand(clean); cmd.Kind != CmdNone {
		return ParsedIntent{
			Kind:   IntentKind(cmd.Kind),
			Scope:  cmd.Scope,
			Ref:    cmd.Ref,
			Reason: "regex-task-command",
		}
	}

	// 2. Fast regex check for clear objectives (0ms, 0 token cost).
	if kind, reason := ClassifyMessageWithReason(clean); kind == MessageKindObjective {
		return ParsedIntent{
			Kind:   IntentObjective,
			Reason: reason,
		}
	}

	// 3. For natural language nuances, Hindi/Hinglish, steering, or ambiguous turns,
	// use LLM brain for deep semantic classification.
	if c.Brain != nil && c.Brain.Enabled() {
		intent, err := c.classifyWithBrain(ctx, clean)
		if err == nil && intent.Kind != IntentNone {
			return intent
		}
	}

	// 4. Default conversation.
	return ParsedIntent{
		Kind:   IntentConversation,
		Reason: "default-conversation",
	}
}

func (c *SmartClassifier) classifyWithBrain(ctx context.Context, text string) (ParsedIntent, error) {
	callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
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
		IntentCancelTasks, IntentDeleteTasks, IntentSteerTask, IntentConversation, IntentObjective:
		return out, nil
	}

	return ParsedIntent{}, nil
}

func (c *SmartClassifier) classifyWithRegex(text string) ParsedIntent {
	// Task commands
	if cmd := ParseTaskCommand(text); cmd.Kind != CmdNone {
		return ParsedIntent{
			Kind:   IntentKind(cmd.Kind),
			Scope:  cmd.Scope,
			Ref:    cmd.Ref,
			Reason: "regex-task-command",
		}
	}

	// Deliverable or multi-step work
	kind, reason := ClassifyMessageWithReason(text)
	if kind == MessageKindObjective {
		return ParsedIntent{
			Kind:   IntentObjective,
			Reason: reason,
		}
	}

	return ParsedIntent{
		Kind:   IntentConversation,
		Reason: reason,
	}
}

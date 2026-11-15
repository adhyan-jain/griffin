package service

import (
	"regexp"
	"strings"
)

// MessageKind says how a JJ chat message should be handled.
type MessageKind string

const (
	// MessageKindConversation is answered directly by one chat task (no orchestration).
	MessageKindConversation MessageKind = "conversation"
	// MessageKindObjective is a request for a deliverable or multi-step work and goes through
	// the planner/managers (JJOrchestrator.SubmitRequest).
	MessageKindObjective MessageKind = "objective"
)

// ClassifyMessage decides whether a chat message is a conversation or an objective.
// Ambiguous text is a conversation: that is the cheap path (one task, no orchestration).
func ClassifyMessage(text string) MessageKind {
	kind, _ := ClassifyMessageWithReason(text)
	return kind
}

var (
	sentenceSplitRe = regexp.MustCompile(`[^.!?\n]+[.!?]*`)

	// Greetings / address / politeness at the start of a sentence.
	leadFillerRe = regexp.MustCompile(`^(?:(?:hi|hello|hey|yo|howdy|greetings|good (?:morning|afternoon|evening)|thanks|thank you|ok|okay|so|well|please|jj)\b[\s,:;-]*)+`)
	// Polite request prefixes: the rest of the sentence is an imperative, even when phrased as a question.
	politePrefixRe = regexp.MustCompile(`^(?:can|could|would|will) you (?:please )?|^(?:i need|i want|i'd like|i would like) you to |^(?:let's|lets) |^go ahead and `)

	questionOpenerRe = regexp.MustCompile(`^(?:what|what's|whats|why|how|who|whom|whose|when|where|which|explain|describe|tell me|define|is|are|was|were|has|have|should|can|could|would|will|(?:do|does|did) (?:you|i|it|the|a|an|we|they|he|she|this|that|my))\b`)

	// Imperatives that are always a request for work.
	objectiveVerbRe = regexp.MustCompile(`^(?:research|investigate|compare|analy[sz]e|audit|draft|build|create|prepare|plan|compile|benchmark|evaluate|find|gather|collect|design|develop|implement|review|fix|debug|refactor|optimi[sz]e|migrate|apply)\b`)
	// Imperatives that only count when a deliverable is named.
	conditionalVerbRe = regexp.MustCompile(`^(?:write|summari[sz]e|produce|generate|make|put together|give me|draw up)\b`)

	deliverableRe = regexp.MustCompile(`\b(?:reports?|tables?|documents?|proposals?|spreadsheets?|presentations?|slide deck|whitepaper|dossier|essay|article|email|letter|brief|spec|readme|script|program|plan|comparison|sources|citations|references|roadmap|checklist|lists?)\b`)
	needWordRe    = regexp.MustCompile(`\b(?:need|want|give|produce|generate|prepare|draft|write|compile)\b`)
)

// ClassifyMessageWithReason is ClassifyMessage plus a short, stable reason for logs and tests.
func ClassifyMessageWithReason(text string) (MessageKind, string) {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" {
		return MessageKindConversation, "empty"
	}
	if cmd := ParseTaskCommand(t); cmd.Kind != CmdNone {
		return MessageKindConversation, "task-command:" + string(cmd.Kind)
	}
	for _, s := range sentenceSplitRe.FindAllString(t, -1) {
		if reason := objectiveSentenceReason(strings.TrimSpace(s)); reason != "" {
			return MessageKindObjective, reason
		}
	}
	// Work on the owner's own devices or the web is never a chat answer: a chat reply can only say it cannot do it.
	// These requests must reach the planner, which hands them to a device that can.
	if reason := deviceRequestReason(t); reason != "" {
		return MessageKindObjective, reason
	}
	return MessageKindConversation, "default-conversation"
}

// deviceRequestReason spots a request for WhatsApp or a web search (the planner's own detectors decide what counts),
// and leaves questions about them ("how does WhatsApp Web work?") as conversation.
func deviceRequestReason(t string) string {
	s := strings.Trim(leadFillerRe.ReplaceAllString(t, ""), " \t.!?")
	polite := false
	if loc := politePrefixRe.FindString(s); loc != "" {
		polite = true
		s = strings.TrimSpace(strings.TrimPrefix(s, loc))
		s = strings.TrimSpace(leadFillerRe.ReplaceAllString(s, ""))
	}
	asked := strings.HasSuffix(strings.TrimSpace(t), "?") || questionOpenerRe.MatchString(s)
	if asked && !polite {
		return ""
	}
	if polite && questionOpenerRe.MatchString(s) {
		return ""
	}
	switch {
	case wantsWhatsApp(t):
		return "whatsapp-request"
	case wantsInstagram(t):
		return "instagram-request"
	case wantsWeb(t):
		return "web-request"
	}
	return ""
}

// objectiveSentenceReason returns a non-empty reason when the sentence asks for a deliverable
// or multi-step work.
func objectiveSentenceReason(s string) string {
	question := strings.HasSuffix(s, "?")
	s = strings.Trim(leadFillerRe.ReplaceAllString(s, ""), " \t.!?")
	if s == "" {
		return ""
	}

	if cmd := ParseTaskCommand(s); cmd.Kind != CmdNone {
		return ""
	}
	if ok, _ := isTaskListQuery(norm(s)); ok {
		return ""
	}
	if isApprovalListQuery(norm(s)) {
		return ""
	}

	polite := false
	if loc := politePrefixRe.FindString(s); loc != "" {
		polite = true
		s = strings.TrimSpace(strings.TrimPrefix(s, loc))
		s = strings.TrimSpace(leadFillerRe.ReplaceAllString(s, ""))
	}
	if !polite && (questionOpenerRe.MatchString(s) || question) {
		return ""
	}
	if polite && questionOpenerRe.MatchString(s) {
		return "" // "can you explain ..." stays a conversation
	}

	if m := objectiveVerbRe.FindString(s); m != "" {
		return "imperative-verb:" + m
	}
	if m := conditionalVerbRe.FindString(s); m != "" && deliverableRe.MatchString(s) {
		return "deliverable-request:" + m
	}
	if deliverableRe.MatchString(s) && needWordRe.MatchString(s) && !question {
		return "deliverable-noun"
	}
	return ""
}

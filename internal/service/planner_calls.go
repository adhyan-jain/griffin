package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
)

var (
	callLead  = regexp.MustCompile(`(?i)\b(?:calling|call|phone|dial|ring|sim\s+call|voice\s*call|make\s+(?:a\s+)?call\s+to|place\s+(?:a\s+)?call\s+to|automated\s+call\s+to|whatsapp\s+call\s+to|whatsapp\s+call)\s+(?:to\s+)?(?:my\s+)?([A-Za-z0-9+]+)`)
	callWord  = regexp.MustCompile(`(?i)\b(?:automated\s+call|phone\s+call|cellular\s+call|voice\s+call|sim\s+call|whatsapp\s+call|call\s+(?:on|via)\s+whatsapp)\b`)
	nonCallRe = regexp.MustCompile(`(?i)\b(?:api|function|method|system|rpc|rest|webhook|contract|database|sql)\s+call\b|\bcall\s+stack\b|\bso[- ]called\b|\bcall\s+it\s+a\s+day\b`)
)

// callFiller are words that follow "call" without naming anyone ("call him instead", "do a normal sim call again").
var callFiller = map[string]bool{"instead": true, "now": true, "him": true, "her": true, "them": true, "normal": true,
	"sim": true, "again": true, "back": true, "me": true, "to": true, "my": true, "a": true, "the": true}

// callRecipient is the contact named in a call request: the first word after a call verb that is not filler, or ""
// when none is named (the caller must then ask, never guess).
func callRecipient(text string) string {
	for _, m := range callLead.FindAllStringSubmatch(text, -1) {
		if w := strings.TrimSpace(m[1]); w != "" && !callFiller[strings.ToLower(w)] {
			return w
		}
	}
	return ""
}

// ErrCallRecipientMissing is returned when a call request names nobody; the owner is asked who to call.
var ErrCallRecipientMissing = errors.New("who should I call? Tell me the contact's name")

// CallChannel represents the transport: cellular SIM or WhatsApp VoIP.
type CallChannel string

const (
	CallChannelCellular CallChannel = "cellular"
	CallChannelWhatsApp CallChannel = "whatsapp"
)

// CallMode defines how Griffin conducts the call:
// - "automated_bg": Griffin dials, suppresses dialer into background, talks and listens via TTS/STT on user's behalf.
// - "supervised_listen": Griffin dials on speakerphone, keeps screen/call UI visible, talks and listens via TTS/STT, owner listens in.
// - "direct_talk": Griffin simply places the call and hands off the line to the owner to talk directly.
type CallMode string

const (
	CallModeAutomatedBg      CallMode = "automated_bg"
	CallModeSupervisedListen CallMode = "supervised_listen"
	CallModeDirectTalk       CallMode = "direct_talk"
)

// wantsCall is true when the user asks to place a voice/cellular phone call or WhatsApp call to someone.
func wantsCall(text string) bool {
	if _, ok := domain.ParseReminder(text); ok {
		return false // "remind me to call mom" is a reminder, not a call
	}
	lower := strings.ToLower(text)
	if nonCallRe.MatchString(lower) {
		return false
	}
	if strings.Contains(lower, "whatsapp call") || strings.Contains(lower, "call on whatsapp") || strings.Contains(lower, "call via whatsapp") {
		return true
	}
	if callLead.MatchString(text) || callWord.MatchString(lower) {
		return true
	}
	return false
}

// extractCallDetails parses recipient, message, channel, and mode from the request text.
func extractCallDetails(text string) (target string, message string, channel CallChannel, mode CallMode, inBackground bool) {
	lower := strings.ToLower(text)
	channel = CallChannelCellular
	if strings.Contains(lower, "whatsapp") {
		channel = CallChannelWhatsApp
	}

	inBackground = strings.Contains(lower, "background") || strings.Contains(lower, "in the bg") || strings.Contains(lower, "in bg") || strings.Contains(lower, "silently")

	forMe := strings.Contains(lower, "for me") ||
		strings.Contains(lower, "i'll talk") ||
		strings.Contains(lower, "i will talk") ||
		strings.Contains(lower, "let me talk") ||
		strings.Contains(lower, "open it and i'll talk") ||
		strings.Contains(lower, "open it and i talk") ||
		strings.Contains(lower, "just open it")

	listenOnly := strings.Contains(lower, "i can listen") ||
		strings.Contains(lower, "i will listen") ||
		strings.Contains(lower, "i'll listen") ||
		strings.Contains(lower, "let me listen") ||
		strings.Contains(lower, "you talk and i listen") ||
		strings.Contains(lower, "you talk and i can listen") ||
		strings.Contains(lower, "you talk, i listen")

	isAutomated := inBackground || strings.Contains(lower, "automated") || strings.Contains(lower, "auto call") || strings.Contains(lower, "message like") || strings.Contains(lower, "saying") || strings.Contains(lower, "speak")

	if forMe {
		mode = CallModeDirectTalk
	} else if isAutomated || inBackground {
		mode = CallModeAutomatedBg
	} else if listenOnly {
		mode = CallModeSupervisedListen
	} else {
		mode = CallModeAutomatedBg
	}

	target = callRecipient(text)

	msgPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(?:tell|telling)\s+(?:him|her|them|[A-Za-z0-9+]+)\s+(?:that\s+)?(.*)$`),
		regexp.MustCompile(`(?i)\b(?:saying|say)\s+(?:that\s+)?(.*)$`),
		regexp.MustCompile(`(?i)\b(?:with\s+message|message)\s+(?:like\s+|that\s+)?(.*)$`),
		regexp.MustCompile(`(?i)\b(?:with\s+text|text)\s+(?:like\s+|that\s+)?(.*)$`),
	}
	for _, p := range msgPatterns {
		if sub := p.FindStringSubmatch(text); len(sub) > 1 {
			msg := strings.TrimSpace(sub[1])
			// Strip trailing qualifiers
			msg = regexp.MustCompile(`(?i)\s*\bin\s+the\s+background\b|\s*\bin\s+background\b|\s*\bin\s+the\s+bg\b|\s*\bin\s+bg\b|\s*\bfor\s+me\b|\s*\band\s+i(?:'ll|\s+will)\s+listen\b|\s*\band\s+i\s+can\s+listen\b`).ReplaceAllString(msg, "")
			msg = strings.TrimSpace(msg)
			if msg != "" {
				message = msg
				break
			}
		}
	}

	if message == "" && mode != CallModeDirectTalk && target != "" {
		message = fmt.Sprintf("Hello %s, this is an automated message from Griffin, an assistant calling on behalf of its owner.", target)
	}

	return target, message, channel, mode, inBackground
}

// callSteps decomposes a phone call request into a cellular/whatsapp call task.
func callSteps(req *domain.UserRequest) ([]domain.ChildTaskPlan, []domain.PlanDependency) {
	target, message, channel, mode, inBackground := extractCallDetails(req.RequestText)
	return []domain.ChildTaskPlan{BuildCallTask(target, message, channel, mode, inBackground, req.Priority)}, nil
}

// BuildCallTask is the one task shape for a phone call: the phone resolves the contact name and asks the owner
// before dialling.
func BuildCallTask(target, message string, channel CallChannel, mode CallMode, inBackground bool, priority domain.TaskPriority) domain.ChildTaskPlan {
	callID := uuid.New().String()
	action := "automated_call"
	if mode == CallModeDirectTalk {
		action = "direct_call"
	}
	if channel == CallChannelWhatsApp {
		action = "whatsapp_call"
	}
	inputJSON, _ := json.Marshal(map[string]any{
		"requires": []string{domain.HostCapCalls},
		"phone": map[string]any{
			"action":         action,
			"channel":        string(channel),
			"call_mode":      string(mode),
			"number":         target,
			"recipient_name": target,
			"message":        message,
			"call_id":        callID,
			"in_background":  inBackground,
		},
	})

	var channelTitle string
	if channel == CallChannelWhatsApp {
		channelTitle = "WhatsApp"
	} else {
		channelTitle = "Cellular"
	}

	var modeDesc string
	switch mode {
	case CallModeDirectTalk:
		modeDesc = "direct handoff"
	case CallModeAutomatedBg:
		modeDesc = "background agent"
	case CallModeSupervisedListen:
		modeDesc = "supervised speaker"
	}

	title := fmt.Sprintf("%s call to %s (%s)", channelTitle, target, modeDesc)
	goal := fmt.Sprintf("%s call to %s (%s): %s", channelTitle, target, modeDesc, message)
	if mode == CallModeDirectTalk {
		goal = fmt.Sprintf("Open %s call to %s for owner to speak directly", strings.ToLower(channelTitle), target)
	}

	return domain.ChildTaskPlan{
		Key:                  "cellular_call",
		Title:                title,
		Goal:                 goal,
		ManagerDomain:        "personal_ops",
		WorkerKey:            "cellular_caller",
		InputPayload:         inputJSON,
		Priority:             priority,
		CapabilitiesRequired: []string{domain.HostCapCalls},
	}
}

// ValidCallContact reports whether a name can be called: it is a real name or number, not a filler word.
func ValidCallContact(name string) bool {
	n := strings.TrimSpace(name)
	return n != "" && !callFiller[strings.ToLower(n)]
}

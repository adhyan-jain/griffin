package service

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
)

var (
	callLead  = regexp.MustCompile(`(?i)\b(?:call|phone|dial|ring|voice\s*call|make\s+(?:a\s+)?call\s+to|place\s+(?:a\s+)?call\s+to|automated\s+call\s+to)\s+(?:to\s+)?(?:my\s+)?([A-Za-z0-9+]+)`)
	callWord  = regexp.MustCompile(`(?i)\b(?:automated\s+call|phone\s+call|cellular\s+call|voice\s+call)\b`)
	nonCallRe = regexp.MustCompile(`(?i)\b(?:api|function|method|system|rpc|rest|webhook|contract|database|sql)\s+call\b|\bcall\s+stack\b|\bso[- ]called\b|\bcall\s+it\s+a\s+day\b`)
)

// wantsCall is true when the user asks to place a voice/cellular phone call to someone.
func wantsCall(text string) bool {
	lower := strings.ToLower(text)
	if nonCallRe.MatchString(lower) {
		return false
	}
	if callLead.MatchString(text) || callWord.MatchString(lower) {
		return true
	}
	return false
}

// extractCallDetails parses the recipient, message, and in-background preference from the request text.
func extractCallDetails(text string) (target string, message string, inBackground bool) {
	lower := strings.ToLower(text)
	inBackground = strings.Contains(lower, "background")

	if m := callLead.FindStringSubmatch(text); len(m) > 1 {
		target = strings.TrimSpace(m[1])
	}
	if target == "" {
		target = "Hiten"
	}

	msgPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(?:tell|telling)\s+(?:him|her|them|[A-Za-z0-9+]+)\s+(?:that\s+)?(.*)$`),
		regexp.MustCompile(`(?i)\b(?:saying|say)\s+(?:that\s+)?(.*)$`),
		regexp.MustCompile(`(?i)\b(?:with\s+message|message)\s+(?:that\s+)?(.*)$`),
	}
	for _, p := range msgPatterns {
		if sub := p.FindStringSubmatch(text); len(sub) > 1 {
			msg := strings.TrimSpace(sub[1])
			// Strip trailing "in background" if present
			msg = regexp.MustCompile(`(?i)\s*\bin\s+the\s+background\b|\s*\bin\s+background\b`).ReplaceAllString(msg, "")
			msg = strings.TrimSpace(msg)
			if msg != "" {
				message = msg
				break
			}
		}
	}
	if message == "" {
		message = fmt.Sprintf("Hello %s, this is an automated message from Adhyan.", target)
	}
	return target, message, inBackground
}

// callSteps decomposes a phone call request into a cellular phone task requiring the "calls" capability.
// Only the Android companion app advertises and runs tasks with domain.HostCapCalls.
func callSteps(req *domain.UserRequest) ([]domain.ChildTaskPlan, []domain.PlanDependency) {
	target, message, inBackground := extractCallDetails(req.RequestText)
	callID := uuid.New().String()
	inputJSON, _ := json.Marshal(map[string]any{
		"requires": []string{domain.HostCapCalls},
		"phone": map[string]any{
			"action":         "automated_call",
			"number":         target,
			"recipient_name": target,
			"message":        message,
			"call_id":        callID,
			"in_background":  inBackground,
		},
	})
	return []domain.ChildTaskPlan{
		{
			Key:                  "cellular_call",
			Title:                fmt.Sprintf("Cellular call to %s", target),
			Goal:                 fmt.Sprintf("Autonomous cellular call to %s: %s", target, message),
			ManagerDomain:        "personal_ops",
			WorkerKey:            "cellular_caller",
			InputPayload:         inputJSON,
			Priority:             req.Priority,
			CapabilitiesRequired: []string{domain.HostCapCalls},
		},
	}, nil
}

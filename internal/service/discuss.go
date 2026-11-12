package service

import (
	"regexp"
	"strings"
)

// Discussion before work. An objective normally runs the moment it is understood. Some jobs (a filter for
// internships, a cold-mail campaign) need to be talked through and tuned first. Two phrases switch that on:
// "let's discuss it first" keeps the request a conversation in which the manager proposes a plan and waits, and
// "go ahead" then submits everything the owner said in that discussion as one objective. Nothing here guesses:
// without one of the phrases the behaviour is exactly what it was.

var discussFirstRe = regexp.MustCompile(`(?i)\b(?:let'?s|lets|can we|i want to|i'?d like to|i need to) (?:first )?(?:discuss|talk (?:it |this )?(?:through|over)|plan|go over|tweak|refine|fine[- ]?tune)\b|\b(?:discuss|plan|talk (?:it|this) (?:through|over)) (?:it |this )?first\b|\bdon'?t (?:run|start|do) (?:anything|it|this) (?:yet|until)\b`)

var goAheadRe = regexp.MustCompile(`(?i)^\s*(?:ok(?:ay)?[, ]+)?(?:yes[, ]+)?(?:go ahead|go for it|run it|start it|do it|looks good,? (?:go|run|start)|that works,? (?:go|run|start)|let'?s go|proceed)\b[.! ]*$`)

// DiscussHint is added to a discussion message so the manager proposes instead of doing.
const DiscussHint = "\n\n(Talk it through with me first: propose a short plan, list your assumptions, and wait for me to say \"go ahead\" before doing anything.)"

// DiscussFirst reports whether the owner asked to talk a request through before it runs.
func DiscussFirst(message string) bool { return discussFirstRe.MatchString(message) }

// GoAhead reports whether the message is just the owner's go-ahead.
func GoAhead(message string) bool { return goAheadRe.MatchString(message) }

// DiscussedObjective builds the objective from a discussion: the owner's own turns from the one that started
// the discussion onward. It returns "" when the history holds no discussion, so a stray "go ahead" does nothing.
// roles and texts are parallel; the assistant's turns are context, not instructions, and are left out.
func DiscussedObjective(roles, texts []string) string {
	start := -1
	for i := len(texts) - 1; i >= 0; i-- {
		if roles[i] == "user" && DiscussFirst(texts[i]) {
			start = i
			break
		}
	}
	if start < 0 {
		return ""
	}
	var parts []string
	for i := start; i < len(texts); i++ {
		if roles[i] != "user" || GoAhead(texts[i]) {
			continue
		}
		parts = append(parts, strings.TrimSpace(strings.TrimSuffix(texts[i], DiscussHint)))
	}
	return strings.Join(parts, "\n")
}

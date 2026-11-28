package agent

import (
	"fmt"
	"regexp"
	"strings"
)

// Violation is one part of a reply the run does not support.
type Violation struct {
	Sentence string
	Reason   string
}

var (
	urlRe   = regexp.MustCompile(`https?://[^\s<>"')\]]+`)
	uuidRe  = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
	moneyRe = regexp.MustCompile(`(?i)(?:₹|rs\.?|inr|\$|€|£)\s?\d[\d,]*(?:\.\d+)?(?:\s?(?:k|lakhs?|lpa|cr))?|\b\d[\d,]*(?:\.\d+)?\s?(?:lpa|lakhs?|rupees)\b`)

	monthNames = "Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec"
	isoDateRe  = regexp.MustCompile(`\b(\d{4})-(\d{2})-(\d{2})\b`)
	dayMonthRe = regexp.MustCompile(`(?i)\b(\d{1,2})(?:st|nd|rd|th)?\s+(` + monthNames + `)[a-z]*\b`)
	monthDayRe = regexp.MustCompile(`\b(` + monthNames + `)[a-z]*\.?\s+(\d{1,2})(?:st|nd|rd|th)?\b`)
	monthNum   = map[string]string{"jan": "01", "feb": "02", "mar": "03", "apr": "04", "may": "05", "jun": "06", "jul": "07", "aug": "08", "sep": "09", "oct": "10", "nov": "11", "dec": "12"}

	negationRe = regexp.MustCompile(`(?i)\b(can't|cannot|can not|couldn't|could not|didn't|did not|haven't|have not|hasn't|unable|not able|wasn't|won't|will|would|shall|should|want me|if you|let me know|need to|needs to|nothing)\b|'ll\b`)
	claimRe    = regexp.MustCompile(`(?i)\b(done|paused|resumed|removed|cancel+ed|sent|set|scheduled|retried|requeued|queued|deleted|approved|rejected|updated|edited|saved|remembered|forgot|forgotten|started|emailed|messaged|called|posted|submitted|created|added|changed|marked|closed)\b`)
	markerRe   = regexp.MustCompile(`(?i)(^\W*(done|okay|ok|all set)\b|\bi\b|\bi've\b|\bhas been\b|\bhave been\b|\bwas\b|\bwere\b|\bis now\b|\bare now\b)`)
)

// claimGroups maps a claim verb to the tools whose action can justify it ("*" is any action).
var claimGroups = []struct {
	verb  *regexp.Regexp
	tools []string
}{
	{regexp.MustCompile(`(?i)\bpaused\b`), []string{"pause"}},
	{regexp.MustCompile(`(?i)\bresumed\b`), []string{"resume", "retry"}},
	{regexp.MustCompile(`(?i)\b(removed|cancel+ed|deleted|closed)\b`), []string{"cancel", "remove", "delete", "close", "forget"}},
	{regexp.MustCompile(`(?i)\b(set|scheduled)\b`), []string{"remind", "pause", "edit", "schedule", "run_routine", "update"}},
	{regexp.MustCompile(`(?i)\b(retried|requeued)\b`), []string{"retry", "resume", "run_routine"}},
	{regexp.MustCompile(`(?i)\bqueued\b`), []string{"*"}},
	{regexp.MustCompile(`(?i)\b(approved|rejected)\b`), []string{"decide"}},
	{regexp.MustCompile(`(?i)\b(updated|edited|changed|marked)\b`), []string{"edit", "update", "set", "mark", "pause", "resume"}},
	{regexp.MustCompile(`(?i)\b(remembered|saved|forgot|forgotten)\b`), []string{"remember", "forget", "save"}},
	{regexp.MustCompile(`(?i)\bstarted\b`), []string{"start_job", "run_routine", "retry"}},
	{regexp.MustCompile(`(?i)\b(sent|emailed|messaged|called|posted|submitted)\b`), []string{"@external"}},
	{regexp.MustCompile(`(?i)\b(done|created|added)\b`), []string{"*"}},
}

func justified(tools []string, actions []Action) bool {
	for _, a := range actions {
		for _, t := range tools {
			switch {
			case t == "*":
				return true
			case t == "@external":
				if a.Effect == EffectExternal && !a.Pending {
					return true
				}
			case strings.Contains(a.Tool, t):
				if a.Pending && a.Effect == EffectExternal {
					continue // nothing has happened yet
				}
				return true
			}
		}
	}
	return false
}

// splitSentences cuts text at ., ! or ? followed by space or the end, and at line breaks, keeping the text intact.
func splitSentences(text string) []string {
	var out []string
	r := []rune(text)
	start := 0
	for i := 0; i < len(r); i++ {
		end := false
		switch r[i] {
		case '\n':
			end = true
		case '.', '!', '?':
			j := i
			for j+1 < len(r) && (r[j+1] == '.' || r[j+1] == '!' || r[j+1] == '?') {
				j++
			}
			if j+1 >= len(r) || r[j+1] == ' ' || r[j+1] == '\n' {
				i = j
				end = true
			}
		}
		if end {
			out = append(out, string(r[start:i+1]))
			start = i + 1
		}
	}
	if start < len(r) {
		out = append(out, string(r[start:]))
	}
	return out
}

func normKey(s string) string {
	s = strings.ToLower(strings.TrimRight(strings.TrimSpace(s), ".,;:!?)"))
	return strings.TrimRight(s, "/")
}

func moneyKey(s string) string {
	s = strings.ToLower(s)
	for _, p := range []string{"₹", "rs.", "rs", "inr", "$", "€", "£", ",", " "} {
		s = strings.ReplaceAll(s, p, "")
	}
	s = strings.ReplaceAll(s, "lakhs", "lakh")
	return s
}

func dateKeys(s string) map[string]bool {
	keys := map[string]bool{}
	for _, m := range isoDateRe.FindAllStringSubmatch(s, -1) {
		keys[m[2]+"-"+m[3]] = true
	}
	for _, m := range dayMonthRe.FindAllStringSubmatch(s, -1) {
		keys[monthNum[strings.ToLower(m[2])]+"-"+fmt.Sprintf("%02s", m[1])] = true
	}
	for _, m := range monthDayRe.FindAllStringSubmatch(s, -1) {
		keys[monthNum[strings.ToLower(m[1])]+"-"+fmt.Sprintf("%02s", m[2])] = true
	}
	return keys
}

func keySet(re *regexp.Regexp, s string, f func(string) string) map[string]bool {
	out := map[string]bool{}
	for _, m := range re.FindAllString(s, -1) {
		out[f(m)] = true
	}
	return out
}

// Check finds the parts of a reply that this turn's actions and evidence do not support: a sentence claiming an
// action no tool did, and a URL, id, amount or date that appears nowhere the model could have read it.
func Check(reply string, actions []Action, evidence string) []Violation {
	var out []Violation
	urls := keySet(urlRe, evidence, normKey)
	uuids := keySet(uuidRe, evidence, strings.ToLower)
	money := keySet(moneyRe, evidence, moneyKey)
	dates := dateKeys(evidence)
	for _, sent := range splitSentences(reply) {
		s := strings.TrimSpace(sent)
		if s == "" {
			continue
		}
		bad := func(reason string) { out = append(out, Violation{Sentence: s, Reason: reason}) }
		for _, u := range urlRe.FindAllString(s, -1) {
			if !urls[normKey(u)] {
				bad("the link " + u + " is not in any tool result or the owner's message")
				break
			}
		}
		for _, u := range uuidRe.FindAllString(s, -1) {
			if !uuids[strings.ToLower(u)] {
				bad("the id " + u + " is not in any tool result")
				break
			}
		}
		for _, m := range moneyRe.FindAllString(s, -1) {
			if !money[moneyKey(m)] {
				bad("the amount " + strings.TrimSpace(m) + " is not in any tool result or the owner's message")
				break
			}
		}
		for k := range dateKeys(s) {
			if !dates[k] {
				bad("a date in this sentence is not in any tool result, the owner's message or the context")
				break
			}
		}
		if strings.Contains(s, "?") || negationRe.MatchString(s) || !claimRe.MatchString(s) || !markerRe.MatchString(s) {
			continue
		}
		for _, g := range claimGroups {
			if g.verb.MatchString(s) && !justified(g.tools, actions) {
				bad("it says an action was done, but no tool call did it this turn")
				break
			}
		}
	}
	return out
}

// DropUnsupported removes the violating sentences and adds a note when anything was removed.
func DropUnsupported(reply string, vs []Violation) string {
	bad := map[string]bool{}
	for _, v := range vs {
		bad[v.Sentence] = true
	}
	var kept []string
	for _, s := range splitSentences(reply) {
		if !bad[strings.TrimSpace(s)] {
			kept = append(kept, s)
		}
	}
	out := strings.TrimSpace(strings.Join(kept, ""))
	note := "I couldn't verify part of that, so I left it out."
	if out == "" {
		return note
	}
	return out + "\n\n(" + note + ")"
}

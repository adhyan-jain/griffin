package domain

import (
	"regexp"
	"sort"
	"strings"
)

// A playbook is a short, reusable note a tool-using run leaves for itself: which sites, steps and
// gotchas worked, so the next similar task starts from what is already known instead of from scratch.
// It is stored as a Markdown artifact with metadata {"kind":"playbook","key":"<keywords>"}, is visible
// and deletable in Files, and is only ever produced by a tool-tier run (read or act).

const (
	// PlaybookKind marks a Markdown artifact as a playbook.
	PlaybookKind = "playbook"

	maxPlaybookChars = 4000
	maxKeyWords      = 8
)

var (
	playbookHeading = regexp.MustCompile(`(?im)^#{1,3}\s*playbook\s*$`)
	nextHeading     = regexp.MustCompile(`(?m)^#{1,3}\s+\S`)
	wordRe          = regexp.MustCompile(`[a-z0-9]+`)

	// Anything that looks like a credential keeps a playbook from being saved at all.
	secretLike = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(password|passwd|passcode|secret|token|api[_ -]?key|private[_ -]?key|cookie|session[_ -]?id|bearer)\b\s*[:=]`),
		regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._\-]{12,}`),
		regexp.MustCompile(`\b[A-Za-z0-9+/_\-]{40,}={0,2}\b`),
		regexp.MustCompile(`(?i)-----BEGIN [A-Z ]*PRIVATE KEY-----`),
		regexp.MustCompile(`\b(sk|pk|ghp|gho|xox[bap]|AKIA)[A-Za-z0-9_\-]{10,}`),
	}

	stopWords = map[string]bool{
		"the": true, "a": true, "an": true, "and": true, "or": true, "to": true, "of": true, "in": true, "on": true, "for": true,
		"with": true, "my": true, "me": true, "is": true, "are": true, "it": true, "this": true, "that": true, "please": true,
		"give": true, "get": true, "do": true, "i": true, "you": true, "your": true, "at": true, "by": true, "from": true, "all": true,
		"any": true, "can": true, "if": true, "as": true, "be": true, "now": true, "jj": true,
	}
)

// ExtractPlaybook returns the "## Playbook" section of an answer (the text up to the next heading),
// or false when there is none or it is empty.
func ExtractPlaybook(answer string) (string, bool) {
	loc := playbookHeading.FindStringIndex(answer)
	if loc == nil {
		return "", false
	}
	rest := answer[loc[1]:]
	if n := nextHeading.FindStringIndex(rest); n != nil {
		rest = rest[:n[0]]
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return "", false
	}
	r := []rune(rest)
	if len(r) > maxPlaybookChars {
		rest = string(r[:maxPlaybookChars])
	}
	return rest, true
}

// PlaybookLooksSafe is false when the text looks like it contains a credential or key. A playbook that
// fails this check is not saved: remembering "how to log in" must never mean remembering the secret.
func PlaybookLooksSafe(text string) bool {
	for _, re := range secretLike {
		if re.MatchString(text) {
			return false
		}
	}
	return true
}

// PlaybookKey turns a task into a stable, order-independent set of keywords (the longest distinct
// meaningful words, sorted), so "Check LinkedIn for intern roles" and "check linkedin intern roles"
// land on the same key.
func PlaybookKey(title, goal string) string {
	seen := map[string]bool{}
	var words []string
	for _, w := range wordRe.FindAllString(strings.ToLower(title+" "+goal), -1) {
		if len(w) < 3 || stopWords[w] || seen[w] {
			continue
		}
		seen[w] = true
		words = append(words, w)
	}
	sort.Slice(words, func(i, j int) bool {
		if len(words[i]) != len(words[j]) {
			return len(words[i]) > len(words[j])
		}
		return words[i] < words[j]
	})
	if len(words) > maxKeyWords {
		words = words[:maxKeyWords]
	}
	sort.Strings(words)
	return strings.Join(words, " ")
}

// PlaybookSimilarity is the share of keywords two keys have in common (0 to 1, Jaccard).
func PlaybookSimilarity(a, b string) float64 {
	sa, sb := map[string]bool{}, map[string]bool{}
	for _, w := range strings.Fields(a) {
		sa[w] = true
	}
	for _, w := range strings.Fields(b) {
		sb[w] = true
	}
	if len(sa) == 0 || len(sb) == 0 {
		return 0
	}
	inter := 0
	for w := range sa {
		if sb[w] {
			inter++
		}
	}
	return float64(inter) / float64(len(sa)+len(sb)-inter)
}

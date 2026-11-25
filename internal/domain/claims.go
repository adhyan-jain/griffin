package domain

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	// numberWord is a spelled-out count used as a claim ("five years", "ten engineers").
	numberWord = regexp.MustCompile(`(?i)\b(one|two|three|four|five|six|seven|eight|nine|ten|eleven|twelve|fifteen|twenty|thirty|fifty|hundred|thousand|million)[\s-]+(years?|months?|users|customers|clients|engineers|projects|stars|downloads|requests|interns|hackathons|papers)\b`)
	// placeholder is text a person was meant to fill in: a draft carrying one is not finished.
	placeholder = regexp.MustCompile(`\[[^\]\n]{1,60}\]|\{\{[^}]*\}\}|<[A-Za-z][^>\n]{0,40}>|\b(?:lorem ipsum|insert [a-z ]{3,30} here)\b|\bX{2,}\b|\bTBD\b`)
)

// openers are words a sentence may start with without them being a claim: ordinary English and the words of a
// letter. Any other capitalised word that starts a sentence must appear in the facts, like a name in the middle of
// one. A word ending in -ing, -ed or -ly is ordinary English too.
var openers = map[string]bool{
	"a": true, "an": true, "and": true, "as": true, "at": true, "after": true, "also": true, "all": true, "any": true, "about": true,
	"because": true, "before": true, "both": true, "but": true, "by": true,
	"each": true, "even": true, "every": true, "for": true, "from": true, "further": true,
	"he": true, "her": true, "his": true, "how": true, "i": true, "if": true, "in": true, "into": true, "is": true, "it": true, "its": true,
	"i'm": true, "i’m": true, "i've": true, "i’ve": true, "i'd": true, "i’d": true, "i'll": true, "i’ll": true,
	"just": true, "many": true, "most": true, "my": true, "much": true, "more": true, "moreover": true,
	"no": true, "not": true, "now": true, "of": true, "on": true, "one": true, "or": true, "our": true, "other": true, "over": true,
	"please": true, "since": true, "so": true, "some": true, "such": true, "that": true, "the": true, "their": true, "then": true,
	"there": true, "these": true, "they": true, "this": true, "those": true, "through": true, "to": true, "though": true, "together": true,
	"under": true, "until": true, "up": true, "we": true, "what": true, "when": true, "where": true, "which": true, "while": true,
	"who": true, "why": true, "with": true, "within": true, "would": true, "you": true, "your": true, "yours": true,
	"dear": true, "hello": true, "hi": true, "thanks": true, "thank": true, "sincerely": true, "regards": true, "best": true, "warm": true,
	"kind": true, "excited": true, "eager": true, "happy": true, "grateful": true, "keen": true,
	"hiring": true, "manager": true, "team": true, "recruiter": true, "application": true, "role": true, "position": true,
	"internship": true, "intern": true, "summer": true, "interested": true, "ready": true, "available": true,
}

// CheckClaims returns every claim in text that known (the owner's profile, the posting and whatever else the text may
// draw on) does not support: a number or a name the facts never mention, a count spelled out in words, or a
// placeholder that was never filled in. It extends the outreach check (CheckOutreach) to sentence starts: a name at
// the front of a sentence ("Google was where...") is a claim too. Nothing returned means the text may be shown to
// the owner for approval.
func CheckClaims(text, known string) []string {
	var problems []string
	// The words of a salutation are not claims.
	lower := strings.ToLower(known) + " hiring manager recruiter recruiting sincerely"
	for _, m := range placeholder.FindAllString(text, -1) {
		problems = append(problems, fmt.Sprintf("left a placeholder in the text: %q", m))
	}
	digits := numberToken.FindAllString(lower, -1)
	seen := map[string]bool{}
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			problems = append(problems, p)
		}
	}
	for _, num := range numberToken.FindAllString(text, -1) {
		num = strings.TrimRight(num, ".,")
		if num == "" || yearToken.MatchString(num) || containsString(digits, num) {
			continue
		}
		add("claim not in your profile or the posting: \"" + num + "\"")
	}
	for _, m := range numberWord.FindAllString(text, -1) {
		if !strings.Contains(lower, strings.ToLower(m)) {
			add("claim not in your profile or the posting: \"" + m + "\"")
		}
	}
	for _, w := range unknownNames(text, lower) {
		add("claim not in your profile or the posting: \"" + w + "\"")
	}
	for _, w := range unknownOpeners(text, lower) {
		add("claim not in your profile or the posting: \"" + w + "\"")
	}
	return problems
}

// unknownOpeners lists capitalised words that start a sentence, are not ordinary words and are not in the facts.
func unknownOpeners(text, known string) []string {
	var out []string
	seen := map[string]bool{}
	for _, loc := range wordToken.FindAllStringIndex(text, -1) {
		w := strings.TrimRight(text[loc[0]:loc[1]], ".'’-")
		if w == "" || w[0] < 'A' || w[0] > 'Z' {
			continue
		}
		before := strings.TrimRight(text[:loc[0]], " \t")
		if before != "" && !sentenceEnd.MatchString(before) {
			continue // mid-sentence: unknownNames has it
		}
		lw := strings.ToLower(w)
		if openers[lw] || commonWords[lw] || seen[lw] || strings.Contains(known, lw) {
			continue
		}
		if strings.HasSuffix(lw, "ing") || strings.HasSuffix(lw, "ed") || strings.HasSuffix(lw, "ly") {
			continue
		}
		if full, ok := shortForms[lw]; ok && strings.Contains(known, full) {
			continue
		}
		seen[lw] = true
		out = append(out, w)
	}
	return out
}

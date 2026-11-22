package domain

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// OutreachContact is one person Griffin proposes to message, with the message itself. Every field comes from the
// person's opened profile; the message is checked by CheckOutreach before anyone is asked to approve it.
type OutreachContact struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	Company string `json:"company"`
	// Club is the person's VIT club as shown on their profile ("ACM VIT"), or empty.
	Club string `json:"club"`
	// Link is the profile the person was verified on (LinkedIn), or a mailto: address.
	Link string `json:"link"`
	// Startup marks a company small enough that its founders are the right people to write to.
	Startup bool   `json:"startup"`
	Message string `json:"message"`
}

// MaxOutreachWords is the longest message Griffin will propose: short enough to read on a phone in one go.
const MaxOutreachWords = 80

var (
	seniorTitle = regexp.MustCompile(`(?i)\b(co-?founder|founder|ceo|cto|coo|cfo|chief|vp|vice[- ]president|director|head of|professor|principal|partner|president)\b`)
	clearAsk    = regexp.MustCompile(`(?i)\b(refer|referral|referring|chat|call|advice|open to|forward|connect|consider)\b`)
	acmWord     = regexp.MustCompile(`\bACM\b`)
	numberToken = regexp.MustCompile(`\d[\d,.]*\+?%?`)
	yearToken   = regexp.MustCompile(`^20[2-3]\d$`)
	wordToken   = regexp.MustCompile(`[A-Za-z][A-Za-z0-9.'’+-]*`)
	sentenceEnd = regexp.MustCompile(`[.!?:;,\n]\s*$`)
)

// Capitalised words any message may use without them being a claim.
var commonWords = map[string]bool{
	"i": true, "i'm": true, "i’m": true, "i've": true, "i’ve": true, "i'd": true, "i’d": true, "i'll": true,
	"hi": true, "hello": true, "hey": true, "dear": true, "thanks": true, "thank": true, "best": true, "regards": true, "warm": true, "cheers": true,
	"happy": true, "would": true, "could": true, "if": true, "no": true, "linkedin": true, "github": true,
	"software": true, "engineer": true, "engineering": true, "intern": true, "internship": true, "developer": true, "backend": true,
	"frontend": true, "full-stack": true, "data": true, "scientist": true, "research": true, "ml": true, "ai": true, "ai/ml": true,
	"sde": true, "sde-1": true, "role": true, "summer": true, "team": true, "resume": true, "cv": true,
}

// IsSeniorTitle reports whether a job title belongs to a founder, executive, director, professor or principal:
// people the owner writes to only at a startup.
func IsSeniorTitle(title string) bool { return seniorTitle.MatchString(title) }

// CheckOutreach returns every reason the message must not be proposed; none means it may go to the owner for
// approval. ownerFacts is the owner's verified profile: every number and name in the message must come from it,
// from the contact's own details, or be an ordinary word.
func CheckOutreach(c OutreachContact, ownerFacts string) []string {
	var problems []string
	if strings.TrimSpace(c.Name) == "" || strings.TrimSpace(c.Link) == "" {
		problems = append(problems, "no verified profile link for this person")
	}
	if IsSeniorTitle(c.Title) && !c.Startup {
		problems = append(problems, fmt.Sprintf("%s is senior (%s) at a company that is not a startup: message an engineer there instead", c.Name, c.Title))
	}
	msg := strings.TrimSpace(c.Message)
	if msg == "" {
		return append(problems, "the message is empty")
	}
	if acmWord.MatchString(msg) && !strings.Contains(strings.ToUpper(c.Club), "ACM") {
		problems = append(problems, "mentions ACM to someone who is not from ACM VIT")
	}
	if n := len(strings.Fields(msg)); n > MaxOutreachWords {
		problems = append(problems, fmt.Sprintf("too long: %d words (at most %d)", n, MaxOutreachWords))
	}
	if !clearAsk.MatchString(msg) {
		problems = append(problems, "no clear ask (a referral, a quick chat or advice)")
	}
	known := strings.ToLower(ownerFacts + " " + c.Name + " " + c.Title + " " + c.Company + " " + c.Club)
	knownDigits := numberToken.FindAllString(known, -1)
	for _, num := range numberToken.FindAllString(msg, -1) {
		num = strings.TrimRight(num, ".,")
		if yearToken.MatchString(num) || containsString(knownDigits, num) {
			continue
		}
		problems = append(problems, "claim not in your profile: \""+num+"\"")
	}
	for _, w := range unknownNames(msg, known) {
		problems = append(problems, "claim not in your profile: \""+w+"\"")
	}
	return problems
}

// unknownNames lists capitalised words that are not at the start of a sentence, not ordinary words, and not found
// in what Griffin knows: those are names or products the message would be asserting without evidence.
func unknownNames(msg, known string) []string {
	var out []string
	seen := map[string]bool{}
	locs := wordToken.FindAllStringIndex(msg, -1)
	for _, loc := range locs {
		w := strings.TrimRight(msg[loc[0]:loc[1]], ".'’-")
		if w == "" || w[0] < 'A' || w[0] > 'Z' {
			continue
		}
		before := strings.TrimRight(msg[:loc[0]], " \t")
		if before == "" || sentenceEnd.MatchString(before) {
			continue // a sentence or line starts with a capital anyway
		}
		lw := strings.ToLower(w)
		if commonWords[lw] || strings.Contains(known, lw) || seen[lw] {
			continue
		}
		seen[lw] = true
		out = append(out, w)
	}
	return out
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if strings.TrimRight(v, ".,") == s {
			return true
		}
	}
	return false
}

// OutreachSendGoal is what an approved outreach task does, written by code around the checked message, so the
// model that sends it can neither change the words nor pick someone else.
func OutreachSendGoal(c OutreachContact, resume string) string {
	var b strings.Builder
	b.WriteString("Open " + c.Link + " in the owner's signed-in browser and send this message to " + c.Name + ", word for word. ")
	if resume != "" {
		b.WriteString("If you can message them directly, attach the file " + resume + " with the attach button. ")
	}
	b.WriteString("If only a connection request is possible, send one with the first two sentences of the message as the note (at most 280 characters) and attach nothing. ")
	b.WriteString("Send nothing else, change nothing, and message nobody else. ")
	b.WriteString("Finish with two lines: DM: sent | requested | blocked, then PROOF: what the page showed, or the exact reason it is blocked. ")
	b.WriteString("Limits: about 12 tool calls, under 4 minutes.\n\nThe message:\n" + c.Message)
	return b.String()
}

// AlumniMessageFor is the owner's short note to one VIT alum. ACM is mentioned only to an ACM VIT alum.
func AlumniMessageFor(firstName, company, focus string, acm bool) string {
	who := "I'm Adhyan, a pre-final-year CSE student at VIT"
	if acm {
		who += " and a senior core member of ACM VIT, like you were"
	}
	return "Hi " + firstName + ", " + who + ". I saw you're at " + company + ", and I'm looking for a summer 2027 " + focus + " internship. " +
		"I built ComputeHive, a distributed task runner in Go with Redis and Docker, and interned as an AI Engineer at Palni. " +
		"Would you be open to a quick chat or a referral? Happy to share my resume.\n\nThanks,\nAdhyan"
}

// IsOutreachTask reports whether a task proposes messages to people (a referral search or the alumni batch):
// every proposal it makes must then carry a contact and pass CheckOutreach.
func IsOutreachTask(t *Task) bool {
	if t == nil {
		return false
	}
	if IsAlumniDMBatch(t.Title) {
		return true
	}
	var p struct {
		ReferralsFor json.RawMessage `json:"referrals_for"`
	}
	return json.Unmarshal(t.InputPayload, &p) == nil && len(p.ReferralsFor) > 0 && string(p.ReferralsFor) != "null"
}

// IsResumeFile reports whether path is one of the owner's current resumes.
func IsResumeFile(path string) bool {
	for _, f := range ResumeFiles {
		if f == path {
			return true
		}
	}
	return false
}

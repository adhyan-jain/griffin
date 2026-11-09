package service

import (
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/griffin/core/internal/domain"
)

// Deciding between "this continues something I asked before" and "this is something new". A wrong guess either
// way is a small annoyance (a stray task, or an answer filed in the wrong thread), so the decision is made from
// signals that can be explained to the owner, who can always override it:
//
//	+ words that only make sense after something ("also", "now", "make it shorter", "send it")
//	+ pronouns with nothing new to refer to ("it", "that", "the second one")
//	+ the message and the earlier thread talking about the same things
//	+ the earlier thread finished recently
//	- wording that starts something fresh ("research", "what is", "new task")
//
// At or above continueAt the message joins the thread; between maybeAt and continueAt it starts a new task and
// offers to continue instead; below that it is simply new.

// ThreadAction is the outcome.
type ThreadAction string

const (
	ThreadNew      ThreadAction = "new"
	ThreadMaybe    ThreadAction = "maybe" // new task, with the option to continue offered
	ThreadContinue ThreadAction = "continue"
)

const (
	continueAt     = 0.60
	maybeAt        = 0.35
	maxThreadAge   = 24 * time.Hour
	maxAnswerChars = 1500
)

// ThreadCandidate is a finished thread the message could continue.
type ThreadCandidate struct {
	Root     *domain.Task
	Latest   *domain.Task // the newest finished task in the thread; the one a continuation would follow
	Answer   string       // its readable answer
	Finished time.Time
}

// ThreadDecision says what to do and why, in words the owner can read.
type ThreadDecision struct {
	Action    ThreadAction
	Candidate *ThreadCandidate
	Score     float64
	Reasons   []string
}

var (
	reNewStart     = regexp.MustCompile(`(?i)\b(new task|new topic|new question|start (a )?new|start over|unrelated|different (question|topic|task)|separate task|forget (that|it|the above))\b`)
	reContinueLead = regexp.MustCompile(`(?i)^(?:(?:ok|okay|and|so|well|hey|please|yeah|yes|great|cool|nice|thanks|thank you)[\s,.!-]+)*(also|then|now|next|and then|additionally|plus|one more|another one|continue|keep going|go on|more|again|retry|redo|try again|instead|actually|what about|how about|but)\b`)
	reEditVerb     = regexp.MustCompile(`(?i)^(?:(?:ok|okay|and|so|please|can you|could you|would you|just)[\s,]+)*(make|change|rewrite|shorten|lengthen|expand|trim|fix|polish|reword|tweak|translate|summari[sz]e|add|remove|delete|replace|rename|move|swap|update|edit|combine|merge|split|turn|convert|send|post|forward|save|download|copy|do)\b`)
	rePronoun      = regexp.MustCompile(`(?i)\b(it|that|this|these|those|them|they|its|the above|the previous|the last one|the first one|the second one|the third one|the draft|the answer|the result|the list|the table|the letter|the plan)\b`)
	// Things a piece of writing has. Editing one of them implies the piece already exists.
	reArtifact   = regexp.MustCompile(`(?i)\b(paragraph|sentence|intro|introduction|opening|ending|conclusion|tone|title|heading|subject line|section|line|bullet|version|format|length|wording|tense|style|formatting|summary|draft)\b`)
	reFreshStart = regexp.MustCompile(`(?i)^(?:(?:ok|okay|hey|please|can you|could you|would you|i want you to|i need you to|i would like you to)[\s,]+)*(research|find|compare|create|build|plan|prepare|look up|search|investigate|analy[sz]e|summari[sz]e (the )?(article|page|report|paper|video)|what is|what's|who is|who's|when|where|how many|how much|how do|how does|why|tell me about|explain|give me a|write me a|draft a new|show me)\b`)
)

var stop = map[string]bool{"the": true, "and": true, "for": true, "you": true, "your": true, "with": true, "that": true, "this": true, "was": true, "are": true,
	"but": true, "not": true, "can": true, "has": true, "have": true, "had": true, "his": true, "her": true, "its": true, "our": true, "out": true, "all": true,
	"any": true, "from": true, "into": true, "than": true, "then": true, "them": true, "they": true, "will": true, "would": true, "could": true, "should": true,
	"about": true, "what": true, "which": true, "who": true, "how": true, "why": true, "when": true, "where": true, "also": true, "just": true, "like": true,
	"make": true, "please": true, "some": true, "more": true, "very": true, "one": true, "now": true, "get": true, "got": true, "let": true, "need": true, "want": true}

// contentWords are the words that carry a topic: lower-case, three letters or more, not filler.
func contentWords(s string) map[string]bool {
	out := map[string]bool{}
	var cur []rune
	flush := func() {
		if len(cur) >= 3 {
			if w := string(cur); !stop[w] {
				out[w] = true
			}
		}
		cur = cur[:0]
	}
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur = append(cur, r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

func wordCount(s string) int { return len(strings.Fields(s)) }

// DecideThread scores each candidate and returns the best one's decision.
func DecideThread(message string, now time.Time, cands []ThreadCandidate) ThreadDecision {
	msg := strings.TrimSpace(message)
	if msg == "" || len(cands) == 0 {
		return ThreadDecision{Action: ThreadNew}
	}
	if reNewStart.MatchString(msg) {
		return ThreadDecision{Action: ThreadNew, Score: -1, Reasons: []string{"you asked for something new"}}
	}
	explicit := reContinueLead.MatchString(msg)
	edit := reEditVerb.MatchString(msg) && (rePronoun.MatchString(msg) || reArtifact.MatchString(msg))
	short := wordCount(msg) <= 12
	anaphora := short && rePronoun.MatchString(msg)
	fresh := reFreshStart.MatchString(msg)
	words := contentWords(msg)

	best := ThreadDecision{Action: ThreadNew, Score: -2}
	for i := range cands {
		c := &cands[i]
		age := now.Sub(c.Finished)
		if age > maxThreadAge || age < -time.Minute {
			continue
		}
		var reasons []string
		score := 0.0
		if explicit {
			score += 0.55
			reasons = append(reasons, "it reads like a continuation")
		}
		if edit {
			score += 0.40
			reasons = append(reasons, "it changes or acts on something that already exists")
		}
		if anaphora && !explicit && !edit {
			score += 0.25
			reasons = append(reasons, "it refers back with a word like \"it\" or \"that\"")
		}
		if len(words) > 0 {
			thread := contentWords(c.Root.Goal + " " + c.Latest.Goal + " " + clipRunes(c.Answer, maxAnswerChars))
			shared := 0
			for w := range words {
				if thread[w] {
					shared++
				}
			}
			if shared > 0 {
				cover := float64(shared) / float64(len(words))
				score += 0.6 * cover
				reasons = append(reasons, "it is about the same things")
			}
		}
		switch {
		case age <= 15*time.Minute:
			score += 0.20
			reasons = append(reasons, "the earlier task just finished")
		case age <= 2*time.Hour:
			score += 0.10
		}
		if fresh && !explicit && !edit {
			score -= 0.40
			reasons = append(reasons, "it starts with a fresh request")
		}
		if score > best.Score {
			best = ThreadDecision{Candidate: c, Score: score, Reasons: reasons}
		}
	}
	switch {
	case best.Candidate == nil:
		return ThreadDecision{Action: ThreadNew}
	case best.Score >= continueAt:
		best.Action = ThreadContinue
	case best.Score >= maybeAt:
		best.Action = ThreadMaybe
	default:
		return ThreadDecision{Action: ThreadNew, Score: best.Score}
	}
	return best
}

func clipRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/provider/gemini"
)

// SpokenCase says what kind of summary was made, so the app can word its notification.
type SpokenCase string

const (
	CaseAnswer   SpokenCase = "answer"   // a short answer, read as it is
	CaseSummary  SpokenCase = "summary"  // a longer result, boiled down
	CaseEmpty    SpokenCase = "empty"    // finished with no text (files only)
	CaseFailed   SpokenCase = "failed"   // did not finish
	CaseApproval SpokenCase = "approval" // waiting for the owner's yes
	CaseRunning  SpokenCase = "running"  // asked about before it finished
)

// SpokenSummary is a few seconds of speech about a task.
type SpokenSummary struct {
	Text string     `json:"text"`
	Case SpokenCase `json:"case"`
}

// Summarizer turns a task into a short thing to say. It prefers the fast model for long results, and
// falls back to the answer's own first sentences, so a notification is never held up by a model.
type Summarizer struct {
	Brain PlanBrain // may be nil
}

const (
	shortAnswerRunes = 220
	spokenMaxRunes   = 260
)

var (
	reFence  = regexp.MustCompile("(?s)```.*?```")
	reImage  = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	reLink   = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	reURL    = regexp.MustCompile(`https?://\S+`)
	reMarks  = regexp.MustCompile("(?m)^\\s{0,3}(#{1,6}\\s+|[-*+]\\s+|\\d+[.)]\\s+|>\\s?)")
	reEmph   = regexp.MustCompile("[*_`~]+")
	reSpaces = regexp.MustCompile(`\s+`)
)

// plainText strips Markdown so it can be spoken: no symbols read aloud, no web addresses.
func plainText(md string) string {
	s := reFence.ReplaceAllString(md, " ")
	s = reImage.ReplaceAllString(s, " ")
	s = reLink.ReplaceAllString(s, "$1")
	s = reURL.ReplaceAllString(s, " ")
	s = reMarks.ReplaceAllString(s, "")
	s = reEmph.ReplaceAllString(s, "")
	return strings.TrimSpace(reSpaces.ReplaceAllString(s, " "))
}

// firstSentences takes whole sentences up to max runes (always at least the first, cut if it is very long).
func firstSentences(s string, max int) string {
	var out []rune
	var cur []rune
	flush := func() bool {
		if len(cur) == 0 {
			return true
		}
		if len(out) > 0 && len(out)+len(cur)+1 > max {
			return false
		}
		if len(out) > 0 {
			out = append(out, ' ')
		}
		out = append(out, cur...)
		cur = nil
		return true
	}
	for _, r := range s {
		cur = append(cur, r)
		if r == '.' || r == '!' || r == '?' {
			if !flush() {
				break
			}
		}
	}
	flush()
	if len(out) > max {
		out = append(out[:max-1], '…')
	}
	return strings.TrimSpace(string(out))
}

const summarySystem = `You write what a phone says aloud when a task finishes. Reply with ONLY the spoken text: at most 35 words, plain sentences, no lists, no symbols, no web addresses.
Lead with the answer or the decision, not with "the task". For a comparison or research, give the recommendation or the main finding and how many options or sources. For writing, say what was written and roughly how long. Use only facts in the text; never add any.`

// Spoken makes the summary for a task. name is the manager's name, used for the approval case.
func (s *Summarizer) Spoken(ctx context.Context, t *domain.Task, name string) SpokenSummary {
	title := strings.TrimSpace(t.Title)
	if title == "" {
		title = strings.TrimSpace(t.Goal)
	}
	title = firstSentences(plainText(title), 80)
	switch t.Status {
	case domain.TaskStatusFailed, domain.TaskStatusCancelled:
		word := "did not finish"
		if t.Status == domain.TaskStatusCancelled {
			word = "was cancelled"
		}
		reason := ""
		if t.FailureReason != nil {
			reason = firstSentences(plainText(*t.FailureReason), 110)
		}
		text := fmt.Sprintf("%s %s.", title, word)
		if reason != "" {
			text = fmt.Sprintf("%s %s: %s", title, word, strings.TrimRight(reason, ".")+".")
		}
		return SpokenSummary{Text: clipSpoken(text, spokenMaxRunes) + " Open it to try again.", Case: CaseFailed}
	case domain.TaskStatusAwaitingApproval:
		return SpokenSummary{Text: fmt.Sprintf("%s needs your approval for %s.", name, title), Case: CaseApproval}
	case domain.TaskStatusCompleted:
	default:
		return SpokenSummary{Text: fmt.Sprintf("%s is still working.", title), Case: CaseRunning}
	}

	answer := plainText(domain.ResultText(t.OutputPayload))
	if answer == "" {
		return SpokenSummary{Text: fmt.Sprintf("%s is done. There is no text answer; open it to see the files.", title), Case: CaseEmpty}
	}
	if len([]rune(answer)) <= shortAnswerRunes {
		return SpokenSummary{Text: answer, Case: CaseAnswer}
	}
	if s != nil && s.Brain != nil && s.Brain.Enabled() {
		prompt := fmt.Sprintf("Task: %s\n\nResult:\n%s", title, clipSpoken(answer, 6000))
		if out, err := s.Brain.Chat(ctx, summarySystem, []gemini.Turn{{Role: "user", Text: prompt}}); err == nil {
			if txt := strings.TrimSpace(plainText(out)); txt != "" && len([]rune(txt)) <= spokenMaxRunes*2 {
				return SpokenSummary{Text: clipSpoken(txt, spokenMaxRunes*2), Case: CaseSummary}
			}
		}
	}
	return SpokenSummary{Text: firstSentences(answer, shortAnswerRunes), Case: CaseSummary}
}

func clipSpoken(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimRightFunc(string(r[:n-1]), unicode.IsSpace) + "…"
}

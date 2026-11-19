package domain

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

// BriefReply is a person who answered the owner, with what the reply contains that needs action.
type BriefReply struct {
	Name        string    `json:"name"`
	Handle      string    `json:"handle"`
	RepliedAt   time.Time `json:"replied_at"`
	Snippet     string    `json:"snippet"`
	Links       []string  `json:"links,omitempty"`       // every link in the reply
	Assessments []string  `json:"assessments,omitempty"` // the links that look like a test or assessment
	Deadlines   []string  `json:"deadlines,omitempty"`   // sentences that name a deadline
}

// BriefPhase is either morning briefing or evening debrief.
type BriefPhase string

const (
	BriefPhaseMorning BriefPhase = "morning"
	BriefPhaseEvening BriefPhase = "evening"
)

// BriefSource is everything the brief is built from. The caller fills it from the database.
type BriefSource struct {
	Now              time.Time
	Phase            BriefPhase
	PendingApprovals []string // one line each
	Replies          []BriefReply
	FollowUpsDue     []string
	FailedTasks      []string
	DsaToday         []*DsaItem
	CompletedToday   []string // completed tasks today (for evening debrief)
	NewOpportunities []string // newly staged opportunities or scraper yields
}

// Brief is the operator summary (morning agenda or evening debrief).
type Brief struct {
	Date  string     `json:"date"`
	Phase BriefPhase `json:"phase,omitempty"`
	Plan  []string   `json:"plan"`
	Text  string     `json:"text"`
	Empty bool       `json:"empty"`
}

var (
	urlRe      = regexp.MustCompile(`https?://[^\s<>"')\]]+`)
	deadlineRe = regexp.MustCompile(`(?i)\b(deadline|due (on|by|date)|by (end of )?(today|tonight|tomorrow|monday|tuesday|wednesday|thursday|friday|saturday|sunday|eod|\d{1,2}(st|nd|rd|th)?\s+(jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec))|within \d+\s*(hours?|hrs?|days?)|expires?|valid (till|until|for)|last date)\b`)
	assessWord = regexp.MustCompile(`(?i)\b(assessment|coding (test|challenge|round)|online (test|assessment)|hackerrank|codility|codesignal|hirevue|testgorilla|mettl|coderpad|take-?home)\b`)
	assessHost = []string{"hackerrank.com", "codility.com", "codesignal.com", "hirevue.com", "testgorilla.com", "mettl.com", "coderpad.io", "hackerearth.com", "app.codility.com", "calendly.com"}
)

// ExtractLinks returns the links in text, trailing punctuation removed, without repeats.
func ExtractLinks(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range urlRe.FindAllString(text, -1) {
		m = strings.TrimRight(m, ".,;:!?")
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}

// IsAssessmentLink reports whether a link is a known test platform, or sits in a sentence that talks about an assessment.
func IsAssessmentLink(link, sentence string) bool {
	if u, err := url.Parse(link); err == nil {
		host := strings.ToLower(u.Hostname())
		for _, h := range assessHost {
			if host == h || strings.HasSuffix(host, "."+h) {
				return true
			}
		}
	}
	return assessWord.MatchString(sentence)
}

var sentenceSplit = regexp.MustCompile(`[.!?]+(\s+|$)|\n+`)

// AnalyzeReply fills a BriefReply's links, assessment links and deadlines from the reply text.
func AnalyzeReply(r BriefReply, text string) BriefReply {
	text = strings.TrimSpace(text)
	r.Snippet = clipRunes(strings.Join(strings.Fields(text), " "), 160)
	r.Links = ExtractLinks(text)
	seen := map[string]bool{}
	for _, s := range sentenceSplit.Split(text, -1) {
		if strings.TrimSpace(s) == "" {
			continue
		}
		for _, l := range ExtractLinks(s) {
			if IsAssessmentLink(l, s) && !seen[l] {
				seen[l] = true
				r.Assessments = append(r.Assessments, l)
			}
		}
		if deadlineRe.MatchString(s) {
			r.Deadlines = append(r.Deadlines, clipRunes(strings.Join(strings.Fields(s), " "), 140))
		}
	}
	return r
}

func clipRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// BuildBrief orders what needs the owner today. Replies with an assessment or a deadline come first, then
// approvals (nothing moves until they are decided), then follow-ups, then DSA, then failures to look at.
func BuildBrief(src BriefSource) Brief {
	b := Brief{Date: src.Now.Format("Mon 2 Jan 2006"), Phase: src.Phase}
	if b.Phase == "" {
		b.Phase = BriefPhaseMorning
	}

	if src.Phase == BriefPhaseEvening {
		for _, c := range src.CompletedToday {
			b.Plan = append(b.Plan, "Completed today: "+c)
		}
		for _, a := range src.PendingApprovals {
			b.Plan = append(b.Plan, "Still awaiting approval: "+a)
		}
		for _, f := range src.FollowUpsDue {
			b.Plan = append(b.Plan, "Follow-up due: "+f)
		}
		for _, o := range src.NewOpportunities {
			b.Plan = append(b.Plan, "Opportunity staged: "+o)
		}
		for _, f := range src.FailedTasks {
			b.Plan = append(b.Plan, "Failed today: "+f)
		}
		b.Empty = len(b.Plan) == 0
		if b.Empty {
			b.Text = "Good evening. All loops are closed for today."
			return b
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "Good evening. %s: %d item(s) in today's debrief.\n", b.Date, len(b.Plan))
		for i, p := range b.Plan {
			fmt.Fprintf(&sb, "%d. %s\n", i+1, p)
		}
		b.Text = strings.TrimRight(sb.String(), "\n")
		return b
	}

	replies := append([]BriefReply(nil), src.Replies...)
	urgency := func(r BriefReply) int {
		switch {
		case len(r.Assessments) > 0:
			return 0
		case len(r.Deadlines) > 0:
			return 1
		}
		return 2
	}
	sort.SliceStable(replies, func(i, j int) bool { return urgency(replies[i]) < urgency(replies[j]) })
	for _, r := range replies {
		who := r.Name
		if who == "" {
			who = r.Handle
		}
		line := "Reply from " + who + ": " + r.Snippet
		for _, a := range r.Assessments {
			line += "\n   assessment: " + a
		}
		for _, d := range r.Deadlines {
			line += "\n   deadline: " + d
		}
		b.Plan = append(b.Plan, line)
	}
	for _, o := range src.NewOpportunities {
		b.Plan = append(b.Plan, "New opportunity: "+o)
	}
	for _, a := range src.PendingApprovals {
		b.Plan = append(b.Plan, "Approve or refuse: "+a)
	}
	for _, f := range src.FollowUpsDue {
		b.Plan = append(b.Plan, "Follow-up due: "+f)
	}
	for _, it := range src.DsaToday {
		if it.State == DsaAssigned {
			b.Plan = append(b.Plan, fmt.Sprintf("DSA: %s (%s) %s", it.Title, it.Difficulty, it.URL()))
		}
	}
	for _, f := range src.FailedTasks {
		b.Plan = append(b.Plan, "Failed overnight: "+f)
	}
	b.Empty = len(b.Plan) == 0
	if b.Empty {
		b.Text = "Good morning. Nothing needs you today."
		return b
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Good morning. %s: %d thing(s) for today.\n", b.Date, len(b.Plan))
	for i, p := range b.Plan {
		fmt.Fprintf(&sb, "%d. %s\n", i+1, p)
	}
	b.Text = strings.TrimRight(sb.String(), "\n")
	return b
}

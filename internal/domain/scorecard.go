package domain

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ScoreRow is one finished task, reduced to what the scorecard counts.
type ScoreRow struct {
	Status        TaskStatus
	Output        json.RawMessage
	FailureReason string
	// Approvals is how many times the task stopped to ask the owner; Rejected is how many of those were refused.
	Approvals int
	Rejected  int
}

// Scorecard is the evidence page: how often finished tasks actually succeeded, how often one claimed success
// with nothing to show for it, and how often the owner was asked first.
type Scorecard struct {
	Finished  int `json:"finished"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
	Cancelled int `json:"cancelled"`
	// FalseCompletions are tasks marked COMPLETED whose output is empty: the failure the system must never let through.
	FalseCompletions int `json:"false_completions"`
	// CaughtFalseCompletions are runs that reported success with no result and were stopped and turned into a failure.
	CaughtFalseCompletions int `json:"caught_false_completions"`
	// AskedApproval counts tasks that stopped at least once to ask the owner.
	AskedApproval int `json:"asked_approval"`
	Rejected      int `json:"approvals_rejected"`

	// Rates are fractions in [0,1]. SuccessRate and FalseCompletionRate are over finished tasks that were not
	// cancelled by the owner (a cancel is the owner's choice, not an outcome).
	SuccessRate         float64 `json:"success_rate"`
	FalseCompletionRate float64 `json:"false_completion_rate"`
	ApprovalRate        float64 `json:"approval_rate"`
}

// emptyResultMarker is the text of the guard that turns "success with no result" into a failure.
const emptyResultMarker = "produced no result"

// ComputeScorecard counts rows. Tasks that are still running are not passed in.
func ComputeScorecard(rows []ScoreRow) Scorecard {
	var s Scorecard
	for _, r := range rows {
		s.Finished++
		switch r.Status {
		case TaskStatusCompleted:
			s.Completed++
			if IsEmptyOutput(r.Output) {
				s.FalseCompletions++
			}
		case TaskStatusFailed:
			s.Failed++
			if strings.Contains(r.FailureReason, emptyResultMarker) {
				s.CaughtFalseCompletions++
			}
		case TaskStatusCancelled:
			s.Cancelled++
		}
		if r.Approvals > 0 {
			s.AskedApproval++
		}
		s.Rejected += r.Rejected
	}
	if judged := s.Finished - s.Cancelled; judged > 0 {
		s.SuccessRate = float64(s.Completed-s.FalseCompletions) / float64(judged)
	}
	if s.Completed > 0 {
		s.FalseCompletionRate = float64(s.FalseCompletions) / float64(s.Completed)
	}
	if s.Finished > 0 {
		s.ApprovalRate = float64(s.AskedApproval) / float64(s.Finished)
	}
	return s
}

// ScorecardMarkdown renders the scorecard as a table for a README or a post.
func ScorecardMarkdown(s Scorecard, days int) string {
	pct := func(f float64) string { return fmt.Sprintf("%.1f%%", f*100) }
	return fmt.Sprintf(`## Griffin scorecard (last %d days)

| Measure | Value |
| :--- | :--- |
| Tasks finished | %d |
| Success rate | %s (%d completed, %d failed, %d cancelled) |
| False-completion rate | %s (%d marked done with no result) |
| False completions caught before they counted | %d |
| Asked for approval | %s (%d tasks, %d refused) |
`, days, s.Finished, pct(s.SuccessRate), s.Completed, s.Failed, s.Cancelled,
		pct(s.FalseCompletionRate), s.FalseCompletions, s.CaughtFalseCompletions,
		pct(s.ApprovalRate), s.AskedApproval, s.Rejected)
}

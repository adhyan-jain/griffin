package research

import (
	"fmt"
	"strings"

	"github.com/griffin/core/internal/domain"
)

// stopExplanation says in words why a run stopped.
func stopExplanation(reason string) string {
	switch reason {
	case domain.StopAnswered:
		return "every research question has at least one supported finding"
	case domain.StopExhausted:
		return "no new search queries could be made for the questions that are still open"
	case domain.StopBudgetSearches:
		return "the search budget was used up before the questions were answered"
	case domain.StopBudgetPages:
		return "the page budget was used up before the questions were answered"
	case domain.StopBudgetModelCalls:
		return "the model-call budget was used up before the questions were answered"
	case domain.StopBudgetTime:
		return "the time budget was used up before the questions were answered"
	case domain.StopCancelled:
		return "the run was cancelled"
	case domain.StopFailed:
		return "the run failed"
	}
	return "the run stopped"
}

// oneLine collapses a quote or claim to a single line of bounded length for the report.
func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		s = string(r[:max]) + "..."
	}
	return s
}

// composeReport writes the report. The narrative (the model's analysis, if any) is one section of it; everything
// that tells the reader how far to trust the run is written here, by code.
func (e *Engine) composeReport(run *domain.ResearchRun, narrative, note string) string {
	var b strings.Builder
	answered := 0
	for _, q := range run.Plan.Questions {
		if q.Status == "answered" {
			answered++
		}
	}
	complete := run.StopReason == domain.StopAnswered

	fmt.Fprintf(&b, "# Research report: %s\n\n", oneLine(run.Objective, 300))
	fmt.Fprintf(&b, "**Stop reason:** `%s` - %s.\n\n", run.StopReason, stopExplanation(run.StopReason))
	if complete {
		fmt.Fprintf(&b, "**Coverage:** complete. All %d research questions have a supported finding.\n\n", len(run.Plan.Questions))
	} else {
		fmt.Fprintf(&b, "**Coverage: INCOMPLETE.** %d of %d research questions have a supported finding. Treat this report as partial.\n\n",
			answered, len(run.Plan.Questions))
	}
	if run.ErrorMessage != nil && *run.ErrorMessage != "" {
		fmt.Fprintf(&b, "**Error:** %s\n\n", oneLine(*run.ErrorMessage, 500))
	}

	if strings.TrimSpace(narrative) != "" {
		b.WriteString("## Analysis\n\n_Written by a model from the findings below; check it against them._\n\n")
		b.WriteString(strings.TrimSpace(narrative) + "\n\n")
	} else if note != "" {
		b.WriteString("_" + note + "_\n\n")
	}

	b.WriteString("## Open questions\n\n")
	if open := openQuestions(run); len(open) == 0 {
		b.WriteString("None.\n\n")
	} else {
		for _, q := range open {
			fmt.Fprintf(&b, "- %s\n", oneLine(q.Question, 300))
		}
		b.WriteString("\n")
	}

	conflicts, _ := DetectConflicts(run.Findings)
	b.WriteString("## Conflicting evidence\n\n")
	if len(conflicts) == 0 {
		b.WriteString("None found.\n\n")
	} else {
		b.WriteString("Different sources disagree on these. No value has been chosen.\n\n")
		for _, c := range conflicts {
			fmt.Fprintf(&b, "- **%s / %s**\n", oneLine(c.Subject, 100), oneLine(c.Property, 100))
			for _, v := range c.Values {
				fmt.Fprintf(&b, "  - %s (%s)\n", oneLine(v.Value, 200), v.SourceURL)
			}
		}
		b.WriteString("\n")
	}

	b.WriteString("## Findings\n\n")
	if len(run.Findings) == 0 {
		b.WriteString("None.\n\n")
	}
	for _, f := range topFindings(run.Findings, 100) {
		fmt.Fprintf(&b, "- [%s, %d%%] %s", f.Status, f.Confidence, oneLine(f.Claim, 300))
		if f.Quote != "" {
			fmt.Fprintf(&b, " \"%s\"", oneLine(f.Quote, 300))
		}
		fmt.Fprintf(&b, " (%s)\n", f.SourceURL)
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "## Run\n\n%d searches, %d pages read, %d model calls, %d rounds.\n",
		run.ProgressMetrics["searches_done"], run.ProgressMetrics["pages_inspected"], run.ProgressMetrics["model_calls"], run.Round)
	return b.String()
}

// structuredReport is the report in machine-readable form; model fields are added under their own keys only.
func (e *Engine) structuredReport(run *domain.ResearchRun, fromModel map[string]any) map[string]any {
	s := map[string]any{}
	for k, v := range fromModel {
		s[k] = v
	}
	answered := 0
	for _, q := range run.Plan.Questions {
		if q.Status == "answered" {
			answered++
		}
	}
	counts := map[string]int{}
	for _, f := range run.Findings {
		counts[string(f.Status)]++
	}
	s["objective"] = run.Objective
	s["stop_reason"] = run.StopReason
	s["complete"] = run.StopReason == domain.StopAnswered
	s["questions_total"] = len(run.Plan.Questions)
	s["questions_answered"] = answered
	s["open_questions"] = run.UnresolvedQuestions
	s["finding_counts"] = counts
	s["rounds"] = run.Round
	return s
}

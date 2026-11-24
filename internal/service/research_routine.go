package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/playbooks"
)

// ResearchRunner advances a research run. *research.Engine is the real one; the interface keeps this package free
// of the research package.
type ResearchRunner interface {
	// RunSteps does the next steps of a run until it finishes, is paused or cancelled, or the context's deadline
	// is near, and returns the run as it then stands.
	RunSteps(ctx context.Context, userID, runID uuid.UUID) (*domain.ResearchRun, domain.ResearchAdvance, error)
}

// PlaybookProcessor is the optional second half of a ResearchRunner: it hands a finished run's supported findings
// to the run's playbook. *research.Engine implements it; a runner without it has no playbooks.
type PlaybookProcessor interface {
	ProcessPlaybook(ctx context.Context, run *domain.ResearchRun) (*playbooks.PlaybookResult, error)
}

// ResearchRun runs the routine for a RESEARCH RUN task, in code: it advances the run it names for as long as the
// context's deadline allows (the engine keeps the last seconds for saving), and while the run still has work it
// creates the next task through create, so the work goes on in a later request. Every step is saved as it is
// done, so nothing is lost if a request is cut off. handled is false for any other task.
func (w *TrackerLists) ResearchRun(ctx context.Context, task *domain.Task, create func(context.Context, *domain.Task) error) (string, bool) {
	if w == nil || task == nil {
		return "", false
	}
	runID, ok := domain.ResearchRunIDOf(task.Title)
	if !ok {
		return "", false
	}
	runner := w.ResearchRunner
	if runner == nil {
		return "Research is not set up on this server, so the run was not advanced.", true
	}
	run, adv, err := runner.RunSteps(ctx, task.UserID, runID)
	if errors.Is(err, domain.ErrNotFound) {
		return "This research run no longer exists, so there is nothing to advance.", true
	}
	if err != nil {
		if w.Logger != nil {
			w.Logger.Warn("Research run could not advance", "run_id", runID, "error", err)
		}
		return "The research run could not advance: " + err.Error() + ". Resume it from the research page to try again.", true
	}

	switch {
	case run.Status == domain.PhasePaused:
		return "The research run is paused. It continues when it is resumed.", true
	case run.Status.IsTerminal():
		return summarizeResearch(run) + w.processPlaybook(ctx, runner, run), true
	case adv.Contended:
		return "Another worker is advancing this research run, so this one stopped and left it to it.", true
	}

	// Work remains: the next task carries the run on from the point it has reached.
	next := domain.NewResearchRunTask(run.UserID, run.ID, run.NextStepIndex, run.Revision, run.Objective)
	if create == nil {
		return fmt.Sprintf("Did %d steps of the research run; it has more to do.", adv.Steps), true
	}
	if err := create(ctx, next); err != nil && !errors.Is(err, domain.ErrIdempotentConflict) {
		if w.Logger != nil {
			w.Logger.Warn("Could not schedule the next research task", "run_id", run.ID, "error", err)
		}
		return fmt.Sprintf("Did %d steps of the research run, but the next task could not be created: %v. Resume the run from the research page to carry on.", adv.Steps, err), true
	}
	return fmt.Sprintf("Did %d steps of the research run (%d searches, %d pages, %d model calls so far); the next task carries it on.",
		adv.Steps, run.ProgressMetrics["searches_done"], run.ProgressMetrics["pages_inspected"], run.ProgressMetrics["model_calls"]), true
}

// processPlaybook gives a completed run to its playbook, once, after synthesis. The playbook sees only supported
// findings. A playbook failure is reported in the answer; the run and its findings are already saved.
func (w *TrackerLists) processPlaybook(ctx context.Context, runner ResearchRunner, run *domain.ResearchRun) string {
	pp, ok := runner.(PlaybookProcessor)
	if !ok || run.Status != domain.PhaseCompleted {
		return ""
	}
	res, err := pp.ProcessPlaybook(ctx, run)
	if err != nil {
		if w.Logger != nil {
			w.Logger.Warn("Research playbook failed", "run_id", run.ID, "playbook", run.Playbook, "error", err)
		}
		return " The playbook could not process the findings: " + err.Error() + "."
	}
	if res == nil {
		return ""
	}
	out := " " + res.Summary
	if len(res.Warnings) > 0 {
		out += " Warnings: " + strings.Join(res.Warnings, "; ") + "."
	}
	return out
}

func summarizeResearch(run *domain.ResearchRun) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Research run %s: %d findings, stop reason %s.", strings.ToLower(string(run.Status)), len(run.Findings), run.StopReason)
	if run.StopReason != domain.StopAnswered {
		if len(run.UnresolvedQuestions) > 0 {
			fmt.Fprintf(&b, " Coverage is incomplete: %d questions are still open.", len(run.UnresolvedQuestions))
		} else {
			b.WriteString(" Coverage is incomplete.")
		}
	}
	if run.ErrorMessage != nil && *run.ErrorMessage != "" {
		b.WriteString(" Error: " + *run.ErrorMessage + ".")
	}
	return b.String()
}

package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/storage"
)

// CodeOutcome says what Griffin did with a claimed task without running a model.
type CodeOutcome int

const (
	// NotInCode means the task needs a model or a device: run it as usual.
	NotInCode CodeOutcome = iota
	// Dropped means the task was cancelled (its listing is closed): there is nothing to run.
	Dropped
	// Answered means code did the whole task; finish it with the answer.
	Answered
)

// RunInCode is the one place where a claimed task is handled by plain code instead of a model. Both the cloud
// worker and the host claim route call it, so the rules can never differ between the two.
func (w *TrackerLists) RunInCode(ctx context.Context, task *domain.Task,
	cancel func(context.Context, uuid.UUID, string) error,
	create func(context.Context, *domain.Task) error,
) (string, CodeOutcome) {
	if w == nil || task == nil {
		return "", NotInCode
	}
	if cancel != nil && w.SkipClosedListing(ctx, task, cancel) {
		return "", Dropped
	}
	if answer, ok := w.SyncInternDock(ctx, task, nil); ok {
		return answer, Answered
	}
	if answer, ok := w.AlumniDMBatch(ctx, task); ok {
		return answer, Answered
	}
	if create != nil {
		if answer, ok := w.BatchApply(ctx, task, create); ok {
			return answer, Answered
		}
	}
	return "", NotInCode
}

// AlumniDMBatch proposes the next LinkedIn messages from the Alumni list, in code: who is next and what they are
// sent come from the list and the owner's template, never from a model. The answer carries the proposals, which
// the engine checks against the outreach rules and holds for the owner's approval.
func (w *TrackerLists) AlumniDMBatch(ctx context.Context, task *domain.Task) (string, bool) {
	if w == nil || w.Store == nil || task == nil || !domain.IsAlumniDMBatch(task.Title) {
		return "", false
	}
	list := w.list(ctx, task.UserID, storage.ListKind, "alumni")
	if list == nil {
		return "The Alumni list does not exist yet, so there is nobody to message.", true
	}
	text, err := w.Store.ReadText(ctx, task.UserID, list.ID, trackerReadLimit)
	if err != nil {
		return "", false // a read error is retried by the normal path
	}
	ps := domain.AlumniDMProposals(text, 8)
	if len(ps) == 0 {
		return "Everyone on the Alumni list with a LinkedIn link has been contacted. Nothing to propose today.", true
	}
	names := make([]string, 0, len(ps))
	for _, p := range ps {
		names = append(names, p.Contact.Name+" ("+p.Contact.Company+")")
	}
	return fmt.Sprintf("Proposed %d LinkedIn messages for your approval: %s.\n\n%s", len(ps), strings.Join(names, ", "), domain.FormatProposals(ps)), true
}

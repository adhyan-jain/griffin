package service

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
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
	if cancel != nil && w.SkipDoNotContact(ctx, task, cancel) {
		return "", Dropped
	}
	if answer, ok := HandOverOutreach(task); ok {
		return answer, Answered
	}
	if answer, ok := w.SyncListings(ctx, task); ok {
		return answer, Answered
	}
	if answer, ok := w.RunListingDetails(ctx, task); ok {
		return answer, Answered
	}
	if answer, ok := w.AssessOpportunities(ctx, task); ok {
		return answer, Answered
	}
	if answer, ok := w.ScanOpportunities(ctx, task); ok {
		return answer, Answered
	}
	if answer, ok := w.PrepareApplications(ctx, task); ok {
		return answer, Answered
	}
	if answer, ok := w.HoldPackageTask(task); ok {
		return answer, Answered
	}
	if answer, ok := w.SyncInternDock(ctx, task, nil); ok {
		return answer, Answered
	}
	if answer, ok := w.ScanCommunities(ctx, task, w.CommunityGet, nil); ok {
		return answer, Answered
	}
	if answer, ok := w.SyncHackathons(ctx, task); ok {
		return answer, Answered
	}
	if answer, ok := w.SyncOpenSource(ctx, task); ok {
		return answer, Answered
	}
	if answer, ok := w.SyncHygiene(ctx, task); ok {
		return answer, Answered
	}
	if answer, ok := w.AlumniDMBatch(ctx, task); ok {
		return answer, Answered
	}
	if answer, ok := w.AlumniEvidenceCheck(ctx, task); ok {
		return answer, Answered
	}
	if answer, ok := w.ResearchRun(ctx, task, create); ok {
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
	// The batch starts from the person records: the list is imported first, and anyone marked do-not-contact (by
	// the owner, or by a tracker row that says so) is never proposed. If that cannot be checked, nobody is proposed.
	var skip func(string) bool
	if w.Alumni != nil {
		if _, err := w.Alumni.Import(ctx, task.UserID); err != nil && w.Logger != nil {
			w.Logger.Warn("Alumni import before the batch failed", "error", err)
		}
		var err error
		if skip, err = w.Alumni.DoNotContact(ctx, task.UserID); err != nil {
			return "The do-not-contact list could not be read, so nobody was proposed. It will be tried again.", true
		}
	}
	ps := domain.AlumniDMProposalsExcluding(text, 8, skip)
	if len(ps) == 0 {
		return "Everyone on the Alumni list with a LinkedIn link has been contacted. Nothing to propose today.", true
	}
	names := make([]string, 0, len(ps))
	for _, p := range ps {
		names = append(names, p.Contact.Name+" ("+p.Contact.Company+")")
	}
	return fmt.Sprintf("Proposed %d LinkedIn messages for your approval: %s.\n\n%s", len(ps), strings.Join(names, ", "), domain.FormatProposals(ps)), true
}

// HandOverOutreach answers an approved outreach task in code, so no model or browser sends a LinkedIn message: the
// answer hands the checked message to the owner, and the Alumni list then shows the person as to send by hand. It
// runs after SkipDoNotContact, so a do-not-contact person is cancelled, never handed over.
func HandOverOutreach(task *domain.Task) (string, bool) {
	if !domain.IsOutreachSendTask(task) {
		return "", false
	}
	goal := task.Goal
	if !strings.Contains(goal, domain.ManualSendMarker) {
		goal = domain.ManualSendMarker + " Send it yourself if you still want to. What this task held:\n" + goal
	}
	return "DM: by hand\nPROOF: nothing was sent by Griffin.\n\n" + goal, true
}

// AlumniEvidenceCheck runs the evidence check routine in code: it imports the Alumni list into person records, then
// checks each new Evidence page and Quote (AlumniPeople.CheckEvidence). Rows it has no time for wait for the next run.
func (w *TrackerLists) AlumniEvidenceCheck(ctx context.Context, task *domain.Task) (string, bool) {
	if w == nil || task == nil || !domain.IsAlumniEvidenceCheck(task.Title) {
		return "", false
	}
	if w.Alumni == nil || w.Alumni.Evidence == nil || w.Alumni.Fetch == nil {
		return "The evidence check is not set up on this server, so nothing was checked.", true
	}
	if _, err := w.Alumni.Import(ctx, task.UserID); err != nil && w.Logger != nil {
		w.Logger.Warn("Alumni import before the evidence check failed", "error", err)
	}
	res, err := w.Alumni.CheckEvidence(ctx, task.UserID, 12)
	if err != nil {
		return "", false // retried by the normal path
	}
	if res.Checked == 0 && res.Left == 0 {
		return "No new evidence on the Alumni list to check.", true
	}
	return fmt.Sprintf("Checked %d evidence pages: %d verified, %d quotes not on the page (ignored), %d pages could not be opened (tried again next run). %d left for the next run.",
		res.Checked, res.Verified, res.NotOnPage, res.Failed, res.Left), true
}

var linkedInProfileRe = regexp.MustCompile(`https?://[^\s"'<>]*linkedin\.com/in/[^\s"'<>)]+`)

// SkipDoNotContact cancels an approved alumni LinkedIn message whose recipient was marked do-not-contact (or opted
// out) after the owner approved it, before anything is sent. It fails closed: if the person records cannot be
// read, the message is cancelled with that reason rather than sent unchecked. A failed cancel also holds the
// message unsent: it still returns true, so the message is never sent. It returns true when the task was dropped.
func (w *TrackerLists) SkipDoNotContact(ctx context.Context, task *domain.Task, cancel func(context.Context, uuid.UUID, string) error) bool {
	if w == nil || w.Alumni == nil || task == nil || cancel == nil {
		return false
	}
	if _, _, ok := domain.AlumniDMOf(task.Title); !ok {
		return false
	}
	link := linkedInProfileRe.FindString(task.Goal)
	if link == "" {
		return false
	}
	reason := ""
	if skip, err := w.Alumni.DoNotContact(ctx, task.UserID); err != nil {
		reason = "the do-not-contact list could not be read, so the message was not sent"
	} else if skip(link) {
		reason = "the person is marked do-not-contact or opted out, so the message was not sent"
	} else {
		return false
	}
	if err := cancel(ctx, task.ID, reason); err != nil {
		// Still report it dropped: the claim answers 204 and the task stays leased, so the reaper requeues it and this
		// check runs again. Returning false here would send a message to someone who must not be contacted.
		slog.Warn("could not cancel a do-not-contact message; holding it unsent", "task_id", task.ID, "error", err)
	}
	return true
}

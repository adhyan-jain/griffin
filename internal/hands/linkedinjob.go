package hands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/gateway"
	"github.com/griffin/core/internal/hands/linkedin"
	"github.com/griffin/core/internal/policy"
	"github.com/griffin/core/internal/worker"
)

// LinkedInService runs LinkedIn scans. The host builds it with whatever laptop browser and phone it has.
type LinkedInService struct {
	Scan   linkedin.Scan
	People linkedin.PeopleSearch // the Alumni people search: needs only the laptop browser
}

// linkedInGate asks the permission gate whether a LinkedIn read may run. It returns nil when it may, else the
// result that stops the task: a refusal, or a request for the owner's approval.
func (e *Executor) linkedInGate(ctx context.Context, task *domain.Task, approvalType, summary string) *worker.ExecutionResult {
	action := policy.Action{Capability: domain.HostCapBrowser, App: "linkedin.com", Class: policy.Read}
	intent := policy.Intent{Capability: domain.HostCapBrowser, App: "linkedin.com", Class: policy.Read}
	if err := e.Gate.Require(task.ID.String(), action, intent); err != nil {
		switch {
		case gateway.IsDenied(err):
			var d *gateway.ErrDenied
			errors.As(err, &d)
			res, _ := fail(fmt.Errorf("your permission settings do not allow this: %s", reasons(d.Decision)), false)
			return res
		case gateway.IsNeedsApproval(err):
			if !worker.ApprovedActionsFrom(ctx)[approvalType] {
				return &worker.ExecutionResult{
					ExecutionID: uuid.New(), ProviderName: "hands", Status: "AWAITING_APPROVAL",
					RequiresApproval: &domain.Approval{
						ID: uuid.New(), UserID: task.UserID, TaskID: task.ID, ActionType: approvalType,
						RiskLevel: domain.ApprovalRiskLow, Summary: summary,
					},
				}
			}
		}
	}
	return nil
}

// runLinkedIn is a read: it scrolls and reads, and never likes, comments, applies or messages. It goes
// through the same permission gate as the other hands, so the owner can set LinkedIn to "ask" or "deny".
func (e *Executor) runLinkedIn(ctx context.Context, task *domain.Task, ls *domain.LinkedInScan) (*worker.ExecutionResult, error) {
	if e.Gate == nil {
		return fail(errors.New("permissions are not configured on this host, so nothing can run"), false)
	}
	if e.LinkedIn == nil || e.LinkedIn.Scan.Laptop == nil && e.LinkedIn.Scan.Phone == nil {
		return fail(errors.New("this host cannot scan LinkedIn (needs GEMINI_API_KEY, and a signed-in laptop browser or a phone)"), false)
	}
	if stop := e.linkedInGate(ctx, task, ls.ApprovalActionType(), ls.Summary()); stop != nil {
		return stop, nil
	}
	// The scan's minutes are a hard limit: scrolling gets them, reading the posts gets ten more, and then the
	// browser is stopped. Without this a hung page held the host (and every task behind it) for hours.
	runCtx, cancel := context.WithTimeout(ctx, LinkedInDeadline(ls.Minutes))
	defer cancel()
	answer, err := e.LinkedIn.Scan.Run(runCtx, task.Goal, ls.Minutes, ls.Workspace)
	if err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return fail(fmt.Errorf("the LinkedIn scan ran past its %v limit and was stopped", LinkedInDeadline(ls.Minutes)), false)
		}
		return fail(err, true)
	}
	out, _ := json.Marshal(map[string]string{"response": answer})
	return &worker.ExecutionResult{
		ExecutionID: uuid.New(), ProviderName: "hands/linkedin", Status: "SUCCESS",
		Output: out, TextOutput: answer,
	}, nil
}

// LinkedInDeadline is the longest a LinkedIn scan may run: its scrolling minutes (10 when unset) plus ten for
// reading what it found.
func LinkedInDeadline(minutes int) time.Duration {
	if minutes <= 0 {
		minutes = 10
	}
	return time.Duration(minutes+10) * time.Minute
}

// PeopleDeadline is the longest an Alumni people search may run. It reads at most 6 pages with pauses of 4 to 8
// seconds, so this is generous: a hung page is stopped instead of holding the host.
const PeopleDeadline = 10 * time.Minute

// runLinkedInPeople searches LinkedIn people for VIT alumni, read only, through the same permission gate as a
// scan. It hands back the new people as an Alumni table under "## Tracker", which the server merges into the list.
// A login wall or security check ends it at once with a message for the owner, and it is not retried.
func (e *Executor) runLinkedInPeople(ctx context.Context, task *domain.Task, lp *domain.LinkedInPeople) (*worker.ExecutionResult, error) {
	if e.Gate == nil {
		return fail(errors.New("permissions are not configured on this host, so nothing can run"), false)
	}
	if e.LinkedIn == nil || e.LinkedIn.People.Open == nil {
		return fail(errors.New("this host has no signed-in laptop browser for LinkedIn"), false)
	}
	if stop := e.linkedInGate(ctx, task, lp.ApprovalActionType(), lp.Summary()); stop != nil {
		return stop, nil
	}
	runCtx, cancel := context.WithTimeout(ctx, PeopleDeadline)
	defer cancel()
	answer, err := e.LinkedIn.People.Run(runCtx, lp)
	if err != nil {
		var needsYou *linkedin.NeedsYou
		switch {
		case errors.As(err, &needsYou), errors.Is(err, linkedin.ErrNoPeople):
			return fail(err, false)
		case errors.Is(runCtx.Err(), context.DeadlineExceeded):
			return fail(fmt.Errorf("the LinkedIn people search ran past its %v limit and was stopped", PeopleDeadline), false)
		}
		return fail(err, true)
	}
	out, _ := json.Marshal(map[string]string{"response": answer})
	return &worker.ExecutionResult{
		ExecutionID: uuid.New(), ProviderName: "hands/linkedin", Status: "SUCCESS",
		Output: out, TextOutput: answer,
	}, nil
}

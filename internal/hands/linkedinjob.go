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
	Scan linkedin.Scan
}

// runLinkedIn is a read: it scrolls and reads, and never likes, comments, applies or messages. It goes
// through the same permission gate as the other hands, so the owner can set LinkedIn to "ask" or "deny".
func (e *Executor) runLinkedIn(ctx context.Context, task *domain.Task, ls *domain.LinkedInScan) (*worker.ExecutionResult, error) {
	if e.Gate == nil {
		return fail(errors.New("permissions are not configured on this host, so nothing can run"), false)
	}
	if e.LinkedIn == nil {
		return fail(errors.New("this host cannot scan LinkedIn (needs GEMINI_API_KEY, and a signed-in laptop browser or a phone)"), false)
	}
	action := policy.Action{Capability: domain.HostCapBrowser, App: "linkedin.com", Class: policy.Read}
	intent := policy.Intent{Capability: domain.HostCapBrowser, App: "linkedin.com", Class: policy.Read}
	if err := e.Gate.Require(task.ID.String(), action, intent); err != nil {
		switch {
		case gateway.IsDenied(err):
			var d *gateway.ErrDenied
			errors.As(err, &d)
			return fail(fmt.Errorf("your permission settings do not allow this: %s", reasons(d.Decision)), false)
		case gateway.IsNeedsApproval(err):
			if !worker.ApprovedActionsFrom(ctx)[ls.ApprovalActionType()] {
				return &worker.ExecutionResult{
					ExecutionID: uuid.New(), ProviderName: "hands", Status: "AWAITING_APPROVAL",
					RequiresApproval: &domain.Approval{
						ID: uuid.New(), UserID: task.UserID, TaskID: task.ID, ActionType: ls.ApprovalActionType(),
						RiskLevel: domain.ApprovalRiskLow, Summary: ls.Summary(),
					},
				}, nil
			}
		}
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

package hands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

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
	answer, err := e.LinkedIn.Scan.Run(ctx, task.Goal, ls.Minutes, ls.Workspace)
	if err != nil {
		return fail(err, true)
	}
	out, _ := json.Marshal(map[string]string{"response": answer})
	return &worker.ExecutionResult{
		ExecutionID: uuid.New(), ProviderName: "hands/linkedin", Status: "SUCCESS",
		Output: out, TextOutput: answer,
	}, nil
}

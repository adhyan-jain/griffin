package hands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/gateway"
	"github.com/griffin/core/internal/hands/browser"
	"github.com/griffin/core/internal/policy"
	"github.com/griffin/core/internal/worker"
)

// BrowserService is the host's way of using the owner's logged-in browser for a goal.
type BrowserService struct {
	// Open starts a browser; the returned func closes it. It is called per job so the profile is free between jobs.
	Open    func(ctx context.Context) (browser.Driver, func(), error)
	Decider browser.Decider
	Stage   browser.Stage
}

func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return strings.TrimPrefix(u.Hostname(), "www.")
	}
	return "browser"
}

// runBrowser prepares a browser job and stops before anything is sent; the owner approves the exact script, and
// the next run replays it. Preparing (looking, opening pages, filling fields) is gated as a read; the final step
// is ALWAYS asked about, whatever the permission policy says.
func (e *Executor) runBrowser(ctx context.Context, task *domain.Task, act *domain.BrowserAction) (*worker.ExecutionResult, error) {
	if e.Gate == nil {
		return fail(errors.New("permissions are not configured on this host, so nothing can run"), false)
	}
	if e.Browser == nil {
		return fail(errors.New("this host has no browser set up (run griffin-host browser-login first)"), false)
	}
	app := hostOf(act.StartURL)
	action := policy.Action{Capability: domain.HostCapBrowser, App: app, Class: policy.Read}
	intent := policy.Intent{Capability: domain.HostCapBrowser, App: app, Class: policy.Read}
	if err := e.Gate.Require(task.ID.String(), action, intent); err != nil {
		switch {
		case gateway.IsDenied(err):
			var d *gateway.ErrDenied
			errors.As(err, &d)
			return fail(fmt.Errorf("your permission settings do not allow this: %s", reasons(d.Decision)), false)
		case gateway.IsNeedsApproval(err):
			// Looking around a site the owner has set to "ask" is approved like any other read.
			if !worker.ApprovedActionsFrom(ctx)["host_action:browser:read:"+app] {
				return &worker.ExecutionResult{
					ExecutionID: uuid.New(), ProviderName: "hands", Status: "AWAITING_APPROVAL",
					RequiresApproval: &domain.Approval{
						ID: uuid.New(), UserID: task.UserID, TaskID: task.ID, ActionType: "host_action:browser:read:" + app,
						RiskLevel: domain.ApprovalRiskLow, Summary: "Open " + app + " in your browser: " + act.Summary(),
					},
				}, nil
			}
		}
	}

	// An approved, staged script replays exactly; nothing is decided again.
	if steps, digest, err := e.Browser.Stage.Load(task.ID); err != nil {
		return fail(err, false)
	} else if steps != nil && worker.ApprovedActionsFrom(ctx)[domain.BrowserApprovalType(digest)] {
		drv, closeFn, err := e.Browser.Open(ctx)
		if err != nil {
			return fail(err, true)
		}
		defer closeFn()
		if err := browser.Replay(ctx, drv, steps); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return fail(err, false)
		}
		e.Browser.Stage.Remove(task.ID)
		return webResult("Done. I carried out the steps you approved: "+act.Summary(), map[string]any{"op": "browser", "digest": digest}), nil
	}

	drv, closeFn, err := e.Browser.Open(ctx)
	if err != nil {
		return fail(err, true)
	}
	defer closeFn()
	out, err := browser.Run(ctx, drv, e.Browser.Decider, act)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return fail(err, false)
	}
	if out.Staged == nil {
		return webResult(out.Result, map[string]any{"op": "browser", "response": out.Result}), nil
	}
	digest, err := e.Browser.Stage.Save(task.ID, act.Goal, out.Staged.Steps)
	if err != nil {
		return fail(fmt.Errorf("could not keep the prepared steps: %w", err), true)
	}
	type shown struct {
		Kind, Does, Text string
	}
	var list []shown
	for _, s := range out.Staged.Steps {
		list = append(list, shown{Kind: string(s.Kind), Does: strings.TrimSpace(s.Role + " " + s.Name + s.URL), Text: s.Text})
	}
	payload, _ := json.Marshal(map[string]any{"digest": digest, "goal": act.Goal, "preview": out.Staged.Preview, "steps": list})
	return &worker.ExecutionResult{
		ExecutionID: uuid.New(), ProviderName: "hands", Status: "AWAITING_APPROVAL",
		RequiresApproval: &domain.Approval{
			ID: uuid.New(), UserID: task.UserID, TaskID: task.ID, ActionType: domain.BrowserApprovalType(digest),
			RiskLevel: domain.ApprovalRiskHigh, Summary: act.Summary() + ". Ready to finish: " + lastLine(out.Staged.Preview),
			ActionPayload: payload,
		},
	}, nil
}

func lastLine(s string) string {
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		return s[i+1:]
	}
	return s
}

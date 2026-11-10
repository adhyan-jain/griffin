// Package hands is what a host does for steps that need more than a text answer: it acts on the owner's
// own devices (WhatsApp now; other apps, the browser and files as they are added). Every outward action goes
// through the permission gateway first, and anything the owner has not approved stops and asks.
package hands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/gateway"
	"github.com/griffin/core/internal/hands/whatsapp"
	"github.com/griffin/core/internal/policy"
	"github.com/griffin/core/internal/worker"
)

// Messenger performs a messaging action. whatsapp.Messenger is the real one.
type Messenger interface {
	Do(ctx context.Context, act *domain.MessagingAction) (*whatsapp.Result, error)
}

// Executor runs the steps a host can do. A step with no hands action goes to Agent, the host's text
// executor, so one host handles both kinds of work.
type Executor struct {
	// Agent runs steps that need no hands. It may be nil on a host that only has hands.
	Agent worker.TaskExecutor
	// Gate authorizes outward actions. It is required: without it nothing outward may run.
	Gate *gateway.Gateway
	// WhatsApp is nil when this host has no way to reach WhatsApp.
	WhatsApp Messenger
	// Web is nil when this host does not offer web search and page reading.
	Web    *WebService
	Logger *slog.Logger
}

// Name implements worker.TaskExecutor.
func (e *Executor) Name() string {
	if e.Agent != nil {
		return "hands+" + e.Agent.Name()
	}
	return "hands"
}

// IsHealthy implements worker.TaskExecutor: the hands are healthy when the agent (if any) is.
func (e *Executor) IsHealthy(ctx context.Context) (bool, error) {
	if e.Agent != nil {
		return e.Agent.IsHealthy(ctx)
	}
	return true, nil
}

func fail(err error, retryable bool) (*worker.ExecutionResult, error) {
	return &worker.ExecutionResult{ExecutionID: uuid.New(), ProviderName: "hands", Status: "FAILED", Error: err, Retryable: retryable}, nil
}

// Execute implements worker.TaskExecutor.
func (e *Executor) Execute(ctx context.Context, task *domain.Task) (*worker.ExecutionResult, error) {
	if wa, err := domain.ParseWebAction(task.InputPayload); err != nil {
		return fail(err, false)
	} else if wa != nil {
		return e.runWeb(ctx, task, wa)
	}
	act, derived, err := e.messagingAction(ctx, task)
	if err != nil {
		return fail(err, false)
	}
	if act == nil {
		if e.Agent == nil {
			return fail(errors.New("this host does not run plain text steps"), false)
		}
		return e.Agent.Execute(ctx, task)
	}
	return e.runMessaging(ctx, task, act, derived)
}

// messagingAction returns the step's action. derived is true when it was written by an earlier agent
// step rather than declared in the step's own payload.
func (e *Executor) messagingAction(ctx context.Context, task *domain.Task) (*domain.MessagingAction, bool, error) {
	act, err := domain.ParseMessagingAction(task.InputPayload)
	if err != nil {
		return nil, false, err
	}
	if act != nil {
		return act, false, nil
	}
	if !requiresMessaging(task.InputPayload) {
		return nil, false, nil
	}
	act, err = ActionFromDependencies(worker.DependencyContextFrom(ctx))
	if err != nil {
		return nil, false, err
	}
	if act == nil {
		return nil, false, errors.New("this step needs a messaging action but the earlier step did not produce a usable one")
	}
	return act, true, nil
}

func requiresMessaging(payload json.RawMessage) bool {
	var v struct {
		Requires []string `json:"requires"`
	}
	if json.Unmarshal(payload, &v) != nil {
		return false
	}
	for _, r := range v.Requires {
		if strings.EqualFold(r, domain.HostCapMessaging) {
			return true
		}
	}
	return false
}

// intentContact is who the owner's own request named, when the plan recorded it. An action an agent
// composed may go to this contact without a fresh ask (if the policy says auto); to anyone else it asks.
func intentContact(payload json.RawMessage) string {
	var v struct {
		IntentContact string `json:"intent_contact"`
	}
	_ = json.Unmarshal(payload, &v)
	return strings.TrimSpace(v.IntentContact)
}

func (e *Executor) runMessaging(ctx context.Context, task *domain.Task, act *domain.MessagingAction, derived bool) (*worker.ExecutionResult, error) {
	log := e.Logger
	if log == nil {
		log = slog.Default()
	}
	if e.Gate == nil {
		return fail(errors.New("permissions are not configured on this host, so nothing outward can run"), false)
	}
	if e.WhatsApp == nil {
		return fail(errors.New("this host has no way to reach WhatsApp"), false)
	}

	class := policy.Read
	if act.Outward() {
		class = policy.Send
	}
	target := ""
	if act.Op != domain.MessagingUnread {
		target = act.Target()
	}
	app := act.Channel
	if app == "" {
		app = "whatsapp"
	}
	action := policy.Action{Capability: domain.HostCapMessaging, App: app, Contact: target, Class: class}
	intent := policy.Intent{Capability: domain.HostCapMessaging, App: app, Class: class, Contact: target}
	if derived {
		// Written by an agent from text: only the contact the owner named themselves counts as intended.
		intent.Contact = intentContact(task.InputPayload)
	}

	if err := e.Gate.Require(task.ID.String(), action, intent); err != nil {
		switch {
		case gateway.IsDenied(err):
			var d *gateway.ErrDenied
			errors.As(err, &d)
			return fail(fmt.Errorf("your permission settings do not allow this: %s", reasons(d.Decision)), false)
		case gateway.IsNeedsApproval(err):
			if !worker.ApprovedActionsFrom(ctx)[act.ApprovalActionType()] {
				var n *gateway.ErrNeedsApproval
				errors.As(err, &n)
				return e.askOwner(task, act, n.Decision), nil
			}
			log.Info("owner has approved this exact action", "task_id", task.ID)
		}
	}

	res, err := e.WhatsApp.Do(ctx, act)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return fail(explain(err), false)
	}
	return success(act, res), nil
}

func reasons(d policy.Decision) string {
	if len(d.Reasons) == 0 {
		return "refused"
	}
	return strings.Join(d.Reasons, "; ")
}

// askOwner pauses the step with an approval bound to this exact action.
func (e *Executor) askOwner(task *domain.Task, act *domain.MessagingAction, d policy.Decision) *worker.ExecutionResult {
	risk := domain.ApprovalRiskMedium
	if act.Outward() {
		risk = domain.ApprovalRiskHigh
	}
	payload, _ := json.Marshal(map[string]any{
		"digest": act.Digest(), "channel": act.Channel, "op": act.Op, "contact": act.Target(),
		"text": act.Text, "prefer": act.Prefer, "why_asked": d.Reasons,
	})
	return &worker.ExecutionResult{
		ExecutionID: uuid.New(), ProviderName: "hands", Status: "AWAITING_APPROVAL",
		RequiresApproval: &domain.Approval{
			ID: uuid.New(), UserID: task.UserID, TaskID: task.ID,
			ActionType: act.ApprovalActionType(), RiskLevel: risk, Summary: act.Summary(), ActionPayload: payload,
		},
	}
}

// explain turns a driver error into what the owner should do about it.
func explain(err error) error {
	switch {
	case errors.Is(err, whatsapp.ErrUnconfirmed):
		return fmt.Errorf("%w. It may or may not have been delivered, so Griffin will not send it again: check the chat before retrying", err)
	case errors.Is(err, whatsapp.ErrNotReady):
		return fmt.Errorf("could not reach WhatsApp on any of your devices: %w", err)
	}
	return err
}

func success(act *domain.MessagingAction, res *whatsapp.Result) *worker.ExecutionResult {
	summary := summarize(act, res)
	out, _ := json.Marshal(map[string]any{
		"response": summary, "channel": act.Channel, "op": act.Op, "contact": act.Target(),
		"via": res.Via, "sent": res.Sent, "messages": res.Messages, "chats": res.Chats, "notes": res.Notes,
	})
	return &worker.ExecutionResult{
		ExecutionID: uuid.New(), ProviderName: "hands/" + res.Via, Status: "SUCCESS", Output: out, TextOutput: summary,
	}
}

func summarize(act *domain.MessagingAction, res *whatsapp.Result) string {
	var b strings.Builder
	switch act.Op {
	case domain.MessagingSend:
		fmt.Fprintf(&b, "Sent your WhatsApp message to %s (via %s):\n\n%s", act.Target(), res.Via, act.Text)
	case domain.MessagingReadRecent:
		if len(res.Messages) == 0 {
			fmt.Fprintf(&b, "No messages found in the chat with %s.", act.Target())
			break
		}
		fmt.Fprintf(&b, "Last %d messages with %s (via %s):\n", len(res.Messages), act.Target(), res.Via)
		for _, m := range res.Messages {
			who := act.Target()
			if m.From == "me" {
				who = "You"
			}
			fmt.Fprintf(&b, "\n%s: %s", who, m.Text)
		}
	case domain.MessagingUnread:
		if len(res.Chats) == 0 {
			b.WriteString("No unread WhatsApp chats.")
			break
		}
		fmt.Fprintf(&b, "%d chats with unread messages (via %s):\n", len(res.Chats), res.Via)
		for _, c := range res.Chats {
			fmt.Fprintf(&b, "\n%s (%d unread)", c.Name, c.Unread)
			if c.Preview != "" {
				fmt.Fprintf(&b, ": %s", c.Preview)
			}
		}
	}
	for _, n := range res.Notes {
		fmt.Fprintf(&b, "\n\nNote: %s.", strings.TrimRight(n, "."))
	}
	return b.String()
}

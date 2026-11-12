package service

import (
	"context"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/notify"
)

// TaskNotifier tells the owner's phone when a task finishes, with a few spoken-length words about it. Tapping
// the notification opens a page that reads the same summary aloud.
type TaskNotifier struct {
	Sender notify.Sender
	Summ   *Summarizer
	// AppURL is the web app's address (no trailing slash), for the tap link.
	AppURL string
	// ManagerName resolves the owner's chosen name; nil means the default.
	ManagerName func(ctx context.Context, userID uuid.UUID) string
	Logger      *slog.Logger
}

// TaskFinished implements FinishNotifier.
func (n *TaskNotifier) TaskFinished(ctx context.Context, t *domain.Task) {
	if n == nil || n.Sender == nil {
		return
	}
	name := domain.DefaultManagerName
	if n.ManagerName != nil {
		if v := n.ManagerName(ctx, t.UserID); v != "" {
			name = v
		}
	}
	sum := n.Summ.Spoken(ctx, t, name)
	msg := notify.Message{
		Body:     sum.Text,
		ClickURL: strings.TrimRight(n.AppURL, "/") + "/listen/" + t.ID.String(),
	}
	switch sum.Case {
	case CaseFailed:
		msg.Title, msg.Priority, msg.Tags = name+": needs attention", 4, []string{"warning"}
	case CaseApproval:
		msg.Title, msg.Priority, msg.Tags = name+": waiting for you", 4, []string{"raised_hand"}
	default:
		msg.Title, msg.Tags = name+": done", []string{"white_check_mark"}
	}
	if err := n.Sender.Send(ctx, msg); err != nil {
		log := n.Logger
		if log == nil {
			log = slog.Default()
		}
		log.Warn("could not send the finish notification", "task_id", t.ID, "error", err)
	}
}

// ApprovalRequested implements ApprovalNotifier: a short push that opens the approvals page, where the owner
// can read, edit and decide on the draft.
func (n *TaskNotifier) ApprovalRequested(ctx context.Context, a *domain.Approval) {
	if n == nil || n.Sender == nil || a == nil {
		return
	}
	name := domain.DefaultManagerName
	if n.ManagerName != nil {
		if v := n.ManagerName(ctx, a.UserID); v != "" {
			name = v
		}
	}
	err := n.Sender.Send(ctx, notify.Message{
		Title:    name + ": waiting for you",
		Body:     a.Summary,
		ClickURL: strings.TrimRight(n.AppURL, "/") + "/approvals",
		Priority: 4,
		Tags:     []string{"raised_hand"},
	})
	if err != nil {
		log := n.Logger
		if log == nil {
			log = slog.Default()
		}
		log.Warn("could not send the approval notification", "approval_id", a.ID, "error", err)
	}
}

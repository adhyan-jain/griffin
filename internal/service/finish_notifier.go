package service

import (
	"context"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/notify"
)

// NotificationOutbox records each event once (see repository.NotificationRepository).
type NotificationOutbox interface {
	Record(ctx context.Context, n *domain.Notification) (bool, error)
}

// TaskNotifier tells the owner's phone when a task finishes, with a few spoken-length words about it. Tapping
// the notification opens the task (or its tracker); a "Listen" action reads the summary aloud.
//
// With an Outbox, every event is written once and pushed only the first time: a task reported finished twice,
// or an approval requested again, never makes a second notification.
type TaskNotifier struct {
	Sender notify.Sender
	Summ   *Summarizer
	// Outbox, when set, makes notifications exactly-once and lets the phone fetch them.
	Outbox NotificationOutbox
	// AppURL is the web app's address (no trailing slash), for the tap link.
	AppURL string
	// ManagerName resolves the owner's chosen name; nil means the default.
	ManagerName func(ctx context.Context, userID uuid.UUID) string
	Logger      *slog.Logger
}

// TaskFinished implements FinishNotifier.
func (n *TaskNotifier) TaskFinished(ctx context.Context, t *domain.Task) {
	if n == nil || n.Sender == nil || t == nil {
		return
	}
	// A cancelled task is the owner's own decision or Griffin dropping dead work: not news.
	if t.Status == domain.TaskStatusCancelled {
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
		Body:      sum.Text,
		ClickURL:  domain.TaskActionURL(n.AppURL, t),
		ListenURL: strings.TrimRight(n.AppURL, "/") + "/listen/" + t.ID.String(),
	}
	priority := domain.NotificationPriorityNormal
	switch sum.Case {
	case CaseFailed:
		msg.Title, msg.Priority, msg.Tags = name+": needs attention", 4, []string{"warning"}
		priority = domain.NotificationPriorityHigh
	case CaseApproval:
		msg.Title, msg.Priority, msg.Tags = name+": waiting for you", 4, []string{"raised_hand"}
		priority = domain.NotificationPriorityHigh
	default:
		msg.Title, msg.Tags = name+": done", []string{"white_check_mark"}
	}
	id := t.ID
	if !n.firstTime(ctx, &domain.Notification{
		UserID: t.UserID, Kind: domain.NotificationKindTask, EntityID: &id, DedupeKey: domain.TaskNotificationKey(t.ID, t.Status),
		Title: msg.Title, Body: msg.Body, Priority: priority, ActionURL: msg.ClickURL,
	}) {
		return
	}
	if err := n.Sender.Send(ctx, msg); err != nil {
		n.log().Warn("could not send the finish notification", "task_id", t.ID, "error", err)
	}
}

// firstTime records the event in the outbox and reports whether this is its first report. Without an outbox
// every report counts as the first. When the outbox cannot be written the push still goes out: a missed
// notification is worse than a rare duplicate.
func (n *TaskNotifier) firstTime(ctx context.Context, row *domain.Notification) bool {
	if n.Outbox == nil {
		return true
	}
	created, err := n.Outbox.Record(ctx, row)
	if err != nil {
		n.log().Warn("could not record the notification", "key", row.DedupeKey, "error", err)
		return true
	}
	return created
}

func (n *TaskNotifier) log() *slog.Logger {
	if n.Logger != nil {
		return n.Logger
	}
	return slog.Default()
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
	msg := notify.Message{
		Title:    name + ": waiting for you",
		Body:     a.Summary,
		ClickURL: domain.ApprovalActionURL(n.AppURL, a.ID),
		Priority: 4,
		Tags:     []string{"raised_hand"},
	}
	id := a.ID
	if !n.firstTime(ctx, &domain.Notification{
		UserID: a.UserID, Kind: domain.NotificationKindApproval, EntityID: &id, DedupeKey: domain.ApprovalNotificationKey(a.ID),
		Title: msg.Title, Body: msg.Body, Priority: domain.NotificationPriorityHigh, ActionURL: msg.ClickURL,
	}) {
		return
	}
	if err := n.Sender.Send(ctx, msg); err != nil {
		n.log().Warn("could not send the approval notification", "approval_id", a.ID, "error", err)
	}
}

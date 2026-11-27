package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/notify"
)

// ntfyDelayLimit is how far ahead ntfy can hold a message (3 days); a little less is used to stay inside it.
const ntfyDelayLimit = 71 * time.Hour

// ReminderTasks creates the task that stands for a reminder.
type ReminderTasks interface {
	CreateTask(ctx context.Context, task *domain.Task, dependencyIDs []uuid.UUID) (*domain.Task, error)
}

// ReminderDue finds and completes reminders whose time has come, without a worker.
type ReminderDue interface {
	DueReminders(ctx context.Context, userID uuid.UUID, now time.Time, limit int) ([]*domain.Task, error)
	FinishReminder(ctx context.Context, taskID uuid.UUID, output json.RawMessage) (bool, error)
}

// Reminders makes reminders and delivers them on time. A reminder is a scheduled task plus a push that ntfy holds
// until the time (so it fires with nothing else running). Where there is no ntfy, the phone's own poll of the
// notification outbox delivers it: DeliverDue writes the outbox row at the moment it is due.
type Reminders struct {
	Tasks  ReminderTasks
	Due    ReminderDue
	Sender notify.Sender
	Outbox NotificationOutbox
	AppURL string
	Now    func() time.Time
	Logger *slog.Logger
}

type reminderPayload struct {
	Kind     string `json:"kind"`
	ToolMode string `json:"tool_mode"`
	What     string `json:"what"`
	RemindAt string `json:"remind_at"`
	Pushed   bool   `json:"pushed"`
}

func (r *Reminders) now() time.Time {
	if r != nil && r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Reminders) log() *slog.Logger {
	if r.Logger != nil {
		return r.Logger
	}
	return slog.Default()
}

// Create stores the reminder and, when it is close enough, hands the push to ntfy to hold until at. A failed push
// is not an error: the task stays, and is delivered when the phone polls or a worker claims it.
func (r *Reminders) Create(ctx context.Context, userID uuid.UUID, what string, at time.Time) (*domain.Task, error) {
	sum := sha256.Sum256([]byte(what))
	key := fmt.Sprintf("reminder:%s:%d:%s", userID, at.Unix(), hex.EncodeToString(sum[:4]))
	id := uuid.New() // known before the task exists, so the push can link to it
	willPush := r.Sender != nil && !at.After(r.now().Add(ntfyDelayLimit))
	build := func(pushed bool) *domain.Task {
		payload, _ := json.Marshal(reminderPayload{Kind: "reminder", ToolMode: "none", What: what, RemindAt: at.Format(time.RFC3339), Pushed: pushed})
		when := at
		return &domain.Task{
			ID: id, UserID: userID, ManagerDomain: "personal_ops", Title: domain.ReminderTitlePrefix + what,
			Goal: "Remind the owner: " + what, InputPayload: payload, Priority: domain.TaskPriorityNormalP2,
			IdempotencyKey: &key, ScheduledFor: &when,
		}
	}
	// The task is made first, so a double-submit (same key) stops here and never pushes twice.
	task, err := r.Tasks.CreateTask(ctx, build(willPush), nil)
	if err != nil || !willPush {
		return task, err
	}
	err = r.Sender.Send(ctx, notify.Message{Title: "Reminder", Body: what, Priority: 4, At: at,
		ClickURL: strings.TrimRight(r.AppURL, "/") + "/tasks/" + task.ID.String(), Tags: []string{"alarm_clock"}})
	if err != nil {
		r.log().Warn("could not schedule the reminder push", "task_id", task.ID, "error", err)
		// The task keeps pushed=false so the poll or a worker delivers it when due.
		if u, ok := r.Tasks.(interface {
			UpdateTask(context.Context, *domain.Task) error
		}); ok {
			task.InputPayload = build(false).InputPayload
			if uerr := u.UpdateTask(ctx, task); uerr != nil {
				r.log().Warn("could not mark the reminder as not pushed", "task_id", task.ID, "error", uerr)
			}
		}
	}
	return task, nil
}

// reminderOf reads a reminder task's text.
func reminderOf(task *domain.Task) (what string, pushed bool) {
	var p reminderPayload
	_ = json.Unmarshal(task.InputPayload, &p)
	what = p.What
	if what == "" {
		what = strings.TrimPrefix(task.Title, domain.ReminderTitlePrefix)
	}
	return what, p.Pushed
}

// Deliver records the reminder's outbox row (once, whoever gets here first) and pushes it when no delayed push
// was already scheduled. It returns the answer for the task.
func (r *Reminders) Deliver(ctx context.Context, task *domain.Task) string {
	what, pushed := reminderOf(task)
	id := task.ID
	row := &domain.Notification{
		UserID: task.UserID, Kind: domain.NotificationKindReminder, EntityID: &id, DedupeKey: domain.ReminderNotificationKey(task.ID),
		Title: "Reminder", Body: what, Priority: domain.NotificationPriorityHigh, ActionURL: strings.TrimRight(r.AppURL, "/") + "/tasks/" + task.ID.String(),
	}
	created := true
	if r.Outbox != nil {
		var err error
		if created, err = r.Outbox.Record(ctx, row); err != nil {
			r.log().Warn("could not record the reminder", "task_id", task.ID, "error", err)
			return "" // try again at the next poll
		}
	}
	if created && !pushed && r.Sender != nil {
		if err := r.Sender.Send(ctx, notify.Message{Title: "Reminder", Body: what, Priority: 4, ClickURL: row.ActionURL, Tags: []string{"alarm_clock"}}); err != nil {
			r.log().Warn("could not send the reminder", "task_id", task.ID, "error", err)
		}
	}
	return "Reminded you: " + what + "."
}

// DeliverDue delivers this owner's reminders whose time has come and completes them. It is called on every
// notification poll, so a reminder arrives on time even when no worker or ntfy topic exists. It is safe under
// concurrent polls: the outbox row is written once, and only one call completes the task.
func (r *Reminders) DeliverDue(ctx context.Context, userID uuid.UUID) {
	if r == nil || r.Due == nil {
		return
	}
	tasks, err := r.Due.DueReminders(ctx, userID, r.now(), 20)
	if err != nil {
		r.log().Warn("could not look for due reminders", "error", err)
		return
	}
	for _, t := range tasks {
		answer := r.Deliver(ctx, t)
		if answer == "" {
			continue
		}
		out, _ := json.Marshal(map[string]string{"answer": answer})
		if _, err := r.Due.FinishReminder(ctx, t.ID, out); err != nil {
			r.log().Warn("could not complete the reminder", "task_id", t.ID, "error", err)
		}
	}
}

// Remind is the claim hook: if a worker claims a reminder first, it is delivered the same way (same outbox key).
func (w *TrackerLists) Remind(ctx context.Context, task *domain.Task) (string, bool) {
	if w == nil || w.Reminders == nil || task == nil || !domain.IsReminder(task.Title) {
		return "", false
	}
	answer := w.Reminders.Deliver(ctx, task)
	if answer == "" {
		return "", false
	}
	return answer, true
}

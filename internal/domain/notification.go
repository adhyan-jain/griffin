package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// Notification is one row of the notification outbox. Every event the owner should hear about is written here
// exactly once (its DedupeKey is unique per owner); the phone and any push service deliver from this table, so
// the same event can never be pushed twice, however often a poller or a retry runs.
type Notification struct {
	ID        uuid.UUID  `json:"id"`
	Seq       int64      `json:"seq"`
	UserID    uuid.UUID  `json:"-"`
	Kind      string     `json:"kind"`
	EntityID  *uuid.UUID `json:"entity_id,omitempty"`
	DedupeKey string     `json:"-"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	Priority  string     `json:"priority"`
	ActionURL string     `json:"action_url"`
	CreatedAt time.Time  `json:"created_at"`
	AckedAt   *time.Time `json:"acked_at,omitempty"`
}

// Notification kinds.
const (
	NotificationKindTask     = "task"
	NotificationKindApproval = "approval"
)

// Notification priorities.
const (
	NotificationPriorityNormal = "NORMAL"
	NotificationPriorityHigh   = "HIGH"
)

// TaskNotificationKey names the event "this task reached this status".
func TaskNotificationKey(taskID uuid.UUID, status TaskStatus) string {
	return "task:" + taskID.String() + ":" + string(status)
}

// ApprovalNotificationKey names the event "this approval is waiting for the owner".
func ApprovalNotificationKey(approvalID uuid.UUID) string {
	return "approval:" + approvalID.String() + ":PENDING"
}

// TaskActionURL is the page a task's notification opens: its tracker when it is a tracker task, otherwise its
// own task page.
func TaskActionURL(appURL string, t *Task) string {
	base := strings.TrimRight(appURL, "/")
	if TrackerTypeOf(t.Title, t.Goal, t.InputPayload) != "" {
		return base + "/trackers?task=" + t.ID.String()
	}
	return base + "/tasks/" + t.ID.String()
}

// ApprovalActionURL is the page an approval's notification opens: that approval.
func ApprovalActionURL(appURL string, approvalID uuid.UUID) string {
	return strings.TrimRight(appURL, "/") + "/approvals?id=" + approvalID.String()
}

package service

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
)

// memOutbox is an in-memory notification outbox with the same unique (owner, dedupe key) rule as the table.
type memOutbox struct {
	mu   sync.Mutex
	rows []*domain.Notification
}

func (m *memOutbox) Record(_ context.Context, n *domain.Notification) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rows {
		if r.UserID == n.UserID && r.DedupeKey == n.DedupeKey {
			return false, nil
		}
	}
	n.ID, n.Seq = uuid.New(), int64(len(m.rows)+1)
	m.rows = append(m.rows, n)
	return true, nil
}

func TestATaskThatFinishesIsAnnouncedOnceHoweverOftenItIsReported(t *testing.T) {
	s, box := &captureSender{}, &memOutbox{}
	n := &TaskNotifier{Sender: s, Outbox: box, AppURL: "https://app.example"}
	task := done("compare laptops", "Compare laptops", "The Acer is the best value.")
	for i := 0; i < 3; i++ {
		n.TaskFinished(context.Background(), task)
	}
	if len(s.msgs) != 1 || len(box.rows) != 1 {
		t.Fatalf("one event must make one push and one row, got %d pushes and %d rows", len(s.msgs), len(box.rows))
	}
	row := box.rows[0]
	if row.Kind != domain.NotificationKindTask || row.EntityID == nil || *row.EntityID != task.ID || row.ActionURL != "https://app.example/tasks/"+task.ID.String() {
		t.Fatalf("the row must name the task and open its page: %+v", row)
	}
	if s.msgs[0].ClickURL != row.ActionURL {
		t.Fatalf("the push must open the same page as the row: %s", s.msgs[0].ClickURL)
	}
}

func TestACancelledTaskIsNotAnnounced(t *testing.T) {
	s, box := &captureSender{}, &memOutbox{}
	n := &TaskNotifier{Sender: s, Outbox: box, AppURL: "https://app.example"}
	task := done("x", "APPLY - Acme - SDE Intern", "")
	task.Status = domain.TaskStatusCancelled
	n.TaskFinished(context.Background(), task)
	if len(s.msgs) != 0 || len(box.rows) != 0 {
		t.Fatalf("Griffin dropping a dead task, or the owner cancelling one, is not news: %d pushes, %d rows", len(s.msgs), len(box.rows))
	}
}

func TestAnApprovalIsAnnouncedOnceAndOpensThatApproval(t *testing.T) {
	s, box := &captureSender{}, &memOutbox{}
	n := &TaskNotifier{Sender: s, Outbox: box, AppURL: "https://app.example/"}
	a := &domain.Approval{ID: uuid.New(), UserID: uuid.New(), Summary: "Send the email to Sam"}
	n.ApprovalRequested(context.Background(), a)
	n.ApprovalRequested(context.Background(), a)
	if len(s.msgs) != 1 || len(box.rows) != 1 {
		t.Fatalf("one approval, one push: %d pushes, %d rows", len(s.msgs), len(box.rows))
	}
	want := "https://app.example/approvals?id=" + a.ID.String()
	if box.rows[0].ActionURL != want || s.msgs[0].ClickURL != want || box.rows[0].Priority != domain.NotificationPriorityHigh {
		t.Fatalf("an approval opens that approval and is high priority: %+v / %s", box.rows[0], s.msgs[0].ClickURL)
	}
	if !strings.Contains(box.rows[0].Body, "Sam") {
		t.Fatalf("the row carries the summary: %q", box.rows[0].Body)
	}
}

func TestWithoutAnOutboxEveryReportStillPushes(t *testing.T) {
	s := &captureSender{}
	n := &TaskNotifier{Sender: s, AppURL: "https://app.example"}
	task := done("q", "Q", "It is 4.")
	n.TaskFinished(context.Background(), task)
	if len(s.msgs) != 1 {
		t.Fatalf("a local setup with no database keeps working: %d", len(s.msgs))
	}
}

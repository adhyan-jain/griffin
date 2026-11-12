package service

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
)

func TestApprovalRequestedPushesTheSummaryAndLinksToApprovals(t *testing.T) {
	s := &captureSender{}
	n := &TaskNotifier{Sender: s, AppURL: "https://app.example/"}
	n.ApprovalRequested(context.Background(), &domain.Approval{ID: uuid.New(), UserID: uuid.New(), Summary: `Send a WhatsApp message to Bro: "hi"`})
	if len(s.msgs) != 1 || s.msgs[0].ClickURL != "https://app.example/approvals" || s.msgs[0].Body == "" {
		t.Fatalf("unexpected push: %+v", s.msgs)
	}
}

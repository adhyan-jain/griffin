package domain

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestScorecardCountsAndRates(t *testing.T) {
	rows := []ScoreRow{
		{Status: TaskStatusCompleted, Output: json.RawMessage(`{"r":"ok"}`)},
		{Status: TaskStatusCompleted, Output: json.RawMessage(`{"r":"ok"}`), Approvals: 1},
		{Status: TaskStatusCompleted, Output: json.RawMessage(`{}`)}, // false completion
		{Status: TaskStatusFailed, FailureReason: "execution reported success but produced no result"},
		{Status: TaskStatusFailed, FailureReason: "timeout", Approvals: 1, Rejected: 1},
		{Status: TaskStatusCancelled},
	}
	s := ComputeScorecard(rows)
	if s.Finished != 6 || s.Completed != 3 || s.Failed != 2 || s.Cancelled != 1 {
		t.Fatalf("counts wrong: %+v", s)
	}
	if s.FalseCompletions != 1 || s.CaughtFalseCompletions != 1 || s.AskedApproval != 2 || s.Rejected != 1 {
		t.Fatalf("quality counts wrong: %+v", s)
	}
	// success = (3 completed - 1 false) / (6 finished - 1 cancelled) = 2/5
	if s.SuccessRate != 0.4 {
		t.Fatalf("success rate %v, want 0.4", s.SuccessRate)
	}
	if got := s.FalseCompletionRate; got < 0.333 || got > 0.334 {
		t.Fatalf("false completion rate %v, want 1/3", got)
	}
	if got := s.ApprovalRate; got < 0.333 || got > 0.334 {
		t.Fatalf("approval rate %v, want 2/6", got)
	}
}

func TestScorecardEmptyHasNoNaN(t *testing.T) {
	s := ComputeScorecard(nil)
	if s.SuccessRate != 0 || s.FalseCompletionRate != 0 || s.ApprovalRate != 0 {
		t.Fatalf("empty scorecard must be all zero: %+v", s)
	}
}

func TestAuditOrderCauseAndTamperDetection(t *testing.T) {
	base := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	done := base.Add(5 * time.Minute)
	decided := base.Add(3 * time.Minute)
	reason := "looks right"
	apID := uuid.New()
	task := &Task{ID: uuid.New(), Goal: "email Priya the resume", Status: TaskStatusCompleted, CreatedAt: base, CompletedAt: &done}
	events := []*TaskEvent{
		{EventType: "HEARTBEAT", Message: "alive", CreatedAt: base.Add(time.Minute)},
		{EventType: "STATUS_CHANGE", Message: "QUEUED -> RUNNING", CreatedAt: base.Add(time.Minute)},
	}
	attempts := []*TaskAttempt{{AttemptNumber: 1, WorkerID: "w1", Status: "COMPLETED", StartedAt: base.Add(2 * time.Minute)}}
	approvals := []*Approval{{ID: apID, ActionType: "SEND_EMAIL", Summary: "to Priya", Status: ApprovalStatusApproved,
		DecisionReason: &reason, DecidedAt: &decided, CreatedAt: base.Add(150 * time.Second)}}

	got := BuildAudit(task, events, attempts, approvals)
	kinds := []string{}
	for _, e := range got {
		kinds = append(kinds, e.Kind)
	}
	want := []string{"instruction", "event", "attempt", "approval_requested", "approval_decided", "result"}
	if len(kinds) != len(want) {
		t.Fatalf("kinds %v, want %v (heartbeat must be dropped)", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("kinds %v, want %v", kinds, want)
		}
	}
	if got[0].Text != "email Priya the resume" || got[0].Actor != "owner" {
		t.Fatalf("first entry must be the owner's instruction: %+v", got[0])
	}
	if got[4].Cause != 3 {
		t.Fatalf("decision must point at its request (seq 3), got cause %d", got[4].Cause)
	}
	if VerifyAudit(got) != -1 {
		t.Fatal("fresh chain must verify")
	}
	got[3].Text = "to someone else" // tamper with the approved action
	if VerifyAudit(got) != 3 {
		t.Fatalf("tampering with entry 3 must be detected at 3, got %d", VerifyAudit(got))
	}
	cut := append([]AuditEntry{}, BuildAudit(task, events, attempts, approvals)...)
	cut = append(cut[:2], cut[3:]...) // remove one entry
	if VerifyAudit(cut) == -1 {
		t.Fatal("removing an entry must be detected")
	}
}

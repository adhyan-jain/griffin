package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/provider/gemini"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/service"
)

type fakeApprovalRepo struct {
	approvals map[uuid.UUID]*domain.Approval
}

func (f *fakeApprovalRepo) GetApprovalByID(_ context.Context, id uuid.UUID) (*domain.Approval, error) {
	if a, ok := f.approvals[id]; ok {
		return a, nil
	}
	return nil, domain.ErrNotFound
}

func (f *fakeApprovalRepo) ListApprovals(_ context.Context, _ uuid.UUID, _ *domain.ApprovalStatus) ([]*domain.Approval, error) {
	var list []*domain.Approval
	for _, a := range f.approvals {
		list = append(list, a)
	}
	return list, nil
}

func (f *fakeApprovalRepo) RespondToApproval(_ context.Context, id, userID uuid.UUID, approved bool, reason string) (*domain.Approval, error) {
	a, ok := f.approvals[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	if approved {
		a.Status = domain.ApprovalStatusApproved
	} else {
		a.Status = domain.ApprovalStatusRejected
	}
	a.DecisionReason = &reason
	return a, nil
}

func (f *fakeApprovalRepo) EditPendingApproval(_ context.Context, id, _ uuid.UUID, text string) (*domain.Approval, error) {
	a, ok := f.approvals[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	a.ActionPayload = json.RawMessage(`{"text":"` + text + `"}`)
	return a, nil
}

func (f *fakeApprovalRepo) UpdateApprovalPayload(_ context.Context, id, _ uuid.UUID, newPayload json.RawMessage, newSummary string) (*domain.Approval, error) {
	a, ok := f.approvals[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	a.ActionPayload = newPayload
	if newSummary != "" {
		a.Summary = newSummary
	}
	return a, nil
}

type fakeTaskRepoForApprovals struct {
	repository.TaskRepository
}

func (f *fakeTaskRepoForApprovals) RecordEvent(context.Context, *domain.TaskEvent) error {
	return nil
}

type mockBrain struct {
	revised string
}

func (m *mockBrain) Enabled() bool { return true }
func (m *mockBrain) Chat(_ context.Context, _ string, _ []gemini.Turn) (string, error) {
	return m.revised, nil
}

func TestReviseApproval_WithBrain(t *testing.T) {
	userID := uuid.New()
	appID := uuid.New()
	taskID := uuid.New()

	initialPayload, _ := json.Marshal(map[string]any{
		"company":    "Sarvam AI",
		"role":       "Applied AI Intern",
		"cover_note": "Original cover pitch detailing research experience.",
	})

	repo := &fakeApprovalRepo{
		approvals: map[uuid.UUID]*domain.Approval{
			appID: {
				ID:            appID,
				UserID:        userID,
				TaskID:        taskID,
				ActionType:    "JOB_APPLY",
				RiskLevel:     domain.ApprovalRiskHigh,
				Summary:       "Submit application to Sarvam AI",
				Status:        domain.ApprovalStatusPending,
				ActionPayload: initialPayload,
			},
		},
	}

	engine := service.NewTaskEngine(&fakeTaskRepoForApprovals{}, repo, nil)
	brain := &mockBrain{revised: "Punchier pitch focusing directly on SGLang docs and bitemporal graphs."}
	handler := NewApprovalHandler(engine, repo, nil).WithBrain(brain)

	body, _ := json.Marshal(map[string]string{
		"instruction": "make it punchier and emphasize SGLang docs",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/approvals/"+appID.String()+"/chat", bytes.NewReader(body))
	req.SetPathValue("id", appID.String())
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	handler.ReviseApproval(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}
	t.Logf("Response body: %s", rec.Body.String())

	var resp struct {
		Success bool            `json:"success"`
		Data    domain.Approval `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	var payload map[string]any
	_ = json.Unmarshal(resp.Data.ActionPayload, &payload)
	if payload["cover_note"] != brain.revised {
		t.Errorf("expected cover_note %q, got %q", brain.revised, payload["cover_note"])
	}
}

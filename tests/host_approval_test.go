package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
)

// messagingTask creates a send step that requires the messaging capability.
func (e *hostEnv) messagingTask(t *testing.T, act domain.MessagingAction) *domain.Task {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"requires": []string{"messaging"}, "messaging": act})
	task, err := e.Engine.CreateTask(context.Background(), &domain.Task{
		UserID: e.userID, Title: "WhatsApp: " + act.Target(), Goal: "send", Priority: domain.TaskPriorityNormalP2, InputPayload: b,
	}, nil)
	if err != nil {
		t.Fatalf("create messaging task: %v", err)
	}
	return task
}

func approvalFor(t *testing.T, e *hostEnv, taskID uuid.UUID) *domain.Approval {
	t.Helper()
	pending := domain.ApprovalStatusPending
	list, err := e.ApprovalRepo.ListApprovals(context.Background(), e.userID, &pending)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range list {
		if a.TaskID == taskID {
			return a
		}
	}
	return nil
}

func TestMessagingStepMustRequireTheCapability(t *testing.T) {
	e := setupHostEnv(t)
	b, _ := json.Marshal(map[string]any{"messaging": map[string]any{"op": "send", "contact": "Mom", "text": "hi"}})
	_, err := e.Engine.CreateTask(context.Background(), &domain.Task{
		UserID: e.userID, Title: "x", Goal: "x", Priority: domain.TaskPriorityNormalP2, InputPayload: b,
	}, nil)
	if err == nil {
		t.Fatal("a messaging action without requires:[messaging] must be refused at creation")
	}
}

// The whole approval round trip: a host pauses a step, the owner approves in the app, the host claims it
// again and is told exactly which action is approved.
func TestHostApprovalRoundTrip(t *testing.T) {
	e := setupHostEnv(t)
	_, h := e.register(t, "laptop", "laptop", "agent", "messaging")
	e.beat(t, h, "agent", "messaging")
	act := domain.MessagingAction{Op: domain.MessagingSend, Contact: "Mom", Text: "running late"}
	if err := act.Validate(); err != nil {
		t.Fatal(err)
	}
	task := e.messagingTask(t, act)

	code, out := e.do(t, "POST", "/api/v1/host/claim", h, nil)
	if code != http.StatusOK || claimedTitle(out) != "WhatsApp: Mom" {
		t.Fatalf("claim: %d %v", code, out)
	}
	if got := unwrap(out)["approved_actions"]; got != nil {
		t.Fatalf("nothing is approved yet, got %v", got)
	}

	path := "/api/v1/host/tasks/" + task.ID.String() + "/approval"
	req := map[string]any{"action_type": act.ApprovalActionType(), "summary": act.Summary(), "risk_level": "HIGH", "payload": map[string]any{"digest": act.Digest()}}
	if code, out := e.do(t, "POST", path, h, req); code != http.StatusOK {
		t.Fatalf("request approval: %d %v", code, out)
	}
	got, _ := e.Engine.GetTask(context.Background(), task.ID)
	if got.Status != domain.TaskStatusAwaitingApproval {
		t.Fatalf("task should wait for the owner, got %s", got.Status)
	}
	app := approvalFor(t, e, task.ID)
	if app == nil || app.ActionType != act.ApprovalActionType() {
		t.Fatalf("pending approval missing or wrong: %+v", app)
	}

	// While waiting, the step is not claimable.
	if code, _ := e.do(t, "POST", "/api/v1/host/claim", h, nil); code != http.StatusNoContent {
		t.Fatalf("a step waiting for approval must not be claimed, got %d", code)
	}

	if code, out := e.do(t, "POST", "/api/v1/approvals/"+app.ID.String()+"/respond", e.auth, map[string]any{"decision": "APPROVE", "reason": "ok"}); code != http.StatusOK {
		t.Fatalf("respond: %d %v", code, out)
	}

	code, out = e.do(t, "POST", "/api/v1/host/claim", h, nil)
	if code != http.StatusOK {
		t.Fatalf("claim after approval: %d %v", code, out)
	}
	approved, _ := unwrap(out)["approved_actions"].([]any)
	if len(approved) != 1 || approved[0] != act.ApprovalActionType() {
		t.Fatalf("claim should list the approved action, got %v", approved)
	}
}

func TestHostCannotRequestApprovalForWorkItDoesNotHold(t *testing.T) {
	e := setupHostEnv(t)
	_, a := e.register(t, "laptop-a", "laptop", "agent", "messaging")
	_, b := e.register(t, "laptop-b", "laptop", "agent", "messaging")
	e.beat(t, a, "agent", "messaging")
	e.beat(t, b, "agent", "messaging")
	act := domain.MessagingAction{Op: domain.MessagingSend, Contact: "Mom", Text: "hi"}
	_ = act.Validate()
	task := e.messagingTask(t, act)
	if code, _ := e.do(t, "POST", "/api/v1/host/claim", a, nil); code != http.StatusOK {
		t.Fatal("a should claim")
	}
	req := map[string]any{"action_type": act.ApprovalActionType(), "summary": act.Summary(), "risk_level": "HIGH"}
	if code, _ := e.do(t, "POST", "/api/v1/host/tasks/"+task.ID.String()+"/approval", b, req); code == http.StatusOK {
		t.Fatal("a host that does not hold the lease must not create an approval")
	}
	if approvalFor(t, e, task.ID) != nil {
		t.Fatal("no approval should exist")
	}
}

func TestHostApprovalRequestIsValidated(t *testing.T) {
	e := setupHostEnv(t)
	_, h := e.register(t, "laptop", "laptop", "agent", "messaging")
	e.beat(t, h, "agent", "messaging")
	act := domain.MessagingAction{Op: domain.MessagingSend, Contact: "Mom", Text: "hi"}
	_ = act.Validate()
	task := e.messagingTask(t, act)
	e.do(t, "POST", "/api/v1/host/claim", h, nil)
	path := "/api/v1/host/tasks/" + task.ID.String() + "/approval"
	// An action type that is not a host action could impersonate the cloud's own approvals (for example
	// the tool_act approval), so it is refused.
	for name, req := range map[string]map[string]any{
		"foreign action type": {"action_type": "tool_act", "summary": "x", "risk_level": "HIGH"},
		"no summary":          {"action_type": act.ApprovalActionType(), "summary": " ", "risk_level": "HIGH"},
	} {
		if code, _ := e.do(t, "POST", path, h, req); code != http.StatusBadRequest {
			t.Fatalf("%s: want 400, got %d", name, code)
		}
	}
	if got, _ := e.Engine.GetTask(context.Background(), task.ID); got.Status != domain.TaskStatusRunning {
		t.Fatalf("a refused request must leave the step running, got %s", got.Status)
	}
}

func TestRejectedHostApprovalCancelsTheStep(t *testing.T) {
	e := setupHostEnv(t)
	_, h := e.register(t, "laptop", "laptop", "agent", "messaging")
	e.beat(t, h, "agent", "messaging")
	act := domain.MessagingAction{Op: domain.MessagingSend, Contact: "Mom", Text: "hi"}
	_ = act.Validate()
	task := e.messagingTask(t, act)
	e.do(t, "POST", "/api/v1/host/claim", h, nil)
	req := map[string]any{"action_type": act.ApprovalActionType(), "summary": act.Summary(), "risk_level": "HIGH"}
	e.do(t, "POST", "/api/v1/host/tasks/"+task.ID.String()+"/approval", h, req)
	app := approvalFor(t, e, task.ID)
	if app == nil {
		t.Fatal("approval missing")
	}
	e.do(t, "POST", "/api/v1/approvals/"+app.ID.String()+"/respond", e.auth, map[string]any{"decision": "REJECT", "reason": "no"})
	if got, _ := e.Engine.GetTask(context.Background(), task.ID); got.Status != domain.TaskStatusCancelled {
		t.Fatalf("a denied send must cancel the step, got %s", got.Status)
	}
	if code, _ := e.do(t, "POST", "/api/v1/host/claim", h, nil); code != http.StatusNoContent {
		t.Fatalf("a cancelled step must not be claimable, got %d", code)
	}
}

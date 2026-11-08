package tests

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/gateway"
	"github.com/griffin/core/internal/hands"
	"github.com/griffin/core/internal/hands/whatsapp"
	"github.com/griffin/core/internal/hostagent"
)

// recordingMessenger stands in for WhatsApp (the drivers are tested on their own); it records what the
// hands would have sent.
type recordingMessenger struct {
	mu   sync.Mutex
	sent []*domain.MessagingAction
}

func (r *recordingMessenger) Do(_ context.Context, a *domain.MessagingAction) (*whatsapp.Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, a)
	return &whatsapp.Result{Via: "whatsapp-web", Sent: a.Outward()}, nil
}

func (r *recordingMessenger) calls() []*domain.MessagingAction {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*domain.MessagingAction(nil), r.sent...)
}

// startHandsHost runs a real hostagent against the real API with the hands executor, the way griffin-host
// wires it: the gateway reads the policy the agent received from the cloud.
func startHandsHost(t *testing.T, e *hostEnv, bearer string, m hands.Messenger) {
	t.Helper()
	token := strings.TrimPrefix(bearer, "Bearer ")
	client := hostagent.NewClient(e.ts.URL, token, e.ts.Client())
	ex := &hands.Executor{WhatsApp: m}
	agent, err := hostagent.New(hostagent.Config{
		Client: client, Executor: ex, Capabilities: []string{"messaging"}, Version: "test",
		PollInterval: 30 * time.Millisecond, PresenceInterval: time.Second, LeaseRenewInterval: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	ex.Gate = gateway.New("laptop", agent, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = agent.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
}

func waitStatus(t *testing.T, e *hostEnv, id uuid.UUID, want domain.TaskStatus) *domain.Task {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	var last domain.TaskStatus
	for time.Now().Before(deadline) {
		got, err := e.Engine.GetTask(context.Background(), id)
		if err == nil {
			last = got.Status
			if got.Status == want {
				return got
			}
		}
		time.Sleep(40 * time.Millisecond)
	}
	t.Fatalf("task never reached %s (last: %s)", want, last)
	return nil
}

// The whole story: ask, wait, approve, send exactly once, report.
func TestWhatsAppSendNeedsApprovalThenSendsExactlyOnce(t *testing.T) {
	e := setupHostEnv(t)
	_, bearer := e.register(t, "laptop", "laptop", "messaging")
	m := &recordingMessenger{}
	startHandsHost(t, e, bearer, m)

	text := "Running late 🙏\nsee you at 6"
	act := domain.MessagingAction{Op: domain.MessagingSend, Contact: "Mom", Text: text}
	if err := act.Validate(); err != nil {
		t.Fatal(err)
	}
	task := e.messagingTask(t, act)

	waitStatus(t, e, task.ID, domain.TaskStatusAwaitingApproval)
	if n := len(m.calls()); n != 0 {
		t.Fatalf("nothing may be sent while waiting for approval, got %d sends", n)
	}
	app := approvalFor(t, e, task.ID)
	if app == nil || !strings.Contains(app.Summary, "Mom") || !strings.Contains(app.Summary, "Running late") {
		t.Fatalf("the owner must see who and what: %+v", app)
	}

	if code, out := e.do(t, "POST", "/api/v1/approvals/"+app.ID.String()+"/respond", e.auth, map[string]any{"decision": "APPROVE", "reason": "ok"}); code != http.StatusOK {
		t.Fatalf("approve: %d %v", code, out)
	}
	done := waitStatus(t, e, task.ID, domain.TaskStatusCompleted)

	calls := m.calls()
	if len(calls) != 1 || calls[0].Text != text || calls[0].Contact != "Mom" {
		t.Fatalf("expected exactly one send with the exact text, got %+v", calls)
	}
	if !strings.Contains(string(done.OutputPayload), "Sent your WhatsApp message to Mom") {
		t.Fatalf("the result should say what happened: %s", done.OutputPayload)
	}
}

func TestDeclinedWhatsAppSendNeverHappens(t *testing.T) {
	e := setupHostEnv(t)
	_, bearer := e.register(t, "laptop", "laptop", "messaging")
	m := &recordingMessenger{}
	startHandsHost(t, e, bearer, m)
	act := domain.MessagingAction{Op: domain.MessagingSend, Contact: "Mom", Text: "hi"}
	_ = act.Validate()
	task := e.messagingTask(t, act)
	waitStatus(t, e, task.ID, domain.TaskStatusAwaitingApproval)
	app := approvalFor(t, e, task.ID)
	e.do(t, "POST", "/api/v1/approvals/"+app.ID.String()+"/respond", e.auth, map[string]any{"decision": "REJECT", "reason": "no"})
	waitStatus(t, e, task.ID, domain.TaskStatusCancelled)
	time.Sleep(300 * time.Millisecond)
	if n := len(m.calls()); n != 0 {
		t.Fatalf("a declined message must never be sent, got %d", n)
	}
}

// With the owner's saved rule "Mom: send automatically", the same task goes straight through.
func TestAnAutoRuleSendsWithoutAnApprovalRoundTrip(t *testing.T) {
	e := setupHostEnv(t)
	_, bearer := e.register(t, "laptop", "laptop", "messaging")
	_, v := policyOf(func() map[string]any { _, o := e.do(t, "GET", "/api/v1/permissions", e.auth, nil); return o }())
	code, out := putPolicy(t, e, e.auth, v, map[string]any{"default": "ask", "rules": []any{
		map[string]any{"class": "read", "mode": "auto"},
		map[string]any{"contact": "mom", "class": "send", "mode": "auto"},
	}})
	if code != http.StatusOK {
		t.Fatalf("save policy: %d %v", code, out)
	}
	m := &recordingMessenger{}
	startHandsHost(t, e, bearer, m)
	act := domain.MessagingAction{Op: domain.MessagingSend, Contact: "Mom", Text: "on my way"}
	_ = act.Validate()
	task := e.messagingTask(t, act)
	waitStatus(t, e, task.ID, domain.TaskStatusCompleted)
	if calls := m.calls(); len(calls) != 1 || calls[0].Text != "on my way" {
		t.Fatalf("want one send, got %+v", calls)
	}
	if app := approvalFor(t, e, task.ID); app != nil {
		t.Fatal("an auto rule must not ask")
	}
}

// A "never" rule fails the step outright; the message is not sent and no approval is offered.
func TestANeverRuleRefusesTheSend(t *testing.T) {
	e := setupHostEnv(t)
	_, bearer := e.register(t, "laptop", "laptop", "messaging")
	_, v := policyOf(func() map[string]any { _, o := e.do(t, "GET", "/api/v1/permissions", e.auth, nil); return o }())
	code, out := putPolicy(t, e, e.auth, v, map[string]any{"default": "ask", "rules": []any{
		map[string]any{"contact": "boss", "class": "send", "mode": "off"},
	}})
	if code != http.StatusOK {
		t.Fatalf("save policy: %d %v", code, out)
	}
	m := &recordingMessenger{}
	startHandsHost(t, e, bearer, m)
	act := domain.MessagingAction{Op: domain.MessagingSend, Contact: "Boss", Text: "hi"}
	_ = act.Validate()
	task := e.messagingTask(t, act)
	failed := waitStatus(t, e, task.ID, domain.TaskStatusFailed)
	if failed.FailureReason == nil || !strings.Contains(*failed.FailureReason, "permission settings") {
		t.Fatalf("the failure should say why: %v", failed.FailureReason)
	}
	if len(m.calls()) != 0 || approvalFor(t, e, task.ID) != nil {
		t.Fatal("no send and no approval request for a refused action")
	}
}

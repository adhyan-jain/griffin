package hostagent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/gateway"
	"github.com/griffin/core/internal/policy"
	"github.com/griffin/core/internal/worker"
)

const testToken = "ghk_test_token_value_that_must_never_leak"

type fakeAPI struct {
	mu        sync.Mutex
	ts        *httptest.Server
	paused    bool
	revoked   bool
	queue     []map[string]any // each is the claim response data
	beats     int
	lastCaps  []string
	completed map[string]json.RawMessage
	failed    map[string]map[string]any
	renewals  int

	polVer     int
	polBody    map[string]any
	polFetches int

	approvals      map[string]map[string]any // task id -> request body
	refuseApproval bool
}

func newFakeAPI(t *testing.T) *fakeAPI {
	f := &fakeAPI{completed: map[string]json.RawMessage{}, failed: map[string]map[string]any{}, approvals: map[string]map[string]any{}}
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, status int, data any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data})
	}
	guard := func(h func(w http.ResponseWriter, r *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			f.mu.Lock()
			revoked := f.revoked
			f.mu.Unlock()
			if revoked || r.Header.Get("Authorization") != "Bearer "+testToken {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			h(w, r)
		}
	}
	mux.HandleFunc("POST /api/v1/host/heartbeat", guard(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Capabilities []string `json:"capabilities"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.beats++
		f.lastCaps = body.Capabilities
		paused := f.paused
		f.mu.Unlock()
		f.mu.Lock()
		pv := f.polVer
		f.mu.Unlock()
		write(w, 200, map[string]any{"paused": paused, "granted": body.Capabilities, "effective_capabilities": body.Capabilities, "live_window_seconds": 45, "policy_version": pv})
	}))
	mux.HandleFunc("GET /api/v1/host/policy", guard(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.polFetches++
		body, ver := f.polBody, f.polVer
		f.mu.Unlock()
		write(w, 200, map[string]any{"policy": body, "version": ver})
	}))
	mux.HandleFunc("POST /api/v1/host/claim", guard(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if len(f.queue) == 0 {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next := f.queue[0]
		f.queue = f.queue[1:]
		write(w, 200, next)
	}))
	mux.HandleFunc("POST /api/v1/host/tasks/{id}/heartbeat", guard(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.renewals++
		f.mu.Unlock()
		write(w, 200, map[string]string{"message": "ok"})
	}))
	mux.HandleFunc("POST /api/v1/host/tasks/{id}/complete", guard(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Output json.RawMessage `json:"output"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.completed[r.PathValue("id")] = body.Output
		f.mu.Unlock()
		write(w, 200, map[string]string{"message": "ok"})
	}))
	mux.HandleFunc("POST /api/v1/host/tasks/{id}/approval", guard(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		refuse := f.refuseApproval
		if !refuse {
			f.approvals[r.PathValue("id")] = body
		}
		f.mu.Unlock()
		if refuse {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		write(w, 200, map[string]string{"message": "Waiting for the owner's approval"})
	}))
	mux.HandleFunc("POST /api/v1/host/tasks/{id}/fail", guard(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.failed[r.PathValue("id")] = body
		f.mu.Unlock()
		write(w, 200, map[string]string{"message": "ok"})
	}))
	f.ts = httptest.NewServer(mux)
	t.Cleanup(f.ts.Close)
	return f
}

func (f *fakeAPI) enqueue(task *domain.Task, deps ...map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d := map[string]any{"task": task}
	if len(deps) > 0 {
		d["dependency_outputs"] = deps
	}
	f.queue = append(f.queue, d)
}

func (f *fakeAPI) completedOutput(id string) (json.RawMessage, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.completed[id]
	return o, ok
}

func (f *fakeAPI) failure(id string) (map[string]any, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.failed[id]
	return o, ok
}

type fnExec struct {
	fn func(ctx context.Context, task *domain.Task) (*worker.ExecutionResult, error)
}

func (e fnExec) Execute(ctx context.Context, t *domain.Task) (*worker.ExecutionResult, error) {
	return e.fn(ctx, t)
}
func (fnExec) Name() string                            { return "fake" }
func (fnExec) IsHealthy(context.Context) (bool, error) { return true, nil }

func newTask(title string) *domain.Task {
	return &domain.Task{ID: uuid.New(), ManagerDomain: "research", Title: title, Goal: "do " + title, InputPayload: json.RawMessage(`{}`)}
}

func startAgent(t *testing.T, f *fakeAPI, exec worker.TaskExecutor, caps ...string) (*Agent, context.CancelFunc, chan error) {
	t.Helper()
	a, err := New(Config{
		Client: NewClient(f.ts.URL, testToken, nil), Executor: exec, Capabilities: caps, Version: "test",
		PresenceInterval: 50 * time.Millisecond, PollInterval: 10 * time.Millisecond, LeaseRenewInterval: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	t.Cleanup(cancel)
	return a, cancel, done
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestAgentAnnouncesItselfAndItsCapabilities(t *testing.T) {
	f := newFakeAPI(t)
	startAgent(t, f, fnExec{fn: func(context.Context, *domain.Task) (*worker.ExecutionResult, error) { return nil, nil }}, "agent", "browser")
	waitFor(t, "two heartbeats", func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.beats >= 2 })
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.Join(f.lastCaps, ",") != "agent,browser" {
		t.Fatalf("host must report its capabilities, got %v", f.lastCaps)
	}
}

func TestAgentRunsAClaimedTaskAndReportsTheResult(t *testing.T) {
	f := newFakeAPI(t)
	task := newTask("summarize")
	f.enqueue(task, map[string]any{"title": "gather", "output": map[string]any{"text": "the facts"}})

	var seenGoal string
	exec := fnExec{fn: func(_ context.Context, tk *domain.Task) (*worker.ExecutionResult, error) {
		seenGoal = tk.Goal
		return &worker.ExecutionResult{Status: "SUCCESS", Output: json.RawMessage(`{"text":"summary"}`)}, nil
	}}
	startAgent(t, f, exec, "agent")
	waitFor(t, "completion", func() bool { _, ok := f.completedOutput(task.ID.String()); return ok })
	out, _ := f.completedOutput(task.ID.String())
	if string(out) != `{"text":"summary"}` {
		t.Fatalf("wrong output reported: %s", out)
	}
	if !strings.Contains(seenGoal, "the facts") || !strings.Contains(seenGoal, "gather") {
		t.Fatalf("the prerequisite's output must reach the agent's prompt, got %q", seenGoal)
	}
}

func TestAgentReportsFailureWithRetryability(t *testing.T) {
	f := newFakeAPI(t)
	task := newTask("flaky")
	f.enqueue(task)
	exec := fnExec{fn: func(context.Context, *domain.Task) (*worker.ExecutionResult, error) {
		return &worker.ExecutionResult{Error: errors.New("rate limited"), Retryable: true}, nil
	}}
	startAgent(t, f, exec, "agent")
	waitFor(t, "failure", func() bool { _, ok := f.failure(task.ID.String()); return ok })
	got, _ := f.failure(task.ID.String())
	if got["retryable"] != true || got["reason"] != "rate limited" {
		t.Fatalf("unexpected failure report: %v", got)
	}
}

func TestAgentNeverCompletesATaskThatProducedNothing(t *testing.T) {
	f := newFakeAPI(t)
	task := newTask("empty")
	f.enqueue(task)
	exec := fnExec{fn: func(context.Context, *domain.Task) (*worker.ExecutionResult, error) {
		return &worker.ExecutionResult{Status: "SUCCESS"}, nil
	}}
	startAgent(t, f, exec, "agent")
	waitFor(t, "failure", func() bool { _, ok := f.failure(task.ID.String()); return ok })
	if _, ok := f.completedOutput(task.ID.String()); ok {
		t.Fatal("a run with no result must not be reported as completed")
	}
}

func (f *fakeAPI) approvalRequest(id string) (map[string]any, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.approvals[id]
	return a, ok
}

// A step that needs the owner's say-so is paused in the cloud with the exact action in the request; it is
// neither completed nor failed.
func TestAgentAsksTheOwnerWhenAStepNeedsApproval(t *testing.T) {
	f := newFakeAPI(t)
	task := newTask("needs approval")
	f.enqueue(task)
	exec := fnExec{fn: func(context.Context, *domain.Task) (*worker.ExecutionResult, error) {
		return &worker.ExecutionResult{RequiresApproval: &domain.Approval{
			ActionType: "host_action:abc", Summary: "Send a WhatsApp message to Mom", RiskLevel: domain.ApprovalRiskHigh,
		}}, nil
	}}
	startAgent(t, f, exec, "agent")
	waitFor(t, "approval request", func() bool { _, ok := f.approvalRequest(task.ID.String()); return ok })
	got, _ := f.approvalRequest(task.ID.String())
	if got["action_type"] != "host_action:abc" || got["summary"] != "Send a WhatsApp message to Mom" || got["risk_level"] != "HIGH" {
		t.Fatalf("approval request carries the wrong action: %v", got)
	}
	time.Sleep(100 * time.Millisecond)
	if _, failed := f.failure(task.ID.String()); failed {
		t.Fatal("a step waiting for approval must not be failed")
	}
	if _, done := f.completedOutput(task.ID.String()); done {
		t.Fatal("a step waiting for approval must not be completed")
	}
}

// If the host cannot ask (the API refuses or is unreachable) the step fails and does not run: an action
// nobody approved never goes ahead.
func TestAgentFailsTheStepWhenItCannotAsk(t *testing.T) {
	f := newFakeAPI(t)
	f.refuseApproval = true
	task := newTask("needs approval")
	f.enqueue(task)
	exec := fnExec{fn: func(context.Context, *domain.Task) (*worker.ExecutionResult, error) {
		return &worker.ExecutionResult{RequiresApproval: &domain.Approval{ActionType: "host_action:abc", Summary: "x", RiskLevel: domain.ApprovalRiskHigh}}, nil
	}}
	startAgent(t, f, exec, "agent")
	waitFor(t, "failure", func() bool { _, ok := f.failure(task.ID.String()); return ok })
	got, _ := f.failure(task.ID.String())
	if got["retryable"] != false {
		t.Fatalf("must not be retried blindly: %v", got)
	}
}

// After the owner approves, the claim lists the approved action and prior outputs reach the executor.
func TestAgentPassesApprovedActionsAndPriorOutputsToTheExecutor(t *testing.T) {
	f := newFakeAPI(t)
	task := newTask("resumed")
	f.mu.Lock()
	f.queue = append(f.queue, map[string]any{
		"task":               task,
		"approved_actions":   []string{"host_action:abc"},
		"dependency_outputs": []map[string]any{{"title": "compose", "output": map[string]any{"response": "hello"}}},
	})
	f.mu.Unlock()
	var sawApproved bool
	var sawDeps int
	exec := fnExec{fn: func(ctx context.Context, _ *domain.Task) (*worker.ExecutionResult, error) {
		sawApproved = worker.ApprovedActionsFrom(ctx)["host_action:abc"]
		sawDeps = len(worker.DependencyContextFrom(ctx))
		return &worker.ExecutionResult{Output: json.RawMessage(`{"response":"done"}`)}, nil
	}}
	startAgent(t, f, exec, "agent")
	waitFor(t, "completion", func() bool { _, ok := f.completedOutput(task.ID.String()); return ok })
	if !sawApproved || sawDeps != 1 {
		t.Fatalf("executor context: approved=%v deps=%d", sawApproved, sawDeps)
	}
}

func TestAgentStopsClaimingWhenThePhoneOwnerPausesIt(t *testing.T) {
	f := newFakeAPI(t)
	f.mu.Lock()
	f.paused = true
	f.mu.Unlock()
	task := newTask("should wait")
	f.enqueue(task)
	ran := make(chan struct{}, 1)
	exec := fnExec{fn: func(context.Context, *domain.Task) (*worker.ExecutionResult, error) {
		ran <- struct{}{}
		return &worker.ExecutionResult{Output: json.RawMessage(`{"a":1}`)}, nil
	}}
	a, _, _ := startAgent(t, f, exec, "agent")
	waitFor(t, "pause noticed", a.Paused)
	// Claims that raced the first heartbeat may already have happened; once paused, no new work starts.
	f.enqueue(newTask("second"))
	time.Sleep(150 * time.Millisecond)
	f.mu.Lock()
	remaining := len(f.queue)
	f.mu.Unlock()
	if remaining == 0 {
		t.Fatal("a paused host must leave queued work alone")
	}
}

func TestAgentExitsWhenTheTokenIsRevoked(t *testing.T) {
	f := newFakeAPI(t)
	_, _, done := startAgent(t, f, fnExec{fn: func(context.Context, *domain.Task) (*worker.ExecutionResult, error) { return nil, nil }}, "agent")
	waitFor(t, "first heartbeat", func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.beats >= 1 })
	f.mu.Lock()
	f.revoked = true
	f.mu.Unlock()
	select {
	case err := <-done:
		if !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("want ErrUnauthorized, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the agent must stop when its token is revoked")
	}
}

func TestShutdownMidTaskReportsNothing(t *testing.T) {
	f := newFakeAPI(t)
	task := newTask("long")
	f.enqueue(task)
	started := make(chan struct{})
	exec := fnExec{fn: func(ctx context.Context, _ *domain.Task) (*worker.ExecutionResult, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	_, cancel, done := startAgent(t, f, exec, "agent")
	<-started
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Run must return after cancellation")
	}
	if _, ok := f.failure(task.ID.String()); ok {
		t.Fatal("an interrupted step must be left to its lease (requeued), not failed for good")
	}
	if _, ok := f.completedOutput(task.ID.String()); ok {
		t.Fatal("an interrupted step must not be completed")
	}
}

func TestLeaseIsRenewedWhileATaskRuns(t *testing.T) {
	f := newFakeAPI(t)
	task := newTask("slow")
	f.enqueue(task)
	exec := fnExec{fn: func(ctx context.Context, _ *domain.Task) (*worker.ExecutionResult, error) {
		time.Sleep(120 * time.Millisecond)
		return &worker.ExecutionResult{Output: json.RawMessage(`{"ok":true}`)}, nil
	}}
	startAgent(t, f, exec, "agent")
	waitFor(t, "completion", func() bool { _, ok := f.completedOutput(task.ID.String()); return ok })
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.renewals < 2 {
		t.Fatalf("the lease must be renewed while the task runs, renewals=%d", f.renewals)
	}
}

func TestClientNeverLeaksTheTokenInErrors(t *testing.T) {
	c := NewClient("http://127.0.0.1:1", testToken, &http.Client{Timeout: 200 * time.Millisecond})
	_, err := c.Claim(context.Background())
	if err == nil {
		t.Fatal("expected a connection error")
	}
	if strings.Contains(err.Error(), testToken) {
		t.Fatalf("the token must never appear in an error: %v", err)
	}
}

func TestNewRejectsAPresenceIntervalThatWouldLooseLiveStatus(t *testing.T) {
	_, err := New(Config{Client: NewClient("http://x", "t", nil), Executor: fnExec{}, PresenceInterval: domain.HostLiveWindow})
	if err == nil {
		t.Fatal("a presence interval at or over the live window would make the host flicker offline")
	}
}

// The agent is what the gateway reads the policy from.
var _ gateway.Source = (*Agent)(nil)

func (f *fakeAPI) setPolicy(ver int, body map[string]any) {
	f.mu.Lock()
	f.polVer, f.polBody = ver, body
	f.mu.Unlock()
}

func (f *fakeAPI) policyFetchCount() int { f.mu.Lock(); defer f.mu.Unlock(); return f.polFetches }

func idleExec() fnExec {
	return fnExec{fn: func(context.Context, *domain.Task) (*worker.ExecutionResult, error) { return nil, nil }}
}

func TestAgentReceivesThePolicyAndOnlyRefetchesWhenItChanges(t *testing.T) {
	f := newFakeAPI(t)
	f.setPolicy(1, map[string]any{"default": "ask", "rules": []any{map[string]any{"class": "read", "mode": "auto"}}})
	a, _, _ := startAgent(t, f, idleExec(), "agent")

	waitFor(t, "policy v1", func() bool { _, v, ok := a.Policy(); return ok && v == 1 })
	if n := f.policyFetchCount(); n != 1 {
		t.Fatalf("one fetch for version 1, got %d", n)
	}
	// Several more heartbeats with the same version must not refetch.
	f.mu.Lock()
	beats := f.beats
	f.mu.Unlock()
	waitFor(t, "more heartbeats", func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.beats >= beats+3 })
	if n := f.policyFetchCount(); n != 1 {
		t.Fatalf("an unchanged version must not refetch, fetches=%d", n)
	}
	// The owner tightens it: the host notices through the heartbeat alone.
	f.setPolicy(2, map[string]any{"default": "off", "rules": []any{}})
	waitFor(t, "policy v2", func() bool { p, v, ok := a.Policy(); return ok && v == 2 && p.Default == policy.Off })
}

func TestAgentIgnoresAPolicyThatDoesNotValidate(t *testing.T) {
	f := newFakeAPI(t)
	f.setPolicy(1, map[string]any{"default": "off", "rules": []any{}})
	a, _, _ := startAgent(t, f, idleExec(), "agent")
	waitFor(t, "policy v1", func() bool { _, v, ok := a.Policy(); return ok && v == 1 })

	// A malformed answer (say, a bug or tampering) must not loosen anything: the last good one stays.
	f.setPolicy(2, map[string]any{"default": "yolo", "rules": []any{map[string]any{"mode": "auto"}}})
	time.Sleep(250 * time.Millisecond)
	p, v, ok := a.Policy()
	if !ok || v != 1 || p.Default != policy.Off {
		t.Fatalf("the last good policy must stay in force, got v=%d ok=%v default=%s", v, ok, p.Default)
	}
}

func TestAMalformedFirstPolicyLeavesTheHostDenyingEverything(t *testing.T) {
	f := newFakeAPI(t)
	f.setPolicy(1, map[string]any{"default": "yolo", "rules": []any{}})
	a, _, _ := startAgent(t, f, idleExec(), "agent")
	time.Sleep(250 * time.Millisecond)
	if _, _, ok := a.Policy(); ok {
		t.Fatal("an invalid first policy must leave the host unsynced (the gateway then denies everything)")
	}
	g := gateway.New("laptop", a, nil)
	err := g.Require("t", policy.Action{Capability: "files", Class: policy.Read, Path: "/home/me/a"}, policy.Intent{Capability: "files", Class: policy.Read, Folder: "/home/me"})
	if !gateway.IsDenied(err) {
		t.Fatalf("an unsynced host must deny, got %v", err)
	}
}

func TestGatewayFollowsThePolicyTheAgentReceived(t *testing.T) {
	f := newFakeAPI(t)
	f.setPolicy(1, map[string]any{"default": "ask", "rules": []any{map[string]any{"class": "read", "mode": "auto"}}})
	a, _, _ := startAgent(t, f, idleExec(), "agent")
	g := gateway.New("laptop", a, nil)

	act := policy.Action{Capability: "browser", Class: policy.Read, App: "chrome"}
	in := policy.Intent{Capability: "browser", Class: policy.Read, App: "chrome"}
	// Before sync: denied. After: allowed. Then the owner switches reading off: denied again.
	waitFor(t, "synced", func() bool { _, _, ok := a.Policy(); return ok })
	if err := g.Require("t", act, in); err != nil {
		t.Fatalf("reading is auto under this policy, got %v", err)
	}
	f.setPolicy(2, map[string]any{"default": "off", "rules": []any{}})
	waitFor(t, "tightened", func() bool { _, v, _ := a.Policy(); return v == 2 })
	if err := g.Require("t", act, in); !gateway.IsDenied(err) {
		t.Fatalf("after the owner turns everything off the gateway must refuse, got %v", err)
	}
}

func TestDrainHandsARunningTaskBackAsRetryableAndStopsClaiming(t *testing.T) {
	f := newFakeAPI(t)
	task := newTask("long job")
	f.enqueue(task)
	started := make(chan struct{})
	var once sync.Once
	exec := fnExec{fn: func(ctx context.Context, _ *domain.Task) (*worker.ExecutionResult, error) {
		once.Do(func() { close(started) })
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	a, _, _ := startAgent(t, f, exec, "agent")
	<-started
	if got := a.RunningTasks(); len(got) != 1 || got[0].Title != "long job" || got[0].ID != task.ID.String() {
		t.Fatalf("the handover note needs to know what is running, got %+v", got)
	}

	if n := a.Drain("laptop battery is at 5%"); n != 1 {
		t.Fatalf("Drain should ask the one running task to stop, asked %d", n)
	}
	waitFor(t, "task handed back", func() bool { _, ok := f.failure(task.ID.String()); return ok })
	got, _ := f.failure(task.ID.String())
	if got["retryable"] != true || got["reason"] != "paused: laptop battery is at 5%" {
		t.Fatalf("a drained task must come back retryable with the reason, got %v", got)
	}
	waitFor(t, "nothing running", func() bool { return a.InFlight() == 0 && len(a.RunningTasks()) == 0 })

	// While draining nothing new is claimed.
	f.enqueue(newTask("waits"))
	time.Sleep(150 * time.Millisecond)
	f.mu.Lock()
	queued := len(f.queue)
	f.mu.Unlock()
	if queued == 0 || !a.Draining() {
		t.Fatalf("a draining host must leave queued work alone (queued=%d draining=%v)", queued, a.Draining())
	}

	a.Resume()
	waitFor(t, "work claimed again after Resume", func() bool { f.mu.Lock(); defer f.mu.Unlock(); return len(f.queue) == 0 })
}

func TestDrainWithNothingRunningIsHarmless(t *testing.T) {
	f := newFakeAPI(t)
	a, _, _ := startAgent(t, f, fnExec{fn: func(context.Context, *domain.Task) (*worker.ExecutionResult, error) { return nil, nil }}, "agent")
	if n := a.Drain("x"); n != 0 {
		t.Fatalf("nothing was running, asked %d", n)
	}
	a.Resume()
	if a.Draining() {
		t.Fatal("Resume must clear the pause")
	}
}

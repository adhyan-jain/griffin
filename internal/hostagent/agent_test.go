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
}

func newFakeAPI(t *testing.T) *fakeAPI {
	f := &fakeAPI{completed: map[string]json.RawMessage{}, failed: map[string]map[string]any{}}
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
		write(w, 200, map[string]any{"paused": paused, "granted": body.Capabilities, "effective_capabilities": body.Capabilities, "live_window_seconds": 45})
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

func TestAgentFailsApprovalGatedStepsInsteadOfActing(t *testing.T) {
	f := newFakeAPI(t)
	task := newTask("needs approval")
	f.enqueue(task)
	exec := fnExec{fn: func(context.Context, *domain.Task) (*worker.ExecutionResult, error) {
		return &worker.ExecutionResult{RequiresApproval: &domain.Approval{}}, nil
	}}
	startAgent(t, f, exec, "agent")
	waitFor(t, "failure", func() bool { _, ok := f.failure(task.ID.String()); return ok })
	got, _ := f.failure(task.ID.String())
	if got["retryable"] != false {
		t.Fatalf("an approval-gated step must not be retried blindly: %v", got)
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

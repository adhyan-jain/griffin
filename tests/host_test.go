package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/service"
)

type hostEnv struct {
	*TestEnv
	ts       *httptest.Server
	hostRepo repository.HostRepository
	userID   uuid.UUID
	auth     string
}

func setupHostEnv(t *testing.T) *hostEnv {
	t.Helper()
	env := SetupTestEnv(t)
	t.Cleanup(env.Teardown)
	env.Config.HostsEnabled = true

	hostRepo := repository.NewPostgresHostRepository(env.DB.Pool)
	server := api.NewServer(api.ServerDependencies{
		Config: env.Config, DB: env.DB, Engine: env.Engine, TaskRepo: env.TaskRepo,
		ApprovalRepo: env.ApprovalRepo, AgentRepo: env.AgentRepo, ArtifactRepo: env.ArtifactRepo,
		StorageService: env.StorageService, WorkerDefRepo: env.WorkerDefRepo, OrchRepo: env.OrchRepo,
		Orchestrator: env.Orchestrator, Factory: env.AgentFactory, ManagerRegistry: env.ManagerReg,
		HostRepo: hostRepo, PolicyRepo: repository.NewPostgresPolicyRepository(env.DB.Pool),
	})
	ts := httptest.NewServer(server.Handler())
	t.Cleanup(ts.Close)

	email := "host_owner@example.com"
	userID := CreateTestUser(t, env.DB, email)
	return &hostEnv{TestEnv: env, ts: ts, hostRepo: hostRepo, userID: userID, auth: GenerateTestJWT(userID, email)}
}

func (e *hostEnv) do(t *testing.T, method, path, authHeader string, body any) (int, map[string]any) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.ts.URL+path, rd)
	req.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	resp, err := e.ts.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

// register creates a host through the owner API and returns its id and bearer header.
func (e *hostEnv) register(t *testing.T, name, kind string, granted ...string) (uuid.UUID, string) {
	t.Helper()
	code, out := e.do(t, "POST", "/api/v1/hosts", e.auth, map[string]any{"name": name, "kind": kind, "capabilities": granted})
	if code != http.StatusCreated {
		t.Fatalf("register %s: %d %v", name, code, out)
	}
	data := unwrap(out)
	token, _ := data["token"].(string)
	host, _ := data["host"].(map[string]any)
	id, err := uuid.Parse(host["id"].(string))
	if err != nil || token == "" {
		t.Fatalf("register response missing id/token: %v", out)
	}
	return id, "Bearer " + token
}

// unwrap handles both bare and {"data": ...} envelopes.
func unwrap(out map[string]any) map[string]any {
	if d, ok := out["data"].(map[string]any); ok {
		return d
	}
	return out
}

func (e *hostEnv) beat(t *testing.T, bearer string, caps ...string) {
	t.Helper()
	code, out := e.do(t, "POST", "/api/v1/host/heartbeat", bearer, map[string]any{"capabilities": caps, "version": "test"})
	if code != http.StatusOK {
		t.Fatalf("heartbeat: %d %v", code, out)
	}
}

func (e *hostEnv) newTask(t *testing.T, title string, requires ...string) *domain.Task {
	t.Helper()
	payload := json.RawMessage(`{}`)
	if len(requires) > 0 {
		b, _ := json.Marshal(map[string]any{"requires": requires})
		payload = b
	}
	task, err := e.Engine.CreateTask(context.Background(), &domain.Task{
		UserID: e.userID, Title: title, Goal: "goal for " + title, Priority: domain.TaskPriorityNormalP2, InputPayload: payload,
	}, nil)
	if err != nil {
		t.Fatalf("create task %q: %v", title, err)
	}
	return task
}

func claimedTitle(out map[string]any) string {
	d := unwrap(out)
	task, _ := d["task"].(map[string]any)
	title, _ := task["title"].(string)
	return title
}

func TestHostAPIRequiresAValidToken(t *testing.T) {
	e := setupHostEnv(t)
	for _, auth := range []string{"", "Bearer nope", "Bearer ghk_" + "x"} {
		if code, _ := e.do(t, "POST", "/api/v1/host/claim", auth, nil); code != http.StatusUnauthorized {
			t.Errorf("auth %q must be 401, got %d", auth, code)
		}
	}
	// The owner's user JWT is not a host token.
	if code, _ := e.do(t, "POST", "/api/v1/host/claim", e.auth, nil); code != http.StatusUnauthorized {
		t.Errorf("a user JWT must not work on host routes, got %d", code)
	}
	// And a host token is not a user token.
	_, bearer := e.register(t, "laptop", "laptop", "agent")
	if code, _ := e.do(t, "GET", "/api/v1/tasks", bearer, nil); code != http.StatusUnauthorized {
		t.Errorf("a host token must not work on user routes, got %d", code)
	}
}

func TestHostTokenIsStoredOnlyAsAHash(t *testing.T) {
	e := setupHostEnv(t)
	_, bearer := e.register(t, "laptop", "laptop", "agent")
	token := bearer[len("Bearer "):]
	var n int
	if err := e.DB.Pool.QueryRow(context.Background(), `SELECT count(*) FROM hosts WHERE token_hash = $1`, token).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("the plaintext token must never be stored")
	}
}

func TestHostClaimRespectsCapabilities(t *testing.T) {
	e := setupHostEnv(t)
	_, laptop := e.register(t, "laptop", "laptop", "agent", "browser")
	e.beat(t, laptop, "agent", "browser")

	e.newTask(t, "needs shell", "shell")
	e.newTask(t, "needs browser", "browser")

	code, out := e.do(t, "POST", "/api/v1/host/claim", laptop, nil)
	if code != http.StatusOK || claimedTitle(out) != "needs browser" {
		t.Fatalf("laptop must claim the browser step, got %d %v", code, out)
	}
	// The shell step is not covered: nothing more to claim.
	if code, _ := e.do(t, "POST", "/api/v1/host/claim", laptop, nil); code != http.StatusNoContent {
		t.Fatalf("a step needing an ungranted capability must stay queued, got %d", code)
	}
}

func TestHostCannotEscalateItsOwnCapabilities(t *testing.T) {
	e := setupHostEnv(t)
	_, bearer := e.register(t, "laptop", "laptop", "agent") // owner grants only "agent"
	e.beat(t, bearer, "agent", "shell", "browser")          // host claims to also have shell and browser
	e.newTask(t, "needs shell", "shell")
	if code, _ := e.do(t, "POST", "/api/v1/host/claim", bearer, nil); code != http.StatusNoContent {
		t.Fatalf("advertising a capability must not grant it, got %d", code)
	}
}

func TestPlainStepsNeedAnAgentHost(t *testing.T) {
	e := setupHostEnv(t)
	_, phone := e.register(t, "phone", "phone", "phone_apps")
	e.beat(t, phone, "phone_apps")
	e.newTask(t, "plain text step")
	if code, _ := e.do(t, "POST", "/api/v1/host/claim", phone, nil); code != http.StatusNoContent {
		t.Fatalf("a phone host without the agent capability must not take plain steps, got %d", code)
	}
	_, laptop := e.register(t, "laptop", "laptop", "agent")
	e.beat(t, laptop, "agent")
	code, out := e.do(t, "POST", "/api/v1/host/claim", laptop, nil)
	if code != http.StatusOK || claimedTitle(out) != "plain text step" {
		t.Fatalf("an agent host takes plain steps, got %d %v", code, out)
	}
}

func TestCloudRunnerNeverClaimsAStepThatNeedsHands(t *testing.T) {
	e := setupHostEnv(t)
	e.newTask(t, "needs browser", "browser")
	plain := e.newTask(t, "plain")

	got, err := e.Engine.ClaimTask(context.Background(), "ec2-worker", 30*time.Second)
	if err != nil || got == nil || got.ID != plain.ID {
		t.Fatalf("the runner must claim only the plain step, got %v err=%v", got, err)
	}
	again, err := e.Engine.ClaimTask(context.Background(), "ec2-worker", 30*time.Second)
	if err != nil || again != nil {
		t.Fatalf("the runner must leave the browser step queued, got %v err=%v", again, err)
	}
}

func TestRunnableCountIgnoresHostWork(t *testing.T) {
	e := setupHostEnv(t)
	ctx := context.Background()
	e.newTask(t, "needs browser", "browser")
	if n, _ := e.TaskRepo.CountRunnableTasks(ctx); n != 0 {
		t.Fatalf("a step only a host can do must not wake or hold awake the runner, count=%d", n)
	}
	e.newTask(t, "plain")
	if n, _ := e.TaskRepo.CountRunnableTasks(ctx); n != 1 {
		t.Fatalf("a plain step counts, got %d", n)
	}
	// A step a host is running does not keep the runner awake either.
	_, laptop := e.register(t, "laptop", "laptop", "agent", "browser")
	e.beat(t, laptop, "agent", "browser")
	for i := 0; i < 2; i++ { // the browser step and the plain step
		if code, _ := e.do(t, "POST", "/api/v1/host/claim", laptop, nil); code != http.StatusOK {
			t.Fatalf("expected claim %d", i+1)
		}
	}
	if n, _ := e.TaskRepo.CountRunnableTasks(ctx); n != 0 {
		t.Fatalf("host-run steps must not count as runner work, got %d", n)
	}
}

func TestOnlyTheLeaseHolderCanFinishAStep(t *testing.T) {
	e := setupHostEnv(t)
	_, a := e.register(t, "laptop-a", "laptop", "agent")
	_, b := e.register(t, "laptop-b", "laptop", "agent")
	e.beat(t, a, "agent")
	e.beat(t, b, "agent")
	task := e.newTask(t, "work")

	code, out := e.do(t, "POST", "/api/v1/host/claim", a, nil)
	if code != http.StatusOK || claimedTitle(out) != "work" {
		t.Fatalf("a should claim: %d %v", code, out)
	}
	path := "/api/v1/host/tasks/" + task.ID.String()
	if code, _ := e.do(t, "POST", path+"/complete", b, map[string]any{"output": map[string]any{"text": "stolen"}}); code == http.StatusOK {
		t.Fatal("a host that does not hold the lease must not complete the step")
	}
	if code, _ := e.do(t, "POST", path+"/heartbeat", b, nil); code == http.StatusOK {
		t.Fatal("a host that does not hold the lease must not renew it")
	}
	if code, out := e.do(t, "POST", path+"/heartbeat", a, nil); code != http.StatusOK {
		t.Fatalf("lease holder heartbeat: %d %v", code, out)
	}
	if code, out := e.do(t, "POST", path+"/complete", a, map[string]any{"output": map[string]any{"text": "done"}}); code != http.StatusOK {
		t.Fatalf("lease holder complete: %d %v", code, out)
	}
	got, _ := e.Engine.GetTask(context.Background(), task.ID)
	if got.Status != domain.TaskStatusCompleted {
		t.Fatalf("want COMPLETED, got %s", got.Status)
	}
}

func TestHostNeverSeesAnotherUsersTasks(t *testing.T) {
	e := setupHostEnv(t)
	other := CreateTestUser(t, e.DB, "someone_else@example.com")
	if _, err := e.Engine.CreateTask(context.Background(), &domain.Task{
		UserID: other, Title: "not yours", Goal: "g", Priority: domain.TaskPriorityNormalP2,
	}, nil); err != nil {
		t.Fatal(err)
	}
	_, laptop := e.register(t, "laptop", "laptop", "agent")
	e.beat(t, laptop, "agent")
	if code, _ := e.do(t, "POST", "/api/v1/host/claim", laptop, nil); code != http.StatusNoContent {
		t.Fatalf("a host must only be given its owner's tasks, got %d", code)
	}
}

func TestRevokedHostLosesAccessImmediately(t *testing.T) {
	e := setupHostEnv(t)
	id, laptop := e.register(t, "laptop", "laptop", "agent")
	e.beat(t, laptop, "agent")
	if code, out := e.do(t, "DELETE", "/api/v1/hosts/"+id.String(), e.auth, nil); code != http.StatusOK {
		t.Fatalf("revoke: %d %v", code, out)
	}
	if code, _ := e.do(t, "POST", "/api/v1/host/claim", laptop, nil); code != http.StatusUnauthorized {
		t.Fatalf("a revoked token must be refused, got %d", code)
	}
}

func TestPausedHostIsGivenNoWork(t *testing.T) {
	e := setupHostEnv(t)
	id, laptop := e.register(t, "laptop", "laptop", "agent")
	e.beat(t, laptop, "agent")
	e.newTask(t, "work")
	if code, out := e.do(t, "PATCH", "/api/v1/hosts/"+id.String(), e.auth, map[string]any{"paused": true}); code != http.StatusOK {
		t.Fatalf("pause: %d %v", code, out)
	}
	if code, _ := e.do(t, "POST", "/api/v1/host/claim", laptop, nil); code != http.StatusNoContent {
		t.Fatalf("a paused host must get nothing, got %d", code)
	}
	code, out := e.do(t, "POST", "/api/v1/host/heartbeat", laptop, map[string]any{"capabilities": []string{"agent"}})
	if code != http.StatusOK || unwrap(out)["paused"] != true {
		t.Fatalf("heartbeat must tell the host it is paused: %d %v", code, out)
	}
	if code, _ := e.do(t, "PATCH", "/api/v1/hosts/"+id.String(), e.auth, map[string]any{"paused": false}); code != http.StatusOK {
		t.Fatal("resume failed")
	}
	if code, _ := e.do(t, "POST", "/api/v1/host/claim", laptop, nil); code != http.StatusOK {
		t.Fatalf("a resumed host takes work again, got %d", code)
	}
}

func TestOwnerCannotTouchAnotherUsersHost(t *testing.T) {
	e := setupHostEnv(t)
	id, _ := e.register(t, "laptop", "laptop", "agent")
	otherEmail := "intruder@example.com"
	other := CreateTestUser(t, e.DB, otherEmail)
	otherAuth := GenerateTestJWT(other, otherEmail)
	if code, _ := e.do(t, "DELETE", "/api/v1/hosts/"+id.String(), otherAuth, nil); code != http.StatusNotFound {
		t.Fatalf("revoking someone else's host must be 404, got %d", code)
	}
	if code, _ := e.do(t, "PATCH", "/api/v1/hosts/"+id.String(), otherAuth, map[string]any{"paused": true}); code != http.StatusNotFound {
		t.Fatalf("pausing someone else's host must be 404, got %d", code)
	}
}

func TestRegisterValidation(t *testing.T) {
	e := setupHostEnv(t)
	bad := []map[string]any{
		{"name": "", "kind": "laptop"},
		{"name": "x", "kind": "toaster"},
		{"name": "x", "kind": "laptop", "capabilities": []string{"telepathy"}},
	}
	for _, b := range bad {
		if code, _ := e.do(t, "POST", "/api/v1/hosts", e.auth, b); code != http.StatusBadRequest {
			t.Errorf("%v must be 400, got %d", b, code)
		}
	}
}

func TestTaskWithUnknownRequirementIsRejected(t *testing.T) {
	e := setupHostEnv(t)
	code, _ := e.do(t, "POST", "/api/v1/tasks", e.auth, map[string]any{
		"title": "x", "goal": "y", "priority": "NORMAL_P2", "input_payload": map[string]any{"requires": []string{"telepathy"}},
	})
	if code != http.StatusBadRequest {
		t.Fatalf("a task that needs an unknown capability must be refused, not run as plain text; got %d", code)
	}
}

func TestOverviewShowsModeAndWhatIsWaiting(t *testing.T) {
	e := setupHostEnv(t)
	e.newTask(t, "needs browser", "browser")

	code, out := e.do(t, "GET", "/api/v1/hosts", e.auth, nil)
	if code != http.StatusOK {
		t.Fatalf("overview: %d %v", code, out)
	}
	d := unwrap(out)
	if d["mode"] != "none" {
		t.Fatalf("no host yet => mode none, got %v", d["mode"])
	}
	w := d["waiting"].([]any)
	if len(w) != 1 || w[0].(map[string]any)["covered"] != false {
		t.Fatalf("the browser step must be waiting and uncovered: %v", w)
	}

	_, laptop := e.register(t, "laptop", "laptop", "agent", "browser")
	e.beat(t, laptop, "agent", "browser")
	d = unwrap(func() map[string]any { _, o := e.do(t, "GET", "/api/v1/hosts", e.auth, nil); return o }())
	if d["mode"] != "laptop" {
		t.Fatalf("a live laptop agent => laptop mode, got %v", d["mode"])
	}
	if w := d["waiting"].([]any); w[0].(map[string]any)["covered"] != true {
		t.Fatalf("with a live laptop the step is covered: %v", w)
	}

	// Phone only => phone mode.
	if err := e.hostRepo.Revoke(context.Background(), e.userID, mustHostID(t, d), time.Now()); err != nil {
		t.Fatal(err)
	}
	_, phone := e.register(t, "phone", "phone", "phone_apps")
	e.beat(t, phone, "phone_apps")
	d = unwrap(func() map[string]any { _, o := e.do(t, "GET", "/api/v1/hosts", e.auth, nil); return o }())
	if d["mode"] != "phone" {
		t.Fatalf("laptop gone, phone live => phone mode, got %v", d["mode"])
	}
}

func mustHostID(t *testing.T, overview map[string]any) uuid.UUID {
	t.Helper()
	hosts := overview["hosts"].([]any)
	id, err := uuid.Parse(hosts[0].(map[string]any)["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

type fakeWake struct{ calls int }

func (f *fakeWake) TriggerWakeup(context.Context) error { f.calls++; return nil }

type fakePresence struct {
	live bool
	err  error
}

func (f fakePresence) AnyLiveAgentHost(context.Context) (bool, error) { return f.live, f.err }

func TestRunnerIsNotWokenWhileALiveHostCanDoTheWork(t *testing.T) {
	ctx := context.Background()
	inner := &fakeWake{}
	if err := service.NewHostAwareWakeup(inner, fakePresence{live: true}, nil).TriggerWakeup(ctx); err != nil || inner.calls != 0 {
		t.Fatalf("a live host => no wake, calls=%d err=%v", inner.calls, err)
	}
	if err := service.NewHostAwareWakeup(inner, fakePresence{live: false}, nil).TriggerWakeup(ctx); err != nil || inner.calls != 1 {
		t.Fatalf("no live host => wake, calls=%d err=%v", inner.calls, err)
	}
	if err := service.NewHostAwareWakeup(inner, fakePresence{err: errors.New("db down")}, nil).TriggerWakeup(ctx); err != nil || inner.calls != 2 {
		t.Fatalf("unknown presence => wake (a needless wake is cheap, a stuck task is not), calls=%d err=%v", inner.calls, err)
	}
}

func TestPresenceFromTheDatabaseDrivesTheWakeDecision(t *testing.T) {
	e := setupHostEnv(t)
	svc := service.NewHostService(e.hostRepo, e.TaskRepo, 30*time.Second, nil)
	ctx := context.Background()

	if live, _ := svc.AnyLiveAgentHost(ctx); live {
		t.Fatal("no hosts => not live")
	}
	_, laptop := e.register(t, "laptop", "laptop", "agent")
	e.beat(t, laptop, "agent")
	if live, err := svc.AnyLiveAgentHost(ctx); err != nil || !live {
		t.Fatalf("a freshly seen agent host => live, got %v err=%v", live, err)
	}
	// Age the heartbeat past the live window: the runner must be wakeable again.
	if _, err := e.DB.Pool.Exec(ctx, `UPDATE hosts SET last_seen_at = NOW() - INTERVAL '2 minutes'`); err != nil {
		t.Fatal(err)
	}
	if live, _ := svc.AnyLiveAgentHost(ctx); live {
		t.Fatal("a host silent for 2 minutes must not suppress the wake")
	}
}

func TestDependencyOutputsTravelWithTheClaim(t *testing.T) {
	e := setupHostEnv(t)
	ctx := context.Background()
	parent := e.newTask(t, "gather")
	child, err := e.Engine.CreateTask(ctx, &domain.Task{
		UserID: e.userID, Title: "write", Goal: "write it up", Priority: domain.TaskPriorityNormalP2,
	}, []uuid.UUID{parent.ID})
	if err != nil {
		t.Fatal(err)
	}
	_ = child

	_, laptop := e.register(t, "laptop", "laptop", "agent")
	e.beat(t, laptop, "agent")
	code, out := e.do(t, "POST", "/api/v1/host/claim", laptop, nil)
	if code != http.StatusOK || claimedTitle(out) != "gather" {
		t.Fatalf("claim parent: %d %v", code, out)
	}
	if code, o := e.do(t, "POST", "/api/v1/host/tasks/"+parent.ID.String()+"/complete", laptop,
		map[string]any{"output": map[string]any{"text": "facts"}}); code != http.StatusOK {
		t.Fatalf("complete parent: %d %v", code, o)
	}
	code, out = e.do(t, "POST", "/api/v1/host/claim", laptop, nil)
	if code != http.StatusOK || claimedTitle(out) != "write" {
		t.Fatalf("claim child: %d %v", code, out)
	}
	deps, _ := unwrap(out)["dependency_outputs"].([]any)
	if len(deps) != 1 || deps[0].(map[string]any)["title"] != "gather" {
		t.Fatalf("the child must carry its prerequisite's output: %v", unwrap(out))
	}
}

func attemptsOf(t *testing.T, e *hostEnv, id uuid.UUID) []*domain.TaskAttempt {
	t.Helper()
	a, err := e.TaskRepo.GetAttempts(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestHostRunsAppearInTheAttemptsTab(t *testing.T) {
	e := setupHostEnv(t)
	hid, laptop := e.register(t, "laptop", "laptop", "agent")
	e.beat(t, laptop, "agent")
	task := e.newTask(t, "tracked")

	if code, _ := e.do(t, "POST", "/api/v1/host/claim", laptop, nil); code != http.StatusOK {
		t.Fatal("claim failed")
	}
	got := attemptsOf(t, e, task.ID)
	if len(got) != 1 || got[0].Status != "RUNNING" || got[0].AttemptNumber != 1 || got[0].WorkerID != domain.HostWorkerPrefix+hid.String() {
		t.Fatalf("a claim must open attempt 1 for this host, got %+v", got)
	}

	// Allow a retry (a task with no retries left fails for good), then fail it retryably: attempt 1 is closed as FAILED with the reason, and the retry gets attempt 2.
	path := "/api/v1/host/tasks/" + task.ID.String()
	if _, err := e.DB.Pool.Exec(context.Background(), `UPDATE tasks SET max_retries = 3 WHERE id = $1`, task.ID); err != nil {
		t.Fatal(err)
	}
	if code, out := e.do(t, "POST", path+"/fail", laptop, map[string]any{"reason": "rate limited", "retryable": true}); code != http.StatusOK {
		t.Fatalf("fail: %d %v", code, out)
	}
	got = attemptsOf(t, e, task.ID)
	if len(got) != 1 || got[0].Status != "FAILED" || got[0].ErrorMessage == nil || *got[0].ErrorMessage != "rate limited" || got[0].CompletedAt == nil {
		t.Fatalf("a failure must close the attempt with its reason, got %+v", got)
	}

	if _, err := e.DB.Pool.Exec(context.Background(), `UPDATE tasks SET scheduled_for = NULL WHERE id = $1`, task.ID); err != nil {
		t.Fatal(err)
	}
	if code, _ := e.do(t, "POST", "/api/v1/host/claim", laptop, nil); code != http.StatusOK {
		t.Fatal("the retry must be claimable")
	}
	if code, out := e.do(t, "POST", path+"/complete", laptop, map[string]any{"output": map[string]any{"text": "done"}}); code != http.StatusOK {
		t.Fatalf("complete: %d %v", code, out)
	}
	got = attemptsOf(t, e, task.ID)
	if len(got) != 2 {
		t.Fatalf("two tries => two attempts, got %+v", got)
	}
	done := 0
	for _, a := range got {
		if a.Status == "COMPLETED" && a.AttemptNumber == 2 && a.DurationMs >= 0 {
			done++
		}
	}
	if done != 1 {
		t.Fatalf("attempt 2 must be COMPLETED, got %+v", got)
	}
}

func TestRefusedCompletionLeavesNoAttemptBehind(t *testing.T) {
	e := setupHostEnv(t)
	_, a := e.register(t, "laptop-a", "laptop", "agent")
	_, b := e.register(t, "laptop-b", "laptop", "agent")
	e.beat(t, a, "agent")
	e.beat(t, b, "agent")
	task := e.newTask(t, "mine")
	if code, _ := e.do(t, "POST", "/api/v1/host/claim", a, nil); code != http.StatusOK {
		t.Fatal("claim failed")
	}
	e.do(t, "POST", "/api/v1/host/tasks/"+task.ID.String()+"/complete", b, map[string]any{"output": map[string]any{"text": "stolen"}})
	got := attemptsOf(t, e, task.ID)
	if len(got) != 1 || got[0].Status != "RUNNING" {
		t.Fatalf("a refused completion must not touch the attempt, got %+v", got)
	}
}

func newServerWithHostsFlag(t *testing.T, e *hostEnv, enabled bool) *httptest.Server {
	t.Helper()
	cfg := *e.Config
	cfg.HostsEnabled = enabled
	server := api.NewServer(api.ServerDependencies{
		Config: &cfg, DB: e.DB, Engine: e.Engine, TaskRepo: e.TaskRepo,
		ApprovalRepo: e.ApprovalRepo, AgentRepo: e.AgentRepo, ArtifactRepo: e.ArtifactRepo,
		StorageService: e.StorageService, WorkerDefRepo: e.WorkerDefRepo, OrchRepo: e.OrchRepo,
		Orchestrator: e.Orchestrator, Factory: e.AgentFactory, ManagerRegistry: e.ManagerReg,
		HostRepo: e.hostRepo, PolicyRepo: repository.NewPostgresPolicyRepository(e.DB.Pool),
	})
	return httptest.NewServer(server.Handler())
}

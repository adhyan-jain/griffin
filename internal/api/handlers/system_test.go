package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/runner"
	"github.com/griffin/core/internal/storage"
)

var t0 = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func tp(d time.Duration) *time.Time { x := t0.Add(-d); return &x }

func TestDeriveDatabase(t *testing.T) {
	cases := []struct {
		name string
		lat  time.Duration
		err  error
		want string
	}{
		{"fast", 20 * time.Millisecond, nil, StateOK},
		{"at threshold", DBDegradedLatency, nil, StateOK},
		{"slow", DBDegradedLatency + time.Millisecond, nil, StateDegraded},
		{"error", time.Millisecond, errors.New("conn refused host=secret"), StateDown},
	}
	for _, c := range cases {
		got := DeriveDatabase(c.lat, c.err, t0)
		if got.State != c.want {
			t.Errorf("%s: got %s want %s", c.name, got.State, c.want)
		}
		if strings.Contains(got.Evidence, "secret") {
			t.Errorf("%s: evidence leaks error detail: %q", c.name, got.Evidence)
		}
	}
}

func TestDeriveStorage(t *testing.T) {
	cases := []struct {
		name, prov string
		err        error
		want       string
	}{
		{"not found is reachable", "s3", storage.ErrObjectNotFound, StateOK},
		{"found", "s3", nil, StateOK},
		{"denied", "s3", storage.ErrPermissionDenied, StateDegraded},
		{"no bucket", "s3", storage.ErrBucketNotFound, StateDown},
		{"timeout", "s3", context.DeadlineExceeded, StateDown},
	}
	for _, c := range cases {
		if got := DeriveStorage(c.prov, c.err, 0, t0); got.State != c.want {
			t.Errorf("%s: got %s want %s", c.name, got.State, c.want)
		}
	}
	m := DeriveStorage("mock", storage.ErrObjectNotFound, 0, t0)
	if !strings.Contains(m.Evidence, "mock") {
		t.Errorf("mock evidence not labelled: %q", m.Evidence)
	}
}

func TestDeriveRunner(t *testing.T) {
	cases := []struct {
		st   runner.RunnerState
		s2z  bool
		want string
	}{
		{runner.RunnerStateRunning, true, StateOK},
		{runner.RunnerStatePending, true, StateDegraded},
		{runner.RunnerStateStopped, true, StateOK},
		{runner.RunnerStateStopped, false, StateDown},
		{runner.RunnerStateUnknown, true, StateUnknown},
	}
	for _, c := range cases {
		if got := DeriveRunner(c.st, c.s2z, t0); got.State != c.want {
			t.Errorf("%s s2z=%v: got %s want %s", c.st, c.s2z, got.State, c.want)
		}
	}
}

func TestDeriveWorker(t *testing.T) {
	stale := WorkerQueueStaleAfter + time.Minute
	cases := []struct {
		name string
		ev   WorkerEvidence
		rs   runner.RunnerState
		want string
	}{
		{"empty evidence", WorkerEvidence{}, runner.RunnerStateStopped, StateUnknown},
		{"active lease fresh heartbeat", WorkerEvidence{ActiveLeases: 1, LastHeartbeat: tp(30 * time.Second)}, runner.RunnerStateRunning, StateOK},
		{"active lease stale heartbeat", WorkerEvidence{ActiveLeases: 1, LastHeartbeat: tp(WorkerHeartbeatFresh + time.Second)}, runner.RunnerStateRunning, StateUnknown},
		{"recent activity idle", WorkerEvidence{LastActivity: tp(time.Minute)}, runner.RunnerStateStopped, StateOK},
		{"old activity idle", WorkerEvidence{LastActivity: tp(time.Hour)}, runner.RunnerStateStopped, StateUnknown},
		{"expired lease", WorkerEvidence{ExpiredLeases: 2, LastHeartbeat: tp(time.Hour)}, runner.RunnerStateRunning, StateDegraded},
		{"young queue", WorkerEvidence{QueuedCount: 1, OldestQueuedAt: tp(time.Minute)}, runner.RunnerStateRunning, StateUnknown},
		{"stale queue runner running", WorkerEvidence{QueuedCount: 1, OldestQueuedAt: tp(stale)}, runner.RunnerStateRunning, StateDown},
		{"stale queue runner stopped", WorkerEvidence{QueuedCount: 1, OldestQueuedAt: tp(stale)}, runner.RunnerStateStopped, StateDegraded},
		{"stale queue runner pending", WorkerEvidence{QueuedCount: 1, OldestQueuedAt: tp(stale)}, runner.RunnerStatePending, StateDegraded},
		{"stale queue runner unknown", WorkerEvidence{QueuedCount: 1, OldestQueuedAt: tp(stale)}, runner.RunnerStateUnknown, StateUnknown},
		{"stale queue but fresh activity", WorkerEvidence{QueuedCount: 1, OldestQueuedAt: tp(stale), LastActivity: tp(10 * time.Second)}, runner.RunnerStateRunning, StateOK},
	}
	for _, c := range cases {
		if got := DeriveWorker(c.ev, c.rs, t0); got.State != c.want {
			t.Errorf("%s: got %s (%s) want %s", c.name, got.State, got.Evidence, c.want)
		}
	}
}

func TestDeriveProvider(t *testing.T) {
	at := func(status, msg string, ago time.Duration) ProviderAttempt {
		return ProviderAttempt{Status: status, Error: msg, EndedAt: t0.Add(-ago)}
	}
	cases := []struct {
		name string
		in   []ProviderAttempt
		want string
	}{
		{"none", nil, StateUnknown},
		{"success", []ProviderAttempt{at("COMPLETED", "", time.Minute)}, StateOK},
		{"auth failure", []ProviderAttempt{at("FAILED", "error: authentication required. Please run agy auth login", time.Minute)}, StateDown},
		{"timed out", []ProviderAttempt{at("TIMED_OUT", "", time.Minute), at("COMPLETED", "", time.Hour)}, StateDegraded},
		{"timeout message", []ProviderAttempt{at("FAILED", "context deadline exceeded", time.Minute)}, StateDegraded},
		{"single generic failure", []ProviderAttempt{at("FAILED", "boom", time.Minute), at("COMPLETED", "", time.Hour)}, StateUnknown},
		{"streak", []ProviderAttempt{at("FAILED", "a", 1), at("FAILED", "b", 2), at("FAILED", "c", 3), at("COMPLETED", "", time.Hour)}, StateDegraded},
		{"recovered", []ProviderAttempt{at("COMPLETED", "", 1), at("FAILED", "authentication", 2)}, StateOK},
	}
	for _, c := range cases {
		if got := DeriveProvider(c.in); got.State != c.want {
			t.Errorf("%s: got %s (%s) want %s", c.name, got.State, got.Evidence, c.want)
		}
	}
}

// --- handler tests ---

type fakeStore struct {
	we   WorkerEvidence
	werr error
	at   []ProviderAttempt
	aerr error
}

func (f fakeStore) WorkerEvidence(context.Context, time.Time) (WorkerEvidence, error) {
	return f.we, f.werr
}
func (f fakeStore) RecentAttempts(context.Context, int) ([]ProviderAttempt, error) {
	return f.at, f.aerr
}

type fakePinger struct{ err error }

func (p fakePinger) Ping(context.Context) error { return p.err }

type panicPinger struct{}

func (panicPinger) Ping(context.Context) error { panic("boom") }

const testSecret = "test-secret-key-32chars-minimum-length-for-hmac!"

func authedServer(h *SystemHandler) http.Handler {
	auth := middleware.AuthenticateUser(middleware.AuthOptions{HMACSecret: testSecret})
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/system/status", auth(http.HandlerFunc(h.GetStatus)))
	return mux
}

func token(t *testing.T) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": uuid.NewString(), "role": "authenticated", "aud": "authenticated",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	s, err := tok.SignedString([]byte(testSecret))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func get(srv http.Handler, tok string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/api/v1/system/status", nil)
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

type envelope struct {
	Success bool                 `json:"success"`
	Data    SystemStatusResponse `json:"data"`
}

func TestSystemStatus_RequiresAuth(t *testing.T) {
	srv := authedServer(NewSystemHandler(nil, nil, nil, "", nil, false))
	if rec := get(srv, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func TestSystemStatus_EnvelopeAndNoFalseOK(t *testing.T) {
	// Nothing configured: only api may be ok; every other component must be unknown.
	srv := authedServer(NewSystemHandler(nil, nil, nil, "", nil, false))
	rec := get(srv, token(t))
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var env envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if !env.Success {
		t.Fatal("success=false")
	}
	if _, err := time.Parse(time.RFC3339, env.Data.GeneratedAt); err != nil {
		t.Fatalf("generated_at: %v", err)
	}
	for _, name := range []string{"api", "database", "worker", "provider", "scheduler", "storage", "runner"} {
		c, ok := env.Data.Components[name]
		if !ok {
			t.Fatalf("missing component %s", name)
		}
		want := StateUnknown
		if name == "api" {
			want = StateOK
		}
		if c.State != want {
			t.Errorf("%s: state %s want %s", name, c.State, want)
		}
		if c.Evidence == "" {
			t.Errorf("%s: empty evidence", name)
		}
	}
	if !strings.Contains(env.Data.Components["scheduler"].Evidence, "system_heartbeats") {
		t.Error("scheduler evidence should mention the missing migration")
	}
	if env.Data.Components["scheduler"].ObservedAt != nil {
		t.Error("scheduler observed_at must be null")
	}
}

func TestSystemStatus_ProbeFailureIsolation(t *testing.T) {
	mock := storage.NewMockStorageProvider()
	mgr := runner.NewMockInstanceManager(runner.RunnerStateRunning)
	store := fakeStore{werr: errors.New("db down"), at: []ProviderAttempt{{Status: "COMPLETED", EndedAt: t0}}}
	h := NewSystemHandler(panicPinger{}, store, mock, "b", mgr, true)
	rec := get(authedServer(h), token(t))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var env envelope
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	c := env.Data.Components
	if c["database"].State != StateUnknown {
		t.Errorf("panicking db probe: %s", c["database"].State)
	}
	if c["worker"].State != StateUnknown {
		t.Errorf("failing worker query: %s", c["worker"].State)
	}
	if c["provider"].State != StateOK || c["storage"].State != StateOK || c["runner"].State != StateOK {
		t.Errorf("healthy probes were affected: %+v", c)
	}
	if !strings.Contains(c["storage"].Evidence, "mock") {
		t.Errorf("storage evidence not labelled mock: %q", c["storage"].Evidence)
	}
}

func TestSystemStatus_DatabaseDown(t *testing.T) {
	h := NewSystemHandler(fakePinger{err: errors.New("x")}, nil, nil, "", nil, false)
	var env envelope
	_ = json.Unmarshal(get(authedServer(h), token(t)).Body.Bytes(), &env)
	if env.Data.Components["database"].State != StateDown {
		t.Errorf("got %s", env.Data.Components["database"].State)
	}
}

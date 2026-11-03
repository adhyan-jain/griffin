package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/database"
	"github.com/griffin/core/internal/runner"
	"github.com/griffin/core/internal/storage"
)

// Component health states. "unknown" means there is no backend evidence; it is never defaulted to ok.
const (
	StateOK       = "ok"
	StateDegraded = "degraded"
	StateDown     = "down"
	StateUnknown  = "unknown"
)

// Thresholds used to derive component state from evidence.
const (
	DBDegradedLatency        = 500 * time.Millisecond // ping slower than this => degraded
	WorkerHeartbeatFresh     = 2 * time.Minute        // lease/heartbeat/attempt activity this recent => worker ok
	WorkerQueueStaleAfter    = 5 * time.Minute        // queued work older than this with no worker progress => unhealthy
	ProviderFailureStreak    = 3                      // consecutive non-auth, non-timeout failures => degraded
	probeTimeout             = 2 * time.Second        // per-probe context timeout
	runnerProbeTimeout       = 3 * time.Second
	storageProbeKey          = ".griffin-health-probe" // never written; a 404 proves reachability + auth
	schedulerNoEvidenceLabel = "no tick record (needs system_heartbeats migration, not applied)"
)

// ComponentStatus is one component's evidence-backed health.
type ComponentStatus struct {
	State      string  `json:"state"`
	Evidence   string  `json:"evidence"`
	ObservedAt *string `json:"observed_at"`
}

// SystemStatusResponse is the payload of GET /api/v1/system/status.
type SystemStatusResponse struct {
	GeneratedAt string                     `json:"generated_at"`
	Components  map[string]ComponentStatus `json:"components"`
}

// WorkerEvidence is an aggregate of the authenticated user's own task activity (no ids).
type WorkerEvidence struct {
	ActiveLeases   int        // RUNNING with unexpired lease
	ExpiredLeases  int        // RUNNING with expired lease
	LastHeartbeat  *time.Time // latest tasks.updated_at among leased RUNNING tasks
	LastActivity   *time.Time // latest attempt start/completion
	QueuedCount    int
	OldestQueuedAt *time.Time
}

// ProviderAttempt is one recent finished attempt (newest first), without task identifiers.
type ProviderAttempt struct {
	Status  string // COMPLETED, FAILED, TIMED_OUT
	Error   string
	EndedAt time.Time
}

// SystemStatusStore reads per-user aggregates. Implementations must run under the request's
// database scope so RLS limits results to the authenticated user.
type SystemStatusStore interface {
	WorkerEvidence(ctx context.Context, now time.Time) (WorkerEvidence, error)
	RecentAttempts(ctx context.Context, limit int) ([]ProviderAttempt, error)
}

// DBPinger is satisfied by *database.DB.
type DBPinger interface {
	Ping(ctx context.Context) error
}

// StorageProber checks the artifact store. Satisfied by a closure over storage.Provider.
type StorageProber interface {
	Name() string
	Head(ctx context.Context, in storage.HeadInput) (*storage.ObjectInfo, error)
}

// SystemHandler serves GET /api/v1/system/status.
type SystemHandler struct {
	db          DBPinger
	store       SystemStatusStore
	storage     StorageProber
	bucket      string
	instanceMgr runner.InstanceManager
	scaleToZero bool
	now         func() time.Time
}

// NewSystemHandler wires the probes. Any dependency may be nil; its component reports unknown.
func NewSystemHandler(db DBPinger, store SystemStatusStore, st StorageProber, bucket string, im runner.InstanceManager, scaleToZero bool) *SystemHandler {
	return &SystemHandler{db: db, store: store, storage: st, bucket: bucket, instanceMgr: im, scaleToZero: scaleToZero, now: time.Now}
}

func stamp(t time.Time) *string {
	s := t.UTC().Format(time.RFC3339)
	return &s
}

func unknownStatus(evidence string) ComponentStatus {
	return ComponentStatus{State: StateUnknown, Evidence: evidence}
}

// GetStatus runs all probes concurrently except the two store queries, which share the request's
// single transaction and therefore run sequentially. A failing probe only degrades its own component.
func (h *SystemHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	now := h.now()
	var mu sync.Mutex
	comps := map[string]ComponentStatus{
		"api": {State: StateOK, Evidence: "this API process answered the request", ObservedAt: stamp(now)},
		// TODO(system_heartbeats): once docs/proposed/000005_system_heartbeats.sql is applied and the
		// scheduler tick handler records a heartbeat, read it here and derive state from its age.
		"scheduler": unknownStatus(schedulerNoEvidenceLabel),
	}
	set := func(name string, c ComponentStatus) {
		mu.Lock()
		comps[name] = c
		mu.Unlock()
	}
	// safe isolates panics so one probe cannot fail the endpoint.
	safe := func(name string, fn func() ComponentStatus) {
		defer func() {
			if rec := recover(); rec != nil {
				set(name, unknownStatus("probe panicked"))
			}
		}()
		set(name, fn())
	}

	var wg sync.WaitGroup
	run := func(fn func()) {
		wg.Add(1)
		go func() { defer wg.Done(); fn() }()
	}
	run(func() { safe("database", func() ComponentStatus { return h.probeDatabase(r.Context()) }) })
	run(func() { safe("storage", func() ComponentStatus { return h.probeStorage(r.Context()) }) })
	var runnerState runner.RunnerState = runner.RunnerStateUnknown
	var runnerComp ComponentStatus
	run(func() {
		safe("runner", func() ComponentStatus {
			runnerComp, runnerState = h.probeRunner(r.Context())
			return runnerComp
		})
	})
	// Store-backed probes share one tx: sequential. Worker needs runner state, so wait for it first.
	wg.Wait()
	safe("worker", func() ComponentStatus { return h.probeWorker(r.Context(), now, runnerState) })
	safe("provider", func() ComponentStatus { return h.probeProvider(r.Context()) })

	httputil.JSON(w, http.StatusOK, SystemStatusResponse{GeneratedAt: now.UTC().Format(time.RFC3339), Components: comps})
}

func (h *SystemHandler) probeDatabase(parent context.Context) ComponentStatus {
	if h.db == nil {
		return unknownStatus("no database configured")
	}
	ctx, cancel := context.WithTimeout(parent, probeTimeout)
	defer cancel()
	start := h.now()
	err := h.db.Ping(ctx)
	lat := h.now().Sub(start)
	return DeriveDatabase(lat, err, h.now())
}

// DeriveDatabase maps ping latency/error to a state.
func DeriveDatabase(latency time.Duration, err error, observed time.Time) ComponentStatus {
	switch {
	case err != nil:
		return ComponentStatus{StateDown, "pool ping failed: " + shortErr(err), stamp(observed)}
	case latency > DBDegradedLatency:
		return ComponentStatus{StateDegraded, fmt.Sprintf("pool ping slow: %dms (threshold %dms)", latency.Milliseconds(), DBDegradedLatency.Milliseconds()), stamp(observed)}
	default:
		return ComponentStatus{StateOK, fmt.Sprintf("pool ping %dms", latency.Milliseconds()), stamp(observed)}
	}
}

func (h *SystemHandler) probeStorage(parent context.Context) ComponentStatus {
	if h.storage == nil {
		return unknownStatus("no storage provider configured")
	}
	ctx, cancel := context.WithTimeout(parent, probeTimeout)
	defer cancel()
	start := h.now()
	_, err := h.storage.Head(ctx, storage.HeadInput{Bucket: h.bucket, Key: storageProbeKey})
	return DeriveStorage(h.storage.Name(), err, h.now().Sub(start), h.now())
}

// DeriveStorage interprets a HeadObject-style probe of a sentinel key. Not-found proves the bucket
// answered and credentials were accepted.
func DeriveStorage(provider string, err error, latency time.Duration, observed time.Time) ComponentStatus {
	label := provider
	if provider == "mock" {
		label = "mock (in-memory provider, not a real object store)"
	}
	ms := latency.Milliseconds()
	switch {
	case err == nil || errors.Is(err, storage.ErrObjectNotFound):
		return ComponentStatus{StateOK, fmt.Sprintf("%s: bucket probe answered in %dms", label, ms), stamp(observed)}
	case errors.Is(err, storage.ErrPermissionDenied):
		return ComponentStatus{StateDegraded, label + ": reachable but access denied for probe", stamp(observed)}
	case errors.Is(err, storage.ErrBucketNotFound):
		return ComponentStatus{StateDown, label + ": bucket not found", stamp(observed)}
	default:
		return ComponentStatus{StateDown, label + ": probe failed: " + shortErr(err), stamp(observed)}
	}
}

func (h *SystemHandler) probeRunner(parent context.Context) (ComponentStatus, runner.RunnerState) {
	if h.instanceMgr == nil {
		return unknownStatus("no instance manager configured"), runner.RunnerStateUnknown
	}
	ctx, cancel := context.WithTimeout(parent, runnerProbeTimeout)
	defer cancel()
	st, err := h.instanceMgr.GetRunnerState(ctx)
	if err != nil {
		return ComponentStatus{StateUnknown, "runner state lookup failed: " + shortErr(err), stamp(h.now())}, runner.RunnerStateUnknown
	}
	return DeriveRunner(st, h.scaleToZero, h.now()), st
}

// DeriveRunner maps the instance lifecycle state to a component state.
func DeriveRunner(st runner.RunnerState, scaleToZero bool, observed time.Time) ComponentStatus {
	ev := "instance state: " + string(st)
	switch st {
	case runner.RunnerStateRunning:
		return ComponentStatus{StateOK, ev, stamp(observed)}
	case runner.RunnerStatePending:
		return ComponentStatus{StateDegraded, ev + " (starting)", stamp(observed)}
	case runner.RunnerStateStopped, runner.RunnerStateStopping:
		if scaleToZero {
			return ComponentStatus{StateOK, ev + " (scale-to-zero idle; wakes on demand)", stamp(observed)}
		}
		return ComponentStatus{StateDown, ev + " (scale-to-zero disabled, runner should be running)", stamp(observed)}
	default:
		return ComponentStatus{StateUnknown, ev, stamp(observed)}
	}
}

func (h *SystemHandler) probeWorker(parent context.Context, now time.Time, rs runner.RunnerState) ComponentStatus {
	if h.store == nil {
		return unknownStatus("no task store configured")
	}
	ctx, cancel := context.WithTimeout(parent, probeTimeout)
	defer cancel()
	ev, err := h.store.WorkerEvidence(ctx, now)
	if err != nil {
		return unknownStatus("worker evidence query failed: " + shortErr(err))
	}
	return DeriveWorker(ev, rs, now)
}

// DeriveWorker derives worker state from the user's lease/heartbeat/queue aggregates and runner state.
func DeriveWorker(ev WorkerEvidence, rs runner.RunnerState, now time.Time) ComponentStatus {
	age := func(t *time.Time) time.Duration { return now.Sub(*t).Round(time.Second) }
	newest := ev.LastHeartbeat
	if ev.LastActivity != nil && (newest == nil || ev.LastActivity.After(*newest)) {
		newest = ev.LastActivity
	}
	fresh := newest != nil && now.Sub(*newest) <= WorkerHeartbeatFresh

	if ev.ActiveLeases > 0 && fresh {
		return ComponentStatus{StateOK, fmt.Sprintf("%d task(s) under active lease; last heartbeat %s ago", ev.ActiveLeases, age(newest)), stamp(*newest)}
	}
	if ev.ExpiredLeases > 0 {
		return ComponentStatus{StateDegraded, fmt.Sprintf("%d running task(s) with expired lease (awaiting reaper)", ev.ExpiredLeases), nilOr(newest)}
	}
	if ev.QueuedCount > 0 && ev.OldestQueuedAt != nil && now.Sub(*ev.OldestQueuedAt) > WorkerQueueStaleAfter && !fresh {
		qa := age(ev.OldestQueuedAt)
		switch rs {
		case runner.RunnerStateRunning:
			return ComponentStatus{StateDown, fmt.Sprintf("%d queued task(s), oldest %s; runner running but no worker heartbeat", ev.QueuedCount, qa), nilOr(newest)}
		case runner.RunnerStateUnknown:
			return ComponentStatus{StateUnknown, fmt.Sprintf("%d queued task(s), oldest %s; runner state unknown", ev.QueuedCount, qa), nilOr(newest)}
		default:
			return ComponentStatus{StateDegraded, fmt.Sprintf("%d queued task(s), oldest %s; runner is %s", ev.QueuedCount, qa, rs), nilOr(newest)}
		}
	}
	if fresh {
		return ComponentStatus{StateOK, fmt.Sprintf("worker activity %s ago", age(newest)), stamp(*newest)}
	}
	if newest == nil {
		return unknownStatus("no worker activity recorded for your tasks")
	}
	return ComponentStatus{StateUnknown, fmt.Sprintf("idle; last worker activity %s ago", age(newest)), stamp(*newest)}
}

func nilOr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	return stamp(*t)
}

func (h *SystemHandler) probeProvider(parent context.Context) ComponentStatus {
	if h.store == nil {
		return unknownStatus("no task store configured")
	}
	ctx, cancel := context.WithTimeout(parent, probeTimeout)
	defer cancel()
	atts, err := h.store.RecentAttempts(ctx, 10)
	if err != nil {
		return unknownStatus("attempt query failed: " + shortErr(err))
	}
	return DeriveProvider(atts)
}

// DeriveProvider derives AI provider health from the outcomes of recent finished attempts (newest first).
func DeriveProvider(atts []ProviderAttempt) ComponentStatus {
	if len(atts) == 0 {
		return unknownStatus("no finished attempts recorded for your tasks")
	}
	latest := atts[0]
	at := stamp(latest.EndedAt)
	if latest.Status == "COMPLETED" {
		return ComponentStatus{StateOK, "last attempt succeeded at " + latest.EndedAt.UTC().Format(time.RFC3339), at}
	}
	var lastOK string
	for _, a := range atts {
		if a.Status == "COMPLETED" {
			lastOK = "; last success " + a.EndedAt.UTC().Format(time.RFC3339)
			break
		}
	}
	msg := strings.ToLower(latest.Error)
	switch {
	case isAuthFailure(msg):
		return ComponentStatus{StateDown, "latest attempt failed with an authentication error" + lastOK, at}
	case latest.Status == "TIMED_OUT" || strings.Contains(msg, "timeout") || strings.Contains(msg, "timed out") || strings.Contains(msg, "deadline exceeded"):
		return ComponentStatus{StateDegraded, "latest attempt timed out" + lastOK, at}
	}
	streak := 0
	for _, a := range atts {
		if a.Status == "COMPLETED" {
			break
		}
		streak++
	}
	if streak >= ProviderFailureStreak {
		return ComponentStatus{StateDegraded, fmt.Sprintf("%d consecutive failed attempts%s", streak, lastOK), at}
	}
	return ComponentStatus{StateUnknown, "latest attempt failed (cause not attributable to the provider)" + lastOK, at}
}

func isAuthFailure(msg string) bool {
	for _, k := range []string{"authentication", "unauthorized", "auth login", "401", "403", "permission denied", "invalid api key", "invalid_api_key"} {
		if strings.Contains(msg, k) {
			return true
		}
	}
	return false
}

// shortErr returns a bounded, secret-free description (no wrapped DSNs or messages with URLs).
func shortErr(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, database.ErrNoDatabaseScope):
		return "no database scope"
	}
	return "error"
}

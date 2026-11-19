package tests

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/api"
	"github.com/griffin/core/internal/api/handlers"
	"github.com/griffin/core/internal/notify"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/service"
)

type flakySender struct {
	mu    sync.Mutex
	fails int32
	sent  int32
}

func (f *flakySender) Send(_ context.Context, _ notify.Message) error {
	if atomic.LoadInt32(&f.fails) > 0 {
		atomic.AddInt32(&f.fails, -1)
		return errors.New("push provider down")
	}
	atomic.AddInt32(&f.sent, 1)
	return nil
}

// DB-integration: brief deduplication is persisted (restart + concurrent ticks) and failures retry.
func TestDurableBriefDedupAcrossRestartsAndConcurrentTicks(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	u := CreateTestUser(t, env.DB, "durable-brief@example.com")

	loc, _ := time.LoadLocation("Asia/Kolkata")
	clock := time.Date(2026, 11, 19, 8, 5, 0, 0, loc)
	ops := repository.NewOperationRepository(env.DB.Pool)
	coach := &service.DsaCoach{Store: repository.NewDsaRepository(env.DB.Pool), Now: func() time.Time { return clock }}
	newBriefer := func(s notify.Sender) *service.Briefer {
		return &service.Briefer{Sources: repository.NewReportRepository(env.DB.Pool), Coach: coach, Sender: s,
			AppURL: "http://app", Ops: ops, Now: func() time.Time { return clock }}
	}

	push := &pushLog{}
	a := newBriefer(push)
	_, err := a.Build(ctx, u) // enrols the user's settings
	require.NoError(t, err)
	a.Tick(ctx)
	require.Len(t, push.msgs, 1)

	// "Restart": a brand-new Briefer with empty memory must not resend.
	b := newBriefer(push)
	b.Tick(ctx)
	assert.Len(t, push.msgs, 1, "persisted dedup survives a restart")

	// Concurrent ticks on the next local day produce exactly one more brief.
	clock = clock.AddDate(0, 0, 1)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); newBriefer(push).Tick(ctx) }()
	}
	wg.Wait()
	push.mu.Lock()
	n := len(push.msgs)
	push.mu.Unlock()
	assert.Equal(t, 2, n, "concurrent ticks must not duplicate the brief")

	// Failure path: provider down -> not recorded as sent, backs off, then retries and succeeds once.
	clock = clock.AddDate(0, 0, 1)
	flaky := &flakySender{fails: 1}
	f := newBriefer(flaky)
	f.Tick(ctx)
	assert.EqualValues(t, 0, atomic.LoadInt32(&flaky.sent))
	f.Tick(ctx)
	assert.EqualValues(t, 0, atomic.LoadInt32(&flaky.sent), "still backing off")
	_, err = env.DB.Pool.Exec(ctx, `UPDATE scheduled_operations SET next_attempt_at = NOW() - interval '1 second' WHERE user_id=$1 AND status='failed'`, u)
	require.NoError(t, err)
	f.Tick(ctx)
	f.Tick(ctx)
	assert.EqualValues(t, 1, atomic.LoadInt32(&flaky.sent), "retry succeeds exactly once")
}

func TestOperationClaimLeaseAndCursor(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	u := CreateTestUser(t, env.DB, "ops@example.com")
	ops := repository.NewOperationRepository(env.DB.Pool)

	op, err := ops.Claim(ctx, u, "x", "k1", time.Minute, 3)
	require.NoError(t, err)
	require.NotNil(t, op)
	again, err := ops.Claim(ctx, u, "x", "k1", time.Minute, 3)
	require.NoError(t, err)
	assert.Nil(t, again, "a live lease blocks a second claimer")

	_, err = env.DB.Pool.Exec(ctx, `UPDATE scheduled_operations SET lease_expires_at = NOW() - interval '1 second' WHERE id=$1`, op.ID)
	require.NoError(t, err)
	reclaimed, err := ops.Claim(ctx, u, "x", "k1", time.Minute, 3)
	require.NoError(t, err)
	require.NotNil(t, reclaimed, "an expired lease can be taken over")
	assert.Equal(t, 2, reclaimed.Attempts)

	require.NoError(t, ops.Complete(ctx, u, "x", reclaimed))
	done, err := ops.Claim(ctx, u, "x", "k1", time.Minute, 3)
	require.NoError(t, err)
	assert.Nil(t, done, "succeeded runs are never re-run")

	// Attempts exhausted -> dead, never claimed again.
	d, err := ops.Claim(ctx, u, "x", "k2", time.Minute, 1)
	require.NoError(t, err)
	require.NoError(t, ops.Fail(ctx, u, "x", d, 1, errors.New("boom"), 0))
	var status string
	require.NoError(t, env.DB.Pool.QueryRow(ctx, `SELECT status FROM scheduled_operations WHERE id=$1`, d.ID).Scan(&status))
	assert.Equal(t, "dead", status)

	require.NoError(t, ops.SetCursor(ctx, u, "x", []byte(`{"after":"abc"}`)))
	c, err := ops.Cursor(ctx, u, "x")
	require.NoError(t, err)
	assert.JSONEq(t, `{"after":"abc"}`, string(c))
}

// Deployment-style (API mode, no worker process): scheduler ticks drive the proactive operations,
// replayed ticks do not duplicate the brief, and a failing operation does not break the others.
func TestSchedulerTickDrivesProactiveOpsWithoutWorker(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	u := CreateTestUser(t, env.DB, "tick-ops@example.com")

	loc, _ := time.LoadLocation("Asia/Kolkata")
	clock := time.Date(2026, 11, 19, 8, 30, 0, 0, loc)
	push := &pushLog{}
	coach := &service.DsaCoach{Store: repository.NewDsaRepository(env.DB.Pool), Now: func() time.Time { return clock }}
	br := &service.Briefer{Sources: repository.NewReportRepository(env.DB.Pool), Coach: coach, Sender: push,
		Ops: repository.NewOperationRepository(env.DB.Pool), Now: func() time.Time { return clock }}
	_, err := br.Build(ctx, u)
	require.NoError(t, err)

	var other int32
	srv := api.NewServer(api.ServerDependencies{
		Config: env.Config, DB: env.DB, Engine: env.Engine, TaskRepo: env.TaskRepo,
		ApprovalRepo: env.ApprovalRepo, AgentRepo: env.AgentRepo, ArtifactRepo: env.ArtifactRepo,
		ProactiveOps: []handlers.ProactiveOp{
			{Name: "boom", Run: func(context.Context) error { return errors.New("sync provider down") }},
			{Name: "briefs", Run: func(c context.Context) error { br.Tick(c); return nil }},
			{Name: "other", Run: func(context.Context) error { atomic.AddInt32(&other, 1); return nil }},
		},
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	tick := func() map[string]string {
		req, _ := http.NewRequest("POST", ts.URL+"/api/v1/scheduler/tick", nil)
		req.Header.Set("X-Worker-API-Key", env.Config.WorkerAPIKey)
		resp, err := ts.Client().Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var out struct {
			Data struct {
				Operations map[string]string `json:"operations"`
			} `json:"data"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
		return out.Data.Operations
	}

	res := tick()
	assert.Equal(t, "sync provider down", res["boom"])
	assert.Equal(t, "ok", res["briefs"])
	assert.Equal(t, "ok", res["other"], "a failing op must not block later ones")
	tick()
	tick()
	push.mu.Lock()
	defer push.mu.Unlock()
	assert.Len(t, push.msgs, 1, "replayed ticks must not duplicate the brief")
	assert.EqualValues(t, 3, atomic.LoadInt32(&other))
}

package tests

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api/handlers"
)

// Verifies the SQL in PostgresSystemStatusStore against the real schema. The test pool is a
// superuser, so RLS scoping itself is covered by the rls_* tests; this checks aggregation only.
func TestSystemStatusStore_Aggregates(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	user := CreateTestUser(t, env.DB, "status@example.com")
	now := time.Now().UTC()

	insertTask := func(status string, leased bool, leaseOffset time.Duration, updatedAgo time.Duration) uuid.UUID {
		id := uuid.New()
		var leasedBy *string
		var leaseExp *time.Time
		if leased {
			s := "worker-1"
			leasedBy = &s
			e := now.Add(leaseOffset)
			leaseExp = &e
		}
		_, err := env.DB.Pool.Exec(ctx, `
			INSERT INTO tasks (id, user_id, title, goal, status, leased_by, lease_expires_at, updated_at)
			VALUES ($1,$2,'t','g',$3::task_status,$4,$5,$6)`,
			id, user, status, leasedBy, leaseExp, now.Add(-updatedAgo))
		if err != nil {
			t.Fatalf("insert task: %v", err)
		}
		return id
	}

	store := handlers.NewPostgresSystemStatusStore(env.DB.Pool)

	// Empty DB: no evidence at all.
	ev, err := store.WorkerEvidence(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if ev.ActiveLeases != 0 || ev.QueuedCount != 0 || ev.LastHeartbeat != nil || ev.LastActivity != nil || ev.OldestQueuedAt != nil {
		t.Fatalf("expected empty evidence, got %+v", ev)
	}
	if got := handlers.DeriveWorker(ev, "stopped", now); got.State != handlers.StateUnknown {
		t.Fatalf("empty DB must be unknown, got %s", got.State)
	}
	atts, err := store.RecentAttempts(ctx, 10)
	if err != nil || len(atts) != 0 {
		t.Fatalf("attempts: %v %v", atts, err)
	}

	running := insertTask("RUNNING", true, time.Minute, 30*time.Second)
	insertTask("RUNNING", true, -time.Minute, time.Hour) // expired lease
	insertTask("QUEUED", false, 0, 10*time.Minute)
	insertTask("QUEUED", false, 0, time.Minute)
	insertTask("COMPLETED", false, 0, time.Minute) // ignored

	ev, err = store.WorkerEvidence(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if ev.ActiveLeases != 1 || ev.ExpiredLeases != 1 || ev.QueuedCount != 2 {
		t.Fatalf("counts wrong: %+v", ev)
	}
	if ev.OldestQueuedAt == nil || now.Sub(*ev.OldestQueuedAt) < 9*time.Minute {
		t.Fatalf("oldest queued wrong: %v", ev.OldestQueuedAt)
	}
	if ev.LastHeartbeat == nil || now.Sub(*ev.LastHeartbeat) > time.Minute {
		t.Fatalf("heartbeat wrong: %v", ev.LastHeartbeat)
	}

	// Attempts: newest finished first, RUNNING excluded.
	ins := func(status, msg string, ago time.Duration) {
		_, err := env.DB.Pool.Exec(ctx, `
			INSERT INTO task_attempts (task_id, worker_id, status, error_message, started_at, completed_at)
			VALUES ($1,'w',$2,$3,$4,$4)`, running, status, msg, now.Add(-ago))
		if err != nil {
			t.Fatalf("insert attempt: %v", err)
		}
	}
	ins("COMPLETED", "", time.Hour)
	ins("FAILED", "error: authentication required", time.Minute)
	ins("RUNNING", "", time.Second)
	atts, err = store.RecentAttempts(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(atts) != 2 || atts[0].Status != "FAILED" {
		t.Fatalf("attempts wrong: %+v", atts)
	}
	if got := handlers.DeriveProvider(atts); got.State != handlers.StateDown {
		t.Fatalf("auth failure must be down, got %s", got.State)
	}
	ev, _ = store.WorkerEvidence(ctx, now)
	if ev.LastActivity == nil {
		t.Fatal("expected last activity from attempts")
	}
}

package tests

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

// Production reaches Postgres through Supabase's transaction pooler, where pgx uses the simple protocol. Queries
// that pass arrays or uuids must work there too; this runs them in that mode.
func TestNotificationAndListingQueriesWorkThroughThePooler(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	owner := CreateTestUser(t, env.DB, "pooler@example.com")

	url := os.Getenv("GRIFFIN_TEST_DB_URL")
	if url == "" {
		url = TestDBURL
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	notes := repository.NewPostgresNotificationRepository(pool)
	entity := uuid.New()
	n := &domain.Notification{UserID: owner, Kind: "task", EntityID: &entity, DedupeKey: "task:x:COMPLETED", Title: "t", Body: "b", Priority: "default", ActionURL: "https://x"}
	if _, err := notes.Record(ctx, n); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := notes.Ack(ctx, owner, []uuid.UUID{n.ID}); err != nil {
		t.Fatalf("the phone's ack must work through the pooler: %v", err)
	}
	if rows, _ := notes.After(ctx, owner, 0, 10); len(rows) != 1 || rows[0].AckedAt == nil {
		t.Fatalf("acked: %+v", rows)
	}

	listings := repository.NewListingRepository(pool)
	l := domain.ListingFromPosting(owner, "unstop", "A", domain.Posting{Company: "Acme", Title: "SDE Intern", Link: "https://x/1", Posted: time.Now()}, time.Now())
	if _, err := listings.Upsert(ctx, l); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if _, err := listings.List(ctx, owner, []string{domain.StageVerified}, 10); err != nil {
		t.Fatalf("list: %v", err)
	}
	if err := listings.Move(ctx, owner, l.ID, domain.StageVerified, domain.StageClosed, "gone", nil); err != nil {
		t.Fatalf("move: %v", err)
	}
	if err := listings.Checked(ctx, owner, l.ID); err != nil {
		t.Fatalf("checked: %v", err)
	}
}

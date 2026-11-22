package tests

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

func TestTheNotificationOutboxRecordsEachEventOncePerOwner(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	a := CreateTestUser(t, env.DB, "outbox-a@example.com")
	b := CreateTestUser(t, env.DB, "outbox-b@example.com")
	repo := repository.NewPostgresNotificationRepository(env.DB.Pool)

	task := uuid.New()
	row := func(owner uuid.UUID) *domain.Notification {
		return &domain.Notification{UserID: owner, Kind: domain.NotificationKindTask, EntityID: &task,
			DedupeKey: domain.TaskNotificationKey(task, domain.TaskStatusCompleted), Title: "JJ: done", Body: "It is done.",
			ActionURL: "https://app.example/tasks/" + task.String()}
	}
	start, err := repo.LatestSeq(ctx, a)
	if err != nil || start != 0 {
		t.Fatalf("a new owner starts at 0: %d %v", start, err)
	}
	first := row(a)
	if created, err := repo.Record(ctx, first); err != nil || !created || first.Seq == 0 {
		t.Fatalf("first record: %v %v %+v", created, err, first)
	}
	if created, err := repo.Record(ctx, row(a)); err != nil || created {
		t.Fatalf("the same event again must be a no-op: %v %v", created, err)
	}
	if created, err := repo.Record(ctx, row(b)); err != nil || !created {
		t.Fatalf("another owner's copy of the key is their own event: %v %v", created, err)
	}

	rows, err := repo.After(ctx, a, 0, 10)
	if err != nil || len(rows) != 1 || rows[0].ID != first.ID || rows[0].ActionURL != first.ActionURL || rows[0].EntityID == nil || *rows[0].EntityID != task {
		t.Fatalf("after 0: %v %+v", err, rows)
	}
	if rows, _ := repo.After(ctx, a, first.Seq, 10); len(rows) != 0 {
		t.Fatalf("nothing after the cursor: %+v", rows)
	}
	if latest, _ := repo.LatestSeq(ctx, a); latest != first.Seq {
		t.Fatalf("latest cursor: %d want %d", latest, first.Seq)
	}
	if err := repo.Ack(ctx, a, []uuid.UUID{first.ID}); err != nil {
		t.Fatal(err)
	}
	if rows, _ := repo.After(ctx, a, 0, 10); len(rows) != 1 || rows[0].AckedAt == nil {
		t.Fatalf("an acked row is still listed for history, with its ack time: %+v", rows)
	}
	if rows, _ := repo.After(ctx, b, 0, 10); len(rows) != 1 || rows[0].AckedAt != nil {
		t.Fatalf("acking one owner's row leaves the other's alone: %+v", rows)
	}
}

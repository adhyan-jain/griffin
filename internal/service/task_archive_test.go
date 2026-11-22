package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
)

type fakeArchiver struct {
	tasks    []*domain.Task
	reads    int
	archived [][]uuid.UUID
}

func (f *fakeArchiver) ArchiveCandidates(context.Context, uuid.UUID) ([]*domain.Task, error) {
	f.reads++
	return f.tasks, nil
}
func (f *fakeArchiver) ArchiveTasks(_ context.Context, _ uuid.UUID, ids []uuid.UUID, _ time.Time) (int64, error) {
	f.archived = append(f.archived, ids)
	return int64(len(ids)), nil
}
func (f *fakeArchiver) UnarchiveTask(context.Context, uuid.UUID, uuid.UUID) (int64, error) {
	return 0, nil
}
func (f *fakeArchiver) DeleteArchived(context.Context, uuid.UUID) (int64, error) { return 0, nil }

func TestAutoArchiverRunsOncePerHour(t *testing.T) {
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	done := now.Add(-5 * 24 * time.Hour)
	old := &domain.Task{ID: uuid.New(), Title: "Old thing", Status: domain.TaskStatusCompleted, InputPayload: json.RawMessage(`{}`), CompletedAt: &done, UpdatedAt: done, CreatedAt: done}
	store := &fakeArchiver{tasks: []*domain.Task{old}}
	a := &AutoArchiver{Store: store, Now: func() time.Time { return now }}
	user := uuid.New()

	n, err := a.Run(context.Background(), user)
	if err != nil || n != 1 || len(store.archived) != 1 || store.archived[0][0] != old.ID {
		t.Fatalf("first run must archive the old task: n=%d err=%v archived=%v", n, err, store.archived)
	}
	now = now.Add(59 * time.Minute)
	if n, _ := a.Run(context.Background(), user); n != 0 || store.reads != 1 {
		t.Fatalf("within the hour it must not even read: n=%d reads=%d", n, store.reads)
	}
	if _, _ = a.Run(context.Background(), uuid.New()); store.reads != 2 {
		t.Fatalf("another user has their own hour: reads=%d", store.reads)
	}
	now = now.Add(2 * time.Minute)
	if _, _ = a.Run(context.Background(), user); store.reads != 3 {
		t.Fatalf("after the hour it runs again: reads=%d", store.reads)
	}
}

func TestAutoArchiverNilIsSafe(t *testing.T) {
	var a *AutoArchiver
	if n, err := a.Run(context.Background(), uuid.New()); n != 0 || err != nil {
		t.Fatalf("a missing archiver does nothing: %d %v", n, err)
	}
}

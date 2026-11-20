package service

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/storage"
)

type listStore struct{ text string }

func (s listStore) SaveArtifact(context.Context, storage.SaveArtifactInput) (*domain.Artifact, error) {
	return nil, nil
}
func (s listStore) ListByKind(context.Context, uuid.UUID, string) ([]*domain.Artifact, error) {
	return []*domain.Artifact{{ID: uuid.New(), Metadata: domain.TrackerListMetadata("list", "internships")}}, nil
}
func (s listStore) ReadText(context.Context, uuid.UUID, uuid.UUID, int64) (string, error) {
	return s.text, nil
}
func (s listStore) UpdateText(context.Context, uuid.UUID, uuid.UUID, string, bool, int64) (*domain.Artifact, error) {
	return nil, nil
}

func future() string { return time.Now().AddDate(0, 1, 0).Format("2006-01-02") }

func ledger() string {
	return "| Tier | Company | Role | Deadline | Application Link | Status |\n|---|---|---|---|---|---|\n" +
		"| Tier A | Acme | SDE Intern | " + future() + " | https://a | not_applied |\n" +
		"| Tier A | Beta | ML Intern | " + future() + " | https://b | approved_submission_unconfirmed |\n" +
		"| Tier A | Old Co | Intern | 2020-01-01 | https://o | not_applied |\n" +
		"| Tier B | Gamma | Ops Intern | " + future() + " | https://g | not_applied |\n" +
		"| Tier A | Delta | Data Intern | " + future() + " | https://d | not_applied |"
}

func TestUsersTaskIsRecognisedAsABatchForTierA(t *testing.T) {
	tier, ok := domain.ApplyBatchOf("ready all the internships in list of A tier and get them ready for final approva", "ready all the internships in list of A tier and get them ready for final approval", nil)
	if !ok || tier != "A" {
		t.Fatalf("tier=%q ok=%v", tier, ok)
	}
	for _, not := range []string{"Summarise the internship tracker", "Remove the passed internships"} {
		if _, ok := domain.ApplyBatchOf(not, not, nil); ok {
			t.Errorf("%q must not be split into applications", not)
		}
	}
}

func TestABatchBecomesOneSmallTaskPerOpenUnappliedListing(t *testing.T) {
	var made []*domain.Task
	w := &TrackerLists{Store: listStore{ledger()}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	task := &domain.Task{ID: uuid.New(), UserID: uuid.New(), Title: "ready all the internships in list of A tier", Goal: "ready all the internships in list of A tier and get them ready for final approval"}
	answer, handled := w.BatchApply(context.Background(), task, func(_ context.Context, c *domain.Task) error { made = append(made, c); return nil })
	if !handled || len(made) != 2 {
		t.Fatalf("handled=%v made=%d answer=%s", handled, len(made), answer)
	}
	for _, c := range made {
		if c.ParentID == nil || *c.ParentID != task.ID || !strings.HasPrefix(c.Title, "APPLY - ") || !strings.Contains(c.Goal, "Do not submit anything") {
			t.Fatalf("bad child: %+v", c)
		}
	}
	names := made[0].Title + made[1].Title
	if !strings.Contains(names, "Acme") || !strings.Contains(names, "Delta") || strings.Contains(names, "Beta") || strings.Contains(names, "Old Co") || strings.Contains(names, "Gamma") {
		t.Fatalf("only open, unapplied Tier A listings get a task: %s", names)
	}
	if !strings.Contains(answer, "2 application tasks") {
		t.Fatalf("the parent must say what it did: %s", answer)
	}
}

func TestAnOrdinaryTaskIsNotSplit(t *testing.T) {
	w := &TrackerLists{Store: listStore{ledger()}}
	_, handled := w.BatchApply(context.Background(), &domain.Task{Title: "Weekly brief", Goal: "summarise my week"}, func(context.Context, *domain.Task) error { return nil })
	if handled {
		t.Fatal("a task that is not a batch request must run as usual")
	}
}

package service

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/storage"
)

const cardHTML = `<a class="x" href="/internships/acme-ai/ml-engineer-intern-0123456789abcdef0123"><div><p class="t">ML Engineer Intern</p><p><span>Acme AI</span><span><svg></svg>Bengaluru, Karnataka, India</span><span>₹60000 per month</span><span>Posted Nov 20, 2026</span></p></div></a>` +
	`<a href="/internships/hotel/apprentice-0123456789abcdef0123456"><p>Apprentice</p><p><span>Hotel</span><span>Goa, India</span></p></a>` +
	`<a href="/internships/us-co/software-engineering-intern-0123456789abcdef"><p>Software Engineering Intern</p><p><span>US Co</span><span>Austin, Texas, United States</span></p></a>`

func TestInternDockSyncAddsOnlyFittingNewPostings(t *testing.T) {
	store := &memList{text: "# Internships tracker\n\n| Tier | Company | Role | Application Link | Status |\n|---|---|---|---|---|\n| Tier A | Google | SWE Intern | https://g | not_applied |\n"}
	w := &TrackerLists{Store: store, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	fetch := func(context.Context, string) (string, error) { return cardHTML, nil }
	task := &domain.Task{ID: uuid.New(), UserID: uuid.New(), Title: domain.InternDockSyncTitle}
	answer, ok := w.SyncInternDock(context.Background(), task, fetch)
	if !ok || !strings.Contains(answer, "1 were new") {
		t.Fatalf("ok=%v answer=%s", ok, answer)
	}
	if !strings.Contains(store.text, "| Tier A | Acme AI | ML Engineer Intern | https://www.interndock.com/internships/acme-ai/") || strings.Contains(store.text, "Apprentice") || strings.Contains(store.text, "US Co") {
		t.Fatalf("list:\n%s", store.text)
	}
	// A second sync adds nothing: no duplicates.
	if answer, _ := w.SyncInternDock(context.Background(), task, fetch); !strings.Contains(answer, "0 were new") || strings.Count(store.text, "Acme AI") != 1 {
		t.Fatalf("second sync duplicated: %s\n%s", answer, store.text)
	}
	if _, ok := w.SyncInternDock(context.Background(), &domain.Task{Title: "something else"}, fetch); ok {
		t.Fatal("only the sync task is handled")
	}
}

// memList is a one-list store that keeps what is written to it.
type memList struct{ text string }

func (s *memList) SaveArtifact(_ context.Context, in storage.SaveArtifactInput) (*domain.Artifact, error) {
	b, _ := io.ReadAll(in.Body)
	s.text = string(b)
	return &domain.Artifact{ID: uuid.New()}, nil
}
func (s *memList) ListByKind(context.Context, uuid.UUID, string) ([]*domain.Artifact, error) {
	return []*domain.Artifact{{ID: uuid.New(), Metadata: domain.TrackerListMetadata("list", "internships")}}, nil
}
func (s *memList) ReadText(context.Context, uuid.UUID, uuid.UUID, int64) (string, error) {
	return s.text, nil
}
func (s *memList) UpdateText(_ context.Context, _ uuid.UUID, _ uuid.UUID, text string, _ bool, _ int64) (*domain.Artifact, error) {
	s.text = text
	return &domain.Artifact{}, nil
}

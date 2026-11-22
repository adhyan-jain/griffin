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

// memTrackerStore keeps living lists in memory, so Save can be run twice against the list it made the first time.
type memTrackerStore struct {
	arts  []*domain.Artifact
	texts map[uuid.UUID]string
}

func (s *memTrackerStore) SaveArtifact(_ context.Context, in storage.SaveArtifactInput) (*domain.Artifact, error) {
	b, _ := io.ReadAll(in.Body)
	a := &domain.Artifact{ID: uuid.New(), Metadata: in.Metadata, CreatedAt: time.Now()}
	if s.texts == nil {
		s.texts = map[uuid.UUID]string{}
	}
	s.texts[a.ID] = string(b)
	s.arts = append(s.arts, a)
	return a, nil
}
func (s *memTrackerStore) ListByKind(_ context.Context, _ uuid.UUID, kind string) ([]*domain.Artifact, error) {
	var out []*domain.Artifact
	for _, a := range s.arts {
		if strings.Contains(string(a.Metadata), `"kind":"`+kind+`"`) {
			out = append(out, a)
		}
	}
	return out, nil
}
func (s *memTrackerStore) ReadText(_ context.Context, _, id uuid.UUID, _ int64) (string, error) {
	return s.texts[id], nil
}
func (s *memTrackerStore) UpdateText(_ context.Context, id, _ uuid.UUID, text string, _ bool, _ int64) (*domain.Artifact, error) {
	s.texts[id] = text
	return nil, nil
}

// The people search hand answers with a "## Tracker" table of the people it read. The server must create the Alumni
// list from the first answer and, from then on, add only the people not already there.
func TestThePeopleSearchAnswerCreatesAndGrowsTheAlumniListWithoutDuplicates(t *testing.T) {
	store := &memTrackerStore{}
	w := &TrackerLists{Store: store, Logger: slog.Default()}
	task := &domain.Task{ID: uuid.New(), UserID: uuid.New(), Title: "ALUMNI OUTREACH TRACKER", Goal: "g",
		InputPayload: []byte(`{"linkedin_people":{"max":40}}`)}
	day := time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC)
	riya := domain.PersonHit{Name: "Riya Sen", Headline: "SDE-2 @ Razorpay | VIT '22 | ACM", URL: "https://www.linkedin.com/in/riya-sen-1a2b3c/"}
	dev := domain.PersonHit{Name: "Dev Rao", Headline: "Software Engineer at Google | VIT Vellore", URL: "https://www.linkedin.com/in/dev-rao/"}
	answer := func(hits ...domain.PersonHit) string {
		table, _ := domain.AlumniRows(hits, day, 40)
		return "Searched LinkedIn people.\n\n" + domain.TrackerSection + "\n\n" + table
	}

	w.Save(context.Background(), task, answer(riya))
	lists, _ := store.ListByKind(context.Background(), task.UserID, storage.ListKind)
	if len(lists) != 1 || !strings.Contains(store.texts[lists[0].ID], "Riya Sen") {
		t.Fatalf("the first answer starts the Alumni list: %+v", store.texts)
	}

	w.Save(context.Background(), task, answer(riya, dev))
	text := store.texts[lists[0].ID]
	if strings.Count(text, "Riya Sen") != 1 || strings.Count(text, "Dev Rao") != 1 {
		t.Fatalf("the second run adds only Dev:\n%s", text)
	}

	// The message batch then finds both people to propose.
	if ps := domain.AlumniDMProposals(text, 8); len(ps) != 2 || !strings.Contains(ps[0].Title, "Riya Sen") {
		t.Fatalf("proposals: %+v", ps)
	}

	// A run that finds nobody new leaves the list as it is.
	w.Save(context.Background(), task, "Searched LinkedIn people. Nobody new to add today.")
	if store.texts[lists[0].ID] != text {
		t.Fatal("an answer with no table must not touch the list")
	}
}

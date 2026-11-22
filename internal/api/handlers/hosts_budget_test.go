package handlers

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/service"
	"github.com/griffin/core/internal/storage"
)

// lists is a TrackerStore that keeps what is written to it.
type lists struct {
	mu   sync.Mutex
	text string
}

func (s *lists) SaveArtifact(context.Context, storage.SaveArtifactInput) (*domain.Artifact, error) {
	return &domain.Artifact{ID: uuid.New()}, nil
}
func (s *lists) ListByKind(context.Context, uuid.UUID, string) ([]*domain.Artifact, error) {
	return []*domain.Artifact{{ID: uuid.New(), Metadata: domain.TrackerListMetadata("list", "hackathons")}}, nil
}
func (s *lists) ReadText(context.Context, uuid.UUID, uuid.UUID, int64) (string, error) {
	return s.text, nil
}
func (s *lists) UpdateText(_ context.Context, _, _ uuid.UUID, text string, _ bool, _ int64) (*domain.Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.text = text
	return &domain.Artifact{}, nil
}

// A claimed routine runs in code during the claim request, which the host platform kills at 29 seconds. The context
// it runs under must carry a deadline RunInCodeBudget after the request began.
func TestTheClaimRunsCodeRoutinesUnderADeadlineFromTheStartOfTheRequest(t *testing.T) {
	var deadlines []time.Time
	var fetches int
	w := &service.TrackerLists{Store: &lists{text: "keep me"}, Hackathons: &service.HackathonSync{
		Get: func(ctx context.Context, _ string) ([]byte, error) {
			fetches++
			d, ok := ctx.Deadline()
			if !ok {
				t.Error("the routine's context has no deadline")
			}
			deadlines = append(deadlines, d)
			return nil, errors.New("offline")
		},
		Post: func(ctx context.Context, _ string, _ []byte) ([]byte, error) { return nil, errors.New("offline") },
	}}
	h := &HostHandler{trackers: w}
	task := &domain.Task{ID: uuid.New(), UserID: uuid.New(), Title: "HACKATHON TRACKER (daily)"}

	start := time.Now()
	answer, how := h.runInCode(context.Background(), start, task)
	if how != service.Answered || fetches == 0 {
		t.Fatalf("the routine ran: %v %q fetches=%d", how, answer, fetches)
	}
	// Fetches end a little before the claim's own deadline, so the answer can still be saved.
	for _, d := range deadlines {
		if d.After(start.Add(RunInCodeBudget)) || d.Before(start.Add(RunInCodeBudget-5*time.Second)) {
			t.Fatalf("a fetch deadline %v is not about %v after the start", d.Sub(start), RunInCodeBudget)
		}
	}
	if RunInCodeBudget >= 29*time.Second || RunInCodeBudget < 15*time.Second {
		t.Fatalf("the budget %v must leave room before Lambda's 29 second kill", RunInCodeBudget)
	}
}

func TestACodeRoutineClaimedLateInTheRequestStopsAtOnceAndStillAnswers(t *testing.T) {
	fetches := 0
	store := &lists{text: "keep me"}
	w := &service.TrackerLists{Store: store, Hackathons: &service.HackathonSync{
		Get:  func(context.Context, string) ([]byte, error) { fetches++; return nil, errors.New("offline") },
		Post: func(context.Context, string, []byte) ([]byte, error) { fetches++; return nil, errors.New("offline") },
	}}
	h := &HostHandler{trackers: w}
	// The request already spent 20 of its 22 seconds on other work (reaping leases, archiving, leasing the task).
	answer, how := h.runInCode(context.Background(), time.Now().Add(-20*time.Second), &domain.Task{ID: uuid.New(), UserID: uuid.New(), Title: "HACKATHON TRACKER"})
	if how != service.Answered || fetches != 0 || !strings.Contains(answer, "left for the next run") || store.text != "keep me" {
		t.Fatalf("a normal answer and nothing started: %v %q fetches=%d list=%q", how, answer, fetches, store.text)
	}
}

func TestTheCodeBudgetCanBeOverridden(t *testing.T) {
	var got time.Time
	w := &service.TrackerLists{Store: &lists{}, Hackathons: &service.HackathonSync{
		Get: func(ctx context.Context, _ string) ([]byte, error) {
			got, _ = ctx.Deadline()
			return nil, errors.New("offline")
		},
		Post: func(context.Context, string, []byte) ([]byte, error) { return nil, errors.New("offline") },
	}}
	h := &HostHandler{trackers: w, codeBudget: time.Minute}
	start := time.Now()
	h.runInCode(context.Background(), start, &domain.Task{ID: uuid.New(), UserID: uuid.New(), Title: "HACKATHON TRACKER"})
	if got.IsZero() || got.Sub(start) < 50*time.Second {
		t.Fatalf("the override is used: %v", got.Sub(start))
	}
}

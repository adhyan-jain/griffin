package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/storage"
)

type alumniStore struct{ text string }

func (s alumniStore) SaveArtifact(context.Context, storage.SaveArtifactInput) (*domain.Artifact, error) {
	return nil, nil
}
func (s alumniStore) ListByKind(context.Context, uuid.UUID, string) ([]*domain.Artifact, error) {
	return []*domain.Artifact{{ID: uuid.New(), Metadata: domain.TrackerListMetadata("list", "alumni")}}, nil
}
func (s alumniStore) ReadText(context.Context, uuid.UUID, uuid.UUID, int64) (string, error) {
	return s.text, nil
}
func (s alumniStore) UpdateText(context.Context, uuid.UUID, uuid.UUID, string, bool, int64) (*domain.Artifact, error) {
	return nil, nil
}

func TestTheAlumniBatchIsPlainCodeAndItsMessagesPassTheRules(t *testing.T) {
	list := domain.AlumniHeader + "\n" +
		"| Sara Iyer | 2024 | Google | ML Engineer | ACM VIT | https://www.linkedin.com/in/sara | intern ₹1.5L | not contacted | 2026-11-20 |\n" +
		"| Meera Das | 2025 | Razorpay | Software Engineer | | https://www.linkedin.com/in/meera | x | not contacted | 2026-11-20 |"
	w := &TrackerLists{Store: alumniStore{list}}
	task := &domain.Task{ID: uuid.New(), UserID: uuid.New(), Title: domain.AlumniDMBatchTitle, Goal: domain.AlumniDMBatchGoal}
	answer, how := w.RunInCode(context.Background(), task, nil, nil)
	if how != Answered || !strings.Contains(answer, "Sara Iyer") {
		t.Fatalf("the batch is answered in code: %v %s", how, answer)
	}

	// The engine turns the answer into approvals, after the outreach rules.
	repo := &proposalRepo{parent: task}
	e := NewTaskEngine(repo, nil, nil)
	e.SetOwnerFacts(func(context.Context, uuid.UUID) string {
		return "- Full name: Adhyan Jain\n- [f1] B.Tech CSE at VIT\n- [f2] ComputeHive: distributed task runner in Go with Redis and Docker\n- [f3] AI Engineer intern at Palni\n- [f4] senior core member of ACM VIT"
	})
	out, _ := json.Marshal(map[string]string{"response": answer})
	e.createProposals(context.Background(), task.ID, out)
	if len(repo.approvals) != 2 {
		t.Fatalf("both messages follow the owner's own template and pass: %d approvals, events %v", len(repo.approvals), repo.events)
	}
	if !strings.Contains(repo.created[0].Goal, "ACM VIT") || strings.Contains(repo.created[1].Goal, "ACM") {
		t.Fatal("ACM is named only to the ACM alum")
	}
	if !strings.Contains(repo.created[0].Goal, domain.ResumeFiles["AI/ML"]) {
		t.Fatalf("the AI/ML resume goes to the ML engineer:\n%s", repo.created[0].Goal)
	}
	if repo.created[0].MaxRetries != 0 {
		t.Fatal("a message is never retried: it could reach the same person twice")
	}
}

func TestAnOrdinaryTaskIsNotRunInCode(t *testing.T) {
	w := &TrackerLists{Store: alumniStore{}}
	if _, how := w.RunInCode(context.Background(), &domain.Task{Title: "Compare laptops", Goal: "compare"}, nil, nil); how != NotInCode {
		t.Fatalf("got %v", how)
	}
	if _, how := (*TrackerLists)(nil).RunInCode(context.Background(), &domain.Task{}, nil, nil); how != NotInCode {
		t.Fatal("no lists, no code path")
	}
}

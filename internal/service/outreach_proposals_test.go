package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

// proposalRepo records what the engine creates when a proposing task finishes.
type proposalRepo struct {
	repository.TaskRepository
	parent    *domain.Task
	created   []*domain.Task
	approvals []*domain.Approval
	events    []string
}

func (r *proposalRepo) GetTaskByID(_ context.Context, id uuid.UUID) (*domain.Task, error) {
	if id == r.parent.ID {
		return r.parent, nil
	}
	return nil, domain.ErrNotFound
}
func (r *proposalRepo) ListTasks(context.Context, repository.TaskFilter) ([]*domain.Task, error) {
	return nil, nil
}
func (r *proposalRepo) CreateTask(_ context.Context, t *domain.Task) (*domain.Task, error) {
	r.created = append(r.created, t)
	return t, nil
}
func (r *proposalRepo) AddDependency(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (r *proposalRepo) CheckDependenciesCompleted(context.Context, uuid.UUID) (bool, error) {
	return false, nil
}
func (r *proposalRepo) UpdateTask(context.Context, *domain.Task) error { return nil }
func (r *proposalRepo) TransitionToApproval(_ context.Context, _ uuid.UUID, a *domain.Approval) error {
	r.approvals = append(r.approvals, a)
	return nil
}
func (r *proposalRepo) RecordEvent(_ context.Context, ev *domain.TaskEvent) error {
	r.events = append(r.events, ev.Message)
	return nil
}

const facts = "- Full name: Adhyan Jain\n- [f1] B.Tech CSE at VIT, pre-final year\n- [f2] ComputeHive: distributed task runner in Go with Redis and Docker\n- [f3] AI Engineer intern at Palni"

func TestOnlyOutreachThatPassesTheRulesReachesTheOwner(t *testing.T) {
	_, _, payload := domain.ReferralTaskFor("NexusFlow", "Software Engineer Intern")
	parent := &domain.Task{ID: uuid.New(), UserID: uuid.New(), Title: "REFERRALS - NexusFlow - Software Engineer Intern", InputPayload: payload}
	good := domain.OutreachContact{Name: "Riya Sharma", Title: "Software Engineer", Company: "NexusFlow", Link: "https://www.linkedin.com/in/riya",
		Message: "Hi Riya, I'm Adhyan, a CSE student at VIT applying for the Software Engineer Intern role. I built ComputeHive in Go with Redis and Docker. Would you be open to referring me?"}
	keutzer := domain.OutreachContact{Name: "Kurt Keutzer", Title: "Co-founder", Company: "NexusFlow", Link: "https://www.linkedin.com/in/kurt",
		Message: "Dear Professor Keutzer, I wrote SGLang guides. Would you refer me?"}
	ps := []domain.Proposal{
		{Title: "Message Riya Sharma - NexusFlow", Why: "an engineer on the team", Contact: &good, Resume: domain.ResumeFiles["software engineering"]},
		{Title: "Message Kurt Keutzer - NexusFlow", Why: "co-founder", Contact: &keutzer},
		{Title: "Message someone", Why: "no details", Goal: "Send this message to someone: hi"},
	}
	out, _ := json.Marshal(map[string]string{"response": "Found three people.\n\n" + domain.FormatProposals(ps)})

	repo := &proposalRepo{parent: parent}
	e := NewTaskEngine(repo, nil, nil)
	e.SetOwnerFacts(func(context.Context, uuid.UUID) string { return facts })
	e.createProposals(context.Background(), parent.ID, out)

	if len(repo.created) != 1 || len(repo.approvals) != 1 {
		t.Fatalf("only the engineer's message may be proposed: %d tasks, %d approvals", len(repo.created), len(repo.approvals))
	}
	goal := repo.created[0].Goal
	if !strings.Contains(goal, good.Message) || !strings.Contains(goal, good.Link) || !strings.Contains(goal, domain.ResumeFiles["software engineering"]) {
		t.Fatalf("the send goal is written by code around the checked message:\n%s", goal)
	}
	if repo.created[0].MaxRetries != 0 {
		t.Fatal("a message is never retried: it could reach the same person twice")
	}
	if repo.approvals[0].ActionType != domain.ProposalApprovalAction || strings.Contains(string(repo.created[0].InputPayload), "act") {
		t.Fatalf("an approved message is handed to the owner, never given the act tier: %s %s", repo.approvals[0].ActionType, repo.created[0].InputPayload)
	}
	if !strings.Contains(goal, domain.ManualSendMarker) {
		t.Fatalf("the goal says Griffin does not send it:\n%s", goal)
	}
	if answer, ok := HandOverOutreach(repo.created[0]); !ok || domain.AlumniDMResult(answer) != domain.AlumniStatusByHand || !strings.Contains(answer, good.Message) {
		t.Fatalf("the approved task is answered in code with the message for the owner: ok=%v\n%s", ok, answer)
	}
	joined := strings.Join(repo.events, "\n")
	if !strings.Contains(joined, "Kurt Keutzer") || !strings.Contains(joined, "senior") || !strings.Contains(joined, "Message someone") {
		t.Fatalf("the owner can see what was held back and why:\n%s", joined)
	}
}

func TestWithoutTheOwnersProfileNoOutreachIsProposed(t *testing.T) {
	_, _, payload := domain.ReferralTaskFor("Acme", "SDE Intern")
	parent := &domain.Task{ID: uuid.New(), UserID: uuid.New(), Title: "REFERRALS - Acme - SDE Intern", InputPayload: payload}
	c := domain.OutreachContact{Name: "Riya", Title: "SDE", Company: "Acme", Link: "https://www.linkedin.com/in/riya", Message: "Hi Riya, would you refer me?"}
	out, _ := json.Marshal(map[string]string{"response": domain.FormatProposals([]domain.Proposal{{Title: "Message Riya - Acme", Contact: &c}})})
	repo := &proposalRepo{parent: parent}
	NewTaskEngine(repo, nil, nil).createProposals(context.Background(), parent.ID, out)
	if len(repo.created) != 0 {
		t.Fatal("claims cannot be checked without the profile, so nothing is proposed")
	}
}

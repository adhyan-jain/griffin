package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/storage"
)

// CodeListsDeps is what the routines that run in code need. A nil field leaves its routine to the model path.
type CodeListsDeps struct {
	Store            *storage.StorageService
	Tasks            repository.TaskRepository
	Engine           *TaskEngine
	Logger           *slog.Logger
	GitHubLogin      string
	CommunityReplies OutreachChatter
	Listings         *repository.OpportunityRepository
	Assess           *OpportunityAssess
	Prep             *ApplicationPrep
	Reminders        *Reminders
	Research         ResearchRunner
	People           *repository.PeopleRepository
}

// NewCodeLists builds the TrackerLists whose RunInCode the scheduler tick uses, wired like the host claim route's.
func NewCodeLists(d CodeListsDeps) *TrackerLists {
	if d.Store == nil || d.Engine == nil {
		return nil
	}
	lists := &TrackerLists{Store: d.Store, Tasks: d.Tasks, Logger: d.Logger, ApprovedDrafts: d.Engine.ApprovedDraftTasks,
		CheckPosting: func(ctx context.Context, link string) PostingState {
			return CheckPosting(ctx, nil, link)
		},
		Hackathons: &HackathonSync{}, OpenSource: &OpenSourceSync{}, Hygiene: &HygieneSync{Login: d.GitHubLogin}}
	if d.CommunityReplies != nil {
		lists.ReplyModel, lists.OwnerFacts = d.CommunityReplies, d.Engine.OwnerFacts
	}
	if d.Listings != nil {
		lists.Details = DetailsFor(d.Listings)
		lists.Sync = &ListingSync{Listings: d.Listings, Details: lists.Details}
		lists.Opportunities = &OpportunityScan{Store: d.Listings}
	}
	lists.Assess = d.Assess
	lists.Reminders = d.Reminders
	lists.Prep = d.Prep
	if d.Research != nil {
		lists.ResearchRunner = d.Research
	}
	if d.People != nil {
		lists.Alumni = NewAlumniPeople(d.Store, d.People)
	}
	return lists
}

// DeliverDueReminders delivers every owner's due reminders: the tick's job, so a reminder arrives on time with no
// phone poll and no worker.
func DeliverDueReminders(ctx context.Context, r *Reminders, users interface {
	UsersWithDueReminders(ctx context.Context, now time.Time, limit int) ([]uuid.UUID, error)
}) error {
	if r == nil || users == nil {
		return nil
	}
	ids, err := users.UsersWithDueReminders(ctx, r.now(), 50)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		r.DeliverDue(ctx, id)
	}
	return nil
}

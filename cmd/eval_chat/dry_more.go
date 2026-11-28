package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/agent"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/service"
)

// newDryMore gives the second tool batch fake services: reads show sample data, writes change nothing real.
func newDryMore(user uuid.UUID) agent.MoreDeps {
	p := &dryPeople{people: []*domain.Person{
		{ID: uuid.New(), Name: "Asha Rao", Company: "Acme", Title: "SDE", PriorityTier: "P1", ContactState: domain.ContactNotContacted},
		{ID: uuid.New(), Name: "Vikram Nair", Company: "Globex", Title: "ML Engineer", PriorityTier: "P2", ContactState: domain.ContactNotContacted},
	}}
	camp := &domain.OutreachCampaign{ID: uuid.New(), UserID: user, Name: "Founders cold mail", Channel: "email", Status: "active", Limits: domain.DefaultOutreachLimits("email")}
	art := &domain.Artifact{ID: uuid.New(), UserID: user, Title: "Project ideas", ArtifactType: "MARKDOWN"}
	host := &domain.Host{ID: uuid.New(), UserID: user, Name: "laptop", Kind: "laptop", Capabilities: []string{"files"}}
	return agent.MoreDeps{
		People: p, Holder: dryHolder{user}, Importer: dryImporter{},
		VIT:      dryVIT{},
		Outreach: &dryOutreach{camp: camp, targets: []*domain.OutreachTarget{{ID: uuid.New(), CampaignID: camp.ID, Name: "Pat Lee", Handle: "pat@example.com", Status: domain.OutreachDrafted, Draft: "Hi Pat"}}},
		Drafter:  dryDrafter{},
		Library:  &dryLibrary{art: art}, Hosts: &dryHosts{host: host}, Profile: &dryProfile{}, Aliases: dryAliases{"mummy": "mom"},
		Notifications: dryNotes{}, Research: dryResearch{}, Packages: dryPackages{}, Companies: dryCompanies{},
		TaskInfo: dryTaskInfo{}, Archive: dryArchive{}, DeleteTask: func(context.Context, uuid.UUID) error { return nil },
		DSA: dryDSA{}, Brief: dryBrief{}, ApprovalEdit: dryApprovalEdit{},
		Assessments:    func(context.Context, uuid.UUID) ([]*domain.AssessmentRecord, error) { return nil, nil },
		DiscoveredJobs: func(context.Context, uuid.UUID, string, int) ([]*domain.DiscoveredJob, error) { return nil, nil },
		InboundEvents:  func(context.Context, uuid.UUID, int, bool) ([]*domain.InboundEvent, error) { return nil, nil },
		SourceHealth:   func(context.Context, uuid.UUID) ([]domain.SourceHealth, error) { return nil, nil },
	}
}

type dryPeople struct{ people []*domain.Person }

func (f *dryPeople) List(context.Context, uuid.UUID, repository.PersonFilter) ([]*domain.Person, error) {
	return f.people, nil
}
func (f *dryPeople) Get(_ context.Context, _, id uuid.UUID) (*domain.Person, error) {
	for _, p := range f.people {
		if p.ID == id {
			return p, nil
		}
	}
	return nil, domain.ErrNotFound
}
func (f *dryPeople) Update(ctx context.Context, u, id uuid.UUID, s *domain.ContactState, dnc *bool) (*domain.Person, error) {
	p, err := f.Get(ctx, u, id)
	if err == nil && s != nil {
		p.ContactState = *s
	}
	if err == nil && dnc != nil {
		p.DoNotContact = *dnc
	}
	return p, err
}
func (f *dryPeople) MarkDrafted(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return true, nil
}
func (f *dryPeople) ApplyVerification(ctx context.Context, u, id uuid.UUID, _ *domain.VerifiedFields) (*domain.Person, error) {
	return f.Get(ctx, u, id)
}

type dryHolder struct{ user uuid.UUID }

func (h dryHolder) HoldAlumniOutreach(context.Context, uuid.UUID, string, json.RawMessage) (*domain.Approval, error) {
	return &domain.Approval{ID: uuid.New(), UserID: h.user}, nil
}

type dryImporter struct{}

func (dryImporter) Import(context.Context, uuid.UUID) (repository.ImportResult, error) {
	return repository.ImportResult{Created: 2}, nil
}

type dryVIT struct{ agent.VITAPI }

func (dryVIT) GetCampaignStats(context.Context, uuid.UUID) (*domain.VITCampaignStats, error) {
	return &domain.VITCampaignStats{TotalPeople: 2}, nil
}
func (dryVIT) GetCampaignQueue(context.Context, uuid.UUID, int) ([]*domain.VITCampaignTarget, error) {
	return []*domain.VITCampaignTarget{{ID: uuid.New(), Name: "Asha Rao"}}, nil
}
func (dryVIT) BuildQueue(context.Context, uuid.UUID) (*domain.VITQueueBuild, error) {
	return &domain.VITQueueBuild{}, nil
}
func (dryVIT) UpdateTargetAction(context.Context, uuid.UUID, uuid.UUID, string) error { return nil }
func (dryVIT) GetLinkedInQueries() []domain.LinkedInSearchQuery {
	return []domain.LinkedInSearchQuery{{Category: "ACM", Query: "ACM VIT", URL: "https://www.linkedin.com/search/results/people/?keywords=ACM"}}
}

type dryOutreach struct {
	agent.OutreachAPI
	camp    *domain.OutreachCampaign
	targets []*domain.OutreachTarget
}

func (f *dryOutreach) CreateCampaign(_ context.Context, c *domain.OutreachCampaign) error {
	c.ID = uuid.New()
	return nil
}
func (f *dryOutreach) ListCampaigns(context.Context, uuid.UUID) ([]*domain.OutreachCampaign, error) {
	return []*domain.OutreachCampaign{f.camp}, nil
}
func (f *dryOutreach) GetCampaign(context.Context, uuid.UUID, uuid.UUID) (*domain.OutreachCampaign, error) {
	return f.camp, nil
}
func (f *dryOutreach) UpdateCampaign(_ context.Context, _, _ uuid.UUID, st *string, _ *domain.OutreachLimits) (*domain.OutreachCampaign, error) {
	if st != nil {
		f.camp.Status = *st
	}
	return f.camp, nil
}
func (f *dryOutreach) Counts(context.Context, uuid.UUID, uuid.UUID) (map[string]int, error) {
	return map[string]int{"drafted": 1}, nil
}
func (f *dryOutreach) ListTargets(context.Context, uuid.UUID, uuid.UUID, *domain.OutreachStatus) ([]*domain.OutreachTarget, error) {
	return f.targets, nil
}
func (f *dryOutreach) UpsertTargets(_ context.Context, _, _ uuid.UUID, in []domain.OutreachTarget) (int, error) {
	return len(in), nil
}
func (f *dryOutreach) CampaignOf(context.Context, uuid.UUID, uuid.UUID) (uuid.UUID, error) {
	return f.camp.ID, nil
}
func (f *dryOutreach) UpdateTarget(context.Context, uuid.UUID, uuid.UUID, repository.TargetUpdate) (*domain.OutreachTarget, error) {
	return f.targets[0], nil
}
func (f *dryOutreach) AwaitingReply(context.Context, uuid.UUID) ([]repository.Awaiting, error) {
	return []repository.Awaiting{{Name: "Pat Lee", Handle: "pat@example.com"}}, nil
}
func (f *dryOutreach) RecordReplyByName(context.Context, uuid.UUID, string, string) (*domain.OutreachTarget, error) {
	return f.targets[0], nil
}
func (f *dryOutreach) RecordReplyByHandle(context.Context, uuid.UUID, string, string) (*domain.OutreachTarget, error) {
	return f.targets[0], nil
}
func (f *dryOutreach) ClaimNextToSend(context.Context, uuid.UUID, uuid.UUID, time.Time) (*repository.SendClaim, error) {
	return &repository.SendClaim{Reason: "dry run"}, nil
}

type dryDrafter struct{}

func (dryDrafter) Draft(context.Context, *domain.OutreachCampaign, *domain.OutreachTarget) (string, error) {
	return "Hi, a short sample message.", nil
}

type dryLibrary struct{ art *domain.Artifact }

func (f *dryLibrary) ListArtifacts(context.Context, uuid.UUID, *uuid.UUID) ([]*domain.Artifact, error) {
	return []*domain.Artifact{f.art}, nil
}
func (f *dryLibrary) ReadText(context.Context, uuid.UUID, uuid.UUID, int64) (string, error) {
	return "# Project ideas\n- a sample idea", nil
}
func (f *dryLibrary) UpdateText(context.Context, uuid.UUID, uuid.UUID, string, bool, int64) (*domain.Artifact, error) {
	return f.art, nil
}
func (f *dryLibrary) CreateList(_ context.Context, _ uuid.UUID, title, _ string) (*domain.Artifact, error) {
	return &domain.Artifact{ID: uuid.New(), Title: title}, nil
}
func (f *dryLibrary) DeleteArtifact(context.Context, uuid.UUID, uuid.UUID) error { return nil }

type dryHosts struct{ host *domain.Host }

func (f *dryHosts) Overview(context.Context, uuid.UUID) (*service.HostOverview, error) {
	return &service.HostOverview{Hosts: []service.HostView{{Host: f.host, Live: true, Effective: f.host.Capabilities}}}, nil
}
func (f *dryHosts) SetPaused(_ context.Context, _, _ uuid.UUID, p bool) error {
	f.host.Paused = p
	return nil
}
func (f *dryHosts) SetGranted(_ context.Context, _, _ uuid.UUID, c []string) error {
	f.host.Capabilities = c
	return nil
}
func (f *dryHosts) Revoke(context.Context, uuid.UUID, uuid.UUID) error { return nil }

type dryProfile struct{ name string }

func (f *dryProfile) ManagerName(context.Context, uuid.UUID) (string, error) { return f.name, nil }
func (f *dryProfile) SetManagerName(_ context.Context, _ uuid.UUID, n string) error {
	f.name = n
	return nil
}
func (f *dryProfile) InboundCallsEnabled(context.Context, uuid.UUID) (bool, error)  { return false, nil }
func (f *dryProfile) SetInboundCallsEnabled(context.Context, uuid.UUID, bool) error { return nil }

type dryAliases map[string]string

func (f dryAliases) List(context.Context, uuid.UUID) (map[string]string, error) { return f, nil }
func (f dryAliases) Put(_ context.Context, _ uuid.UUID, a, t string) error      { f[a] = t; return nil }
func (f dryAliases) Delete(_ context.Context, _ uuid.UUID, a string) (bool, error) {
	_, ok := f[a]
	delete(f, a)
	return ok, nil
}

type dryNotes struct{}

func (dryNotes) After(context.Context, uuid.UUID, int64, int) ([]*domain.Notification, error) {
	return []*domain.Notification{{Kind: "reminder", Title: "Sample", Body: "A sample notification", CreatedAt: time.Now()}}, nil
}
func (dryNotes) LatestSeq(context.Context, uuid.UUID) (int64, error) { return 1, nil }

type dryResearch struct{ agent.ResearchAPI }

func (dryResearch) ListRuns(context.Context, uuid.UUID, int) ([]*domain.ResearchRun, error) {
	return []*domain.ResearchRun{{ID: uuid.New(), Objective: "Find pay at Acme", Status: "COMPLETED"}}, nil
}
func (dryResearch) GetRun(_ context.Context, _, id uuid.UUID) (*domain.ResearchRun, error) {
	return &domain.ResearchRun{ID: id, Objective: "Find pay at Acme", Status: "COMPLETED"}, nil
}
func (dryResearch) ListEvidenceForRun(context.Context, uuid.UUID, uuid.UUID) ([]*domain.EvidenceItem, error) {
	return nil, nil
}
func (dryResearch) StartRun(_ context.Context, u uuid.UUID, r domain.ResearchRequest) (*domain.ResearchRun, error) {
	return &domain.ResearchRun{ID: uuid.New(), UserID: u, Objective: r.Objective}, nil
}
func (dryResearch) PauseRun(context.Context, uuid.UUID, uuid.UUID) error  { return nil }
func (dryResearch) CancelRun(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (dryResearch) ResumeRun(_ context.Context, _, id uuid.UUID) (*domain.ResearchRun, error) {
	return &domain.ResearchRun{ID: id, Status: domain.PhasePaused}, nil
}

type dryPackages struct{}

func (dryPackages) PackageFor(_ context.Context, _, l uuid.UUID) (*domain.ApplicationPackage, error) {
	return &domain.ApplicationPackage{ID: uuid.New(), ListingID: l, ResumeFile: "resume.pdf", CoverLetter: "A sample cover letter."}, nil
}
func (dryPackages) GetPackage(_ context.Context, _, id uuid.UUID) (*domain.ApplicationPackage, error) {
	return &domain.ApplicationPackage{ID: id, ResumeFile: "resume.pdf"}, nil
}

type dryCompanies struct{}

func (dryCompanies) ListCompanySummaries(context.Context, uuid.UUID, int) ([]*repository.CompanySummary, error) {
	return []*repository.CompanySummary{{Company: domain.Company{ID: uuid.New(), Name: "Acme", Status: "active"}, OpenListings: 2}}, nil
}
func (dryCompanies) GetCompanyDetail(_ context.Context, _, id uuid.UUID) (*repository.CompanyDetail, error) {
	return &repository.CompanyDetail{Company: &domain.Company{ID: id, Name: "Acme"}}, nil
}

type dryTaskInfo struct{}

func (dryTaskInfo) GetEvents(context.Context, uuid.UUID) ([]*domain.TaskEvent, error) {
	return nil, nil
}
func (dryTaskInfo) GetAttempts(context.Context, uuid.UUID) ([]*domain.TaskAttempt, error) {
	return nil, nil
}
func (dryTaskInfo) Audit(context.Context, uuid.UUID, uuid.UUID) ([]domain.AuditEntry, error) {
	return nil, nil
}
func (dryTaskInfo) ScoreRows(context.Context, uuid.UUID, time.Time) ([]domain.ScoreRow, error) {
	return nil, nil
}

type dryArchive struct{}

func (dryArchive) ArchiveTasks(_ context.Context, _ uuid.UUID, ids []uuid.UUID, _ time.Time) (int64, error) {
	return int64(len(ids)), nil
}
func (dryArchive) UnarchiveTask(context.Context, uuid.UUID, uuid.UUID) (int64, error) { return 1, nil }
func (dryArchive) DeleteArchived(context.Context, uuid.UUID) (int64, error)           { return 0, nil }

type dryDSA struct{}

func (dryDSA) Today(context.Context, uuid.UUID) ([]*domain.DsaItem, error) {
	return []*domain.DsaItem{{Slug: "two-sum", Title: "Two Sum", Difficulty: "Easy", State: "assigned"}}, nil
}
func (dryDSA) Sync(context.Context, uuid.UUID) ([]string, error)   { return nil, nil }
func (dryDSA) MarkSolved(context.Context, uuid.UUID, string) error { return nil }

type dryBrief struct{}

func (dryBrief) Build(context.Context, uuid.UUID) (domain.Brief, error) {
	return domain.Brief{Text: "Sample brief: one deadline today."}, nil
}

type dryApprovalEdit struct{}

func (dryApprovalEdit) EditApproval(_ context.Context, id, _ uuid.UUID, _ string) (*domain.Approval, error) {
	return &domain.Approval{ID: id}, nil
}

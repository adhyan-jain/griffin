package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/agent"
	"github.com/griffin/core/internal/config"
	"github.com/griffin/core/internal/database"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/research"
	"github.com/griffin/core/internal/service"
	"github.com/griffin/core/internal/storage"
)

// chatExtras are the long-lived services main builds that the second tool batch wraps.
type chatExtras struct {
	Research     *research.Engine
	ResearchRepo research.ResearchStore
	Briefer      *service.Briefer
	Dsa          *service.DsaCoach
}

// chatMoreDeps wires the second tool batch to the same repositories and services the HTTP handlers use.
func chatMoreDeps(cfg *config.Config, db database.Querier, engine *service.TaskEngine, store *storage.StorageService,
	listings *repository.OpportunityRepository, owner service.OwnerProfileSource, x chatExtras) agent.MoreDeps {
	people := repository.NewPeopleRepository(db)
	outreach := repository.NewOutreachRepository(db)
	profiles := repository.NewPostgresProfileRepository(db)
	tasks := repository.NewPostgresTaskRepository(db)
	m := agent.MoreDeps{
		People: people, Holder: engine, Profiles: owner, GradCutoff: cfg.AlumniGraduationCutoff,
		VIT:      service.NewVITCampaignService(db, people, repository.NewCampaignRepository(db)),
		Outreach: outreach,
		Profile:  profiles, Aliases: repository.NewPostgresAliasRepository(db),
		Policy:        service.NewPolicyService(repository.NewPostgresPolicyRepository(db)),
		Notifications: repository.NewPostgresNotificationRepository(db),
		Archive:       tasks, DeleteTask: engine.DeleteTask, ApprovalEdit: engine,
		TaskInfo: taskInfo{tasks, repository.NewReportRepository(db)},
	}
	if cfg.HostsEnabled {
		m.Hosts = service.NewHostService(repository.NewPostgresHostRepository(db), tasks, time.Duration(max(cfg.LeaseDurationSeconds, 60))*time.Second, slog.Default())
	}
	if d := outreachDrafterFor(cfg); d != nil {
		m.Drafter = d
	}
	if store != nil {
		m.Library = library{repository.NewPostgresArtifactRepository(db), store}
		m.Importer = service.NewAlumniPeople(store, people)
	}
	if listings != nil {
		m.Packages, m.Companies = listings, listings
		m.SourceHealth = listings.SourceHealth
	}
	if x.Research != nil && x.ResearchRepo != nil {
		m.Research = researchJoin{x.Research, x.ResearchRepo}
	}
	if x.Briefer != nil {
		m.Brief = x.Briefer
	}
	if x.Dsa != nil {
		m.DSA = x.Dsa
	}
	inbox, disc, inbound := repository.NewInboxRepository(db), repository.NewDiscoveryRepository(db), repository.NewInboundEventRepository(db)
	m.Assessments = func(ctx context.Context, u uuid.UUID) ([]*domain.AssessmentRecord, error) {
		return inbox.ListActiveAssessments(ctx, u, nil)
	}
	m.DiscoveredJobs = disc.ListDiscoveredJobs
	m.InboundEvents = func(ctx context.Context, u uuid.UUID, limit int, actionable bool) ([]*domain.InboundEvent, error) {
		if actionable {
			return inbound.ListActionable(ctx, u, limit)
		}
		return inbound.List(ctx, u, limit)
	}
	return m
}

func outreachDrafterFor(cfg *config.Config) agent.CampaignDrafter {
	if strings.TrimSpace(cfg.GeminiAPIKey) == "" {
		return nil
	}
	return outreachDrafter(cfg)
}

// taskInfo joins the task repository (events, attempts) and the report repository (audit, scorecard).
type taskInfo struct {
	*repository.PostgresTaskRepository
	*repository.ReportRepository
}

// researchJoin joins the research engine (runs) and its store (reads).
type researchJoin struct {
	*research.Engine
	research.ResearchStore
}

// library joins the artifact repository (listing) and the storage service (text, create, delete), as the handlers do.
type library struct {
	repo repository.ArtifactRepository
	st   *storage.StorageService
}

func (l library) ListArtifacts(ctx context.Context, u uuid.UUID, task *uuid.UUID) ([]*domain.Artifact, error) {
	return l.repo.ListArtifacts(ctx, u, task)
}
func (l library) ReadText(ctx context.Context, u, id uuid.UUID, max int64) (string, error) {
	return l.st.ReadText(ctx, u, id, max)
}
func (l library) UpdateText(ctx context.Context, id, u uuid.UUID, text string, app bool, max int64) (*domain.Artifact, error) {
	return l.st.UpdateText(ctx, id, u, text, app, max)
}
func (l library) DeleteArtifact(ctx context.Context, id, u uuid.UUID) error {
	return l.st.DeleteArtifact(ctx, id, u)
}
func (l library) CreateList(ctx context.Context, u uuid.UUID, title, content string) (*domain.Artifact, error) {
	body := strings.TrimRight(content, "\n ")
	if body == "" {
		body = "# " + title
	}
	return l.st.SaveArtifact(ctx, storage.SaveArtifactInput{
		UserID: u, Title: title, Filename: storage.SanitizeFilename(strings.ToLower(strings.ReplaceAll(title, " ", "-"))) + ".md",
		ArtifactType: "MARKDOWN", ContentType: "text/markdown; charset=utf-8", Body: strings.NewReader(body + "\n"),
		Metadata: json.RawMessage(`{"kind":"` + storage.ListKind + `"}`),
	})
}

package main

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/agent"
	"github.com/griffin/core/internal/api/handlers"
	"github.com/griffin/core/internal/config"
	"github.com/griffin/core/internal/database"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/hands/web"
	"github.com/griffin/core/internal/provider/gemini"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/service"
	"github.com/griffin/core/internal/storage"
)

// buildChatAgent makes JJ's tool-using chat agent, or returns nil (the old chat path stays) when no Gemini key is set.
// External agent actions run from the engine's approval hook, so they execute only what the owner approved.
func buildChatAgent(cfg *config.Config, repoDB database.Querier, engine *service.TaskEngine, approvalRepo repository.ApprovalRepository,
	schedules repository.ScheduleRepository, reminders *service.Reminders, profiles *repository.OwnerProfileRepository, store *storage.StorageService, listings *repository.OpportunityRepository) handlers.ChatAgent {
	if strings.TrimSpace(cfg.GeminiAPIKey) == "" {
		return nil
	}
	pg := agent.NewPGStore(repoDB)
	deps := agent.Deps{
		Tasks: engine, Routines: schedules, Reminders: reminders,
		Approvals: chatApprovals{approvalRepo, engine},
		Search:    web.SearcherFromEnv(), Fetch: web.NewFetcher(web.FetcherConfig{}), Store: pg,
		CheckLink: func(ctx context.Context, link string) service.PostingState {
			return service.CheckPosting(ctx, nil, link)
		},
	}
	if listings != nil {
		deps.Listings = listings
	}
	if store != nil { // a nil *StorageService inside an interface would pass the nil check and panic
		lists := &service.TrackerLists{Store: store}
		if listings != nil {
			lists.Sync = &service.ListingSync{Listings: listings}
			deps.ListingsChanged = func(ctx context.Context, user uuid.UUID) { _ = lists.WriteListingsList(ctx, user) }
		}
		deps.Trackers, deps.Vault = lists, store
	}
	tools, err := agent.NewRegistry(agent.NewTools(deps)...)
	if err != nil {
		panic("agent tools: " + err.Error()) // a duplicate or incomplete tool is a coding error
	}
	tools.Audit = engine.RecordOwnerCommand
	engine.OnActionDecided(tools.Decided)

	owner := service.OwnerProfileWithLibrary{Structured: profiles}
	if store != nil {
		owner.Library = (&service.TrackerLists{Store: store}).ProfileText
	}
	model := &gemini.Client{APIKey: cfg.GeminiAPIKey, Model: cfg.GeminiAgentModel, MaxOutputTokens: 1500, Fallback: gemini.DefaultFallbackModel}
	summariser := &gemini.Client{APIKey: cfg.GeminiAPIKey, Model: gemini.DefaultModel, MaxOutputTokens: 400}
	return &agent.Agent{
		Model: agent.GeminiModel{Client: model}, Store: pg, Registry: tools, Approvals: engine,
		Profile: func(ctx context.Context, user uuid.UUID) string {
			p, err := owner.Latest(ctx, user)
			if err != nil || p == nil {
				return ""
			}
			return p.ContextBlock()
		},
		Summarize: func(ctx context.Context, previous string, turns []agent.Turn) (string, error) {
			var b strings.Builder
			for _, t := range turns {
				b.WriteString(t.Role + ": " + t.Text + "\n")
			}
			return summariser.Chat(ctx, "Update the running summary of a conversation between the owner and JJ. Keep names, decisions, open questions and ids the owner cares about; drop small talk. Reply with the new summary only, under 150 words.",
				[]gemini.Turn{{Role: "user", Text: "Previous summary:\n" + previous + "\n\nNew messages:\n" + b.String()}})
		},
	}
}

// chatApprovals joins the approval list (a repository) and the decision (the engine) for the agent's tools.
type chatApprovals struct {
	repo   repository.ApprovalRepository
	engine *service.TaskEngine
}

func (v chatApprovals) ListApprovals(ctx context.Context, user uuid.UUID, st *domain.ApprovalStatus) ([]*domain.Approval, error) {
	return v.repo.ListApprovals(ctx, user, st)
}

func (v chatApprovals) RespondApproval(ctx context.Context, id, user uuid.UUID, ok bool, reason string) (*domain.Approval, error) {
	return v.engine.RespondApproval(ctx, id, user, ok, reason)
}

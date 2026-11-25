package research

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
)

// ResearchStore defines the persistence interface required by the research engine.
type ResearchStore interface {
	CreateRun(ctx context.Context, run *domain.ResearchRun) error
	GetRun(ctx context.Context, userID, runID uuid.UUID) (*domain.ResearchRun, error)
	UpdateRun(ctx context.Context, run *domain.ResearchRun) error
	ListRuns(ctx context.Context, userID uuid.UUID, limit int) ([]*domain.ResearchRun, error)
	RecordStep(ctx context.Context, step *domain.ResearchStep) error
	ListSteps(ctx context.Context, userID, runID uuid.UUID) ([]*domain.ResearchStep, error)
	AddSource(ctx context.Context, s *domain.ResearchSource) error
	AddEvidence(ctx context.Context, e *domain.EvidenceItem) error
	ListEvidenceForRun(ctx context.Context, userID, runID uuid.UUID) ([]*domain.EvidenceItem, error)
}

// KnowledgeStore defines the persistence interface for canonical entities and graph relationships.
type KnowledgeStore interface {
	UpsertEntity(ctx context.Context, e *domain.CanonicalEntity) error
	GetEntity(ctx context.Context, userID, id uuid.UUID) (*domain.CanonicalEntity, error)
	FindEntityByKey(ctx context.Context, userID uuid.UUID, kind domain.EntityKind, key string) (*domain.CanonicalEntity, error)
	ListEntities(ctx context.Context, userID uuid.UUID, kind domain.EntityKind, limit int) ([]*domain.CanonicalEntity, error)
	AddAlias(ctx context.Context, a *domain.EntityAlias) error
	FindEntityByAlias(ctx context.Context, userID uuid.UUID, alias string) (*domain.CanonicalEntity, error)
	UpsertRelationship(ctx context.Context, rel *domain.EntityRelationship) error
	ListRelationships(ctx context.Context, userID, entityID uuid.UUID) ([]*domain.EntityRelationship, error)
	RecordObservation(ctx context.Context, obs *domain.EntityObservation) error
	ListObservations(ctx context.Context, userID, entityID uuid.UUID) ([]*domain.EntityObservation, error)
}

// Engine orchestrates the multi-phase, iterative deep research lifecycle.
type Engine struct {
	Repo      ResearchStore
	Knowledge KnowledgeStore
	Tools     *ToolGateway
	Router    *Router
	Expander  *QueryExpander
	Logger    *slog.Logger
	Now       func() time.Time
}

// NewEngine constructs a new Deep Research Engine.
func NewEngine(
	repo ResearchStore,
	knowledge KnowledgeStore,
	tools *ToolGateway,
	router *Router,
	logger *slog.Logger,
) *Engine {
	if logger == nil {
		logger = slog.Default()
	}
	return &Engine{
		Repo:      repo,
		Knowledge: knowledge,
		Tools:     tools,
		Router:    router,
		Expander:  NewQueryExpander(),
		Logger:    logger,
		Now:       time.Now,
	}
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

// StartRun initializes a new research run and executes its planning phase.
func (e *Engine) StartRun(ctx context.Context, userID uuid.UUID, req domain.ResearchRequest) (*domain.ResearchRun, error) {
	if req.Depth == "" {
		req.Depth = domain.DepthStandard
	}
	if req.Playbook == "" {
		req.Playbook = "generic"
	}

	budgets := domain.BudgetDefaults(req.Depth)
	if req.MaxSearches > 0 {
		budgets["max_searches"] = req.MaxSearches
	}
	if req.MaxPages > 0 {
		budgets["max_pages"] = req.MaxPages
	}
	if req.MaxModelCalls > 0 {
		budgets["max_model_calls"] = req.MaxModelCalls
	}
	if req.MaxTimeSeconds > 0 {
		budgets["max_time_seconds"] = req.MaxTimeSeconds
	}

	run := &domain.ResearchRun{
		UserID:              userID,
		Playbook:            req.Playbook,
		Objective:           req.Objective,
		Depth:               req.Depth,
		Status:              domain.PhaseRequestReceived,
		ContextData:         req.Context,
		ConstraintsList:     req.Constraints,
		BudgetConfig:        budgets,
		ProgressMetrics:     map[string]int{"searches_done": 0, "pages_inspected": 0, "model_calls": 0, "findings_count": 0},
		Findings:            []domain.ResearchFinding{},
		UnresolvedQuestions: req.ResearchQuestions,
		ReportMarkdown:      "",
	}

	if err := e.Repo.CreateRun(ctx, run); err != nil {
		return nil, fmt.Errorf("create run: %w", err)
	}

	e.recordStep(ctx, run, 1, domain.PhaseRequestReceived, "run_initialized", map[string]any{"objective": req.Objective})

	// Planning
	run.Status = domain.PhasePlanning
	planResp, err := e.Router.AskPlan(ctx, req.Objective, req.Constraints, string(req.Depth))
	if err != nil {
		e.Logger.Warn("Planning model failed, using fallback plan", "error", err)
		run.Plan = domain.ResearchPlan{
			Questions: []domain.ResearchQuestion{
				{ID: "q1", Question: req.Objective, Category: "overview", Status: "pending"},
			},
			InitialQueries: []string{req.Objective},
		}
	} else {
		run.ProgressMetrics["model_calls"]++
		var questions []domain.ResearchQuestion
		for _, q := range planResp.Questions {
			questions = append(questions, domain.ResearchQuestion{
				ID:       q.ID,
				Question: q.Question,
				Category: q.Category,
				Status:   "pending",
			})
		}
		if len(req.ResearchQuestions) > 0 {
			for i, rq := range req.ResearchQuestions {
				questions = append(questions, domain.ResearchQuestion{
					ID:       fmt.Sprintf("user_q%d", i+1),
					Question: rq,
					Category: "user_specified",
					Status:   "pending",
				})
			}
		}
		run.Plan = domain.ResearchPlan{
			Questions:      questions,
			InitialQueries: planResp.InitialQueries,
		}
	}

	run.Status = domain.PhaseSourceDiscovery
	if err := e.Repo.UpdateRun(ctx, run); err != nil {
		return nil, fmt.Errorf("update run after planning: %w", err)
	}

	e.recordStep(ctx, run, 2, domain.PhasePlanning, "plan_generated", map[string]any{
		"questions_count": len(run.Plan.Questions),
		"initial_queries": run.Plan.InitialQueries,
	})

	return run, nil
}

// ExecuteCycle runs one iterative research cycle (search -> inspect -> extract -> gap analysis -> synthesis).
func (e *Engine) ExecuteCycle(ctx context.Context, userID, runID uuid.UUID) (*domain.ResearchRun, error) {
	run, err := e.Repo.GetRun(ctx, userID, runID)
	if err != nil {
		return nil, err
	}
	if run.Status.IsTerminal() || run.Status == domain.PhasePaused {
		return run, nil
	}

	if run.ProgressMetrics == nil {
		run.ProgressMetrics = make(map[string]int)
	}
	if run.BudgetConfig == nil {
		run.BudgetConfig = make(map[string]int)
	}

	stepIndex := 3
	maxSearches := run.BudgetConfig["max_searches"]
	maxPages := run.BudgetConfig["max_pages"]

	// 1. Gather queries to execute
	queries := run.Plan.InitialQueries
	var discoveredSources []SearchHit

	for _, q := range queries {
		if run.ProgressMetrics["searches_done"] >= maxSearches {
			break
		}
		if e.Expander.HasExecuted(q) {
			continue
		}
		hits, err := e.Tools.Search(ctx, q, 5)
		e.Expander.MarkExecuted(q)
		run.ProgressMetrics["searches_done"]++
		if err != nil {
			e.Logger.Warn("Search error", "query", q, "error", err)
			continue
		}
		discoveredSources = append(discoveredSources, hits...)
	}

	e.recordStep(ctx, run, stepIndex, domain.PhaseSourceDiscovery, "searches_executed", map[string]any{
		"hits_found": len(discoveredSources),
	})
	stepIndex++

	// 2. Register discovered sources and inspect content
	run.Status = domain.PhaseContentInspection
	var pages []*FetchedContent
	seenURLs := make(map[string]bool)

	for _, hit := range discoveredSources {
		if seenURLs[hit.URL] {
			continue
		}
		seenURLs[hit.URL] = true

		srcType, relTier := ClassifySourceType(hit.URL)
		sourceRecord := &domain.ResearchSource{
			UserID:          run.UserID,
			RunID:           &run.ID,
			URL:             hit.URL,
			Title:           hit.Title,
			Domain:          hit.Domain,
			SourceType:      srcType,
			ReliabilityTier: relTier,
		}
		_ = e.Repo.AddSource(ctx, sourceRecord)

		if run.ProgressMetrics["pages_inspected"] >= maxPages {
			continue
		}

		page, err := e.Tools.Fetch(ctx, hit.URL)
		if err != nil {
			e.Logger.Debug("Page fetch skipped", "url", hit.URL, "error", err)
			continue
		}
		run.ProgressMetrics["pages_inspected"]++
		pages = append(pages, page)
	}

	e.recordStep(ctx, run, stepIndex, domain.PhaseContentInspection, "pages_inspected", map[string]any{
		"pages_count": len(pages),
	})
	stepIndex++

	// 3. Information Extraction & Claim Verification
	run.Status = domain.PhaseExtraction
	var qList []string
	for _, q := range run.Plan.Questions {
		qList = append(qList, q.Question)
	}

	var newLeads []string
	for _, page := range pages {
		ext, err := e.Router.ExtractClaims(ctx, run.Objective, page.URL, page.Text, qList)
		if err != nil {
			e.Logger.Warn("Extraction error", "url", page.URL, "error", err)
			continue
		}
		run.ProgressMetrics["model_calls"]++

		for _, f := range ext.Findings {
			evidence := &domain.EvidenceItem{
				UserID:           run.UserID,
				RunID:            &run.ID,
				Claim:            f.Claim,
				ExtractedValue:   f.ExtractedValue,
				ExcerptQuote:     f.Quote,
				SourceURL:        page.URL,
				SourceDomain:     page.Domain,
				Confidence:       f.Confidence,
				Status:           domain.EvidenceSupported,
				ExtractionMethod: "model_extraction",
			}
			_ = e.Repo.AddEvidence(ctx, evidence)

			run.Findings = append(run.Findings, domain.ResearchFinding{
				ID:              uuid.New().String(),
				Topic:           f.Topic,
				Claim:           f.Claim,
				ExtractedValue:  f.ExtractedValue,
				EvidenceID:      &evidence.ID,
				SourceURL:       page.URL,
				Confidence:      f.Confidence,
				IsContradiction: f.IsContradiction,
			})
		}
		newLeads = append(newLeads, ext.NewLeads...)
	}
	run.ProgressMetrics["findings_count"] = len(run.Findings)

	e.recordStep(ctx, run, stepIndex, domain.PhaseExtraction, "claims_extracted", map[string]any{
		"findings_count": len(run.Findings),
		"leads_count":    len(newLeads),
	})
	stepIndex++

	// 4. Gap Analysis & Follow-Up Leads
	run.Status = domain.PhaseGapAnalysis
	var unresolved []string
	for _, q := range run.Plan.Questions {
		answered := false
		for _, f := range run.Findings {
			if strings.Contains(strings.ToLower(f.Claim), strings.ToLower(q.Category)) ||
				strings.Contains(strings.ToLower(f.Topic), strings.ToLower(q.Category)) {
				answered = true
				break
			}
		}
		if !answered {
			unresolved = append(unresolved, q.Question)
		}
	}
	run.UnresolvedQuestions = unresolved

	// If budget remains, follow up on 2 top leads
	if run.ProgressMetrics["searches_done"] < maxSearches && len(newLeads) > 0 {
		run.Status = domain.PhaseFollowUpResearch
		for i, lead := range newLeads {
			if i >= 2 {
				break
			}
			followUps := e.Expander.ExpandLead(lead, run.Objective)
			for _, fq := range followUps {
				if run.ProgressMetrics["searches_done"] < maxSearches && !e.Expander.HasExecuted(fq) {
					hits, _ := e.Tools.Search(ctx, fq, 3)
					e.Expander.MarkExecuted(fq)
					run.ProgressMetrics["searches_done"]++
					for _, h := range hits {
						if !seenURLs[h.URL] && run.ProgressMetrics["pages_inspected"] < maxPages {
							seenURLs[h.URL] = true
							if p, err := e.Tools.Fetch(ctx, h.URL); err == nil {
								run.ProgressMetrics["pages_inspected"]++
								pages = append(pages, p)
							}
						}
					}
				}
			}
		}
	}

	// 5. Synthesis
	run.Status = domain.PhaseSynthesis
	findingsJSON, _ := json.Marshal(run.Findings)
	reportMD, structured, err := e.Router.SynthesizeReport(ctx, run.Objective, findingsJSON, run.UnresolvedQuestions)
	if err != nil {
		e.Logger.Warn("Report synthesis failed, generating deterministic summary", "error", err)
		run.ReportMarkdown = fmt.Sprintf("# Research Report: %s\n\nTotal findings: %d\nUnresolved questions: %d",
			run.Objective, len(run.Findings), len(run.UnresolvedQuestions))
	} else {
		run.ProgressMetrics["model_calls"]++
		run.ReportMarkdown = reportMD
		run.ReportStructured = structured
	}

	// 6. Persistence & Completion
	run.Status = domain.PhaseCompleted
	now := e.now()
	run.CompletedAt = &now

	if err := e.Repo.UpdateRun(ctx, run); err != nil {
		return nil, fmt.Errorf("finalize run: %w", err)
	}

	e.recordStep(ctx, run, stepIndex, domain.PhaseCompleted, "run_completed", map[string]any{
		"final_findings_count": len(run.Findings),
		"unresolved_count":     len(run.UnresolvedQuestions),
	})

	return run, nil
}

// PauseRun pauses an in-flight research run.
func (e *Engine) PauseRun(ctx context.Context, userID, runID uuid.UUID) error {
	run, err := e.Repo.GetRun(ctx, userID, runID)
	if err != nil {
		return err
	}
	if run.Status.IsTerminal() {
		return fmt.Errorf("cannot pause a completed or failed run")
	}
	run.Status = domain.PhasePaused
	return e.Repo.UpdateRun(ctx, run)
}

// ResumeRun resumes a paused run.
func (e *Engine) ResumeRun(ctx context.Context, userID, runID uuid.UUID) (*domain.ResearchRun, error) {
	run, err := e.Repo.GetRun(ctx, userID, runID)
	if err != nil {
		return nil, err
	}
	if run.Status != domain.PhasePaused {
		return run, nil
	}
	run.Status = domain.PhaseSourceDiscovery
	if err := e.Repo.UpdateRun(ctx, run); err != nil {
		return nil, err
	}
	return e.ExecuteCycle(ctx, userID, runID)
}

func (e *Engine) recordStep(ctx context.Context, run *domain.ResearchRun, idx int, phase domain.ResearchPhase, action string, output map[string]any) {
	step := &domain.ResearchStep{
		RunID:      run.ID,
		UserID:     run.UserID,
		StepIndex:  idx,
		Phase:      phase,
		Action:     action,
		OutputData: output,
		Status:     "completed",
		CreatedAt:  e.now(),
	}
	_ = e.Repo.RecordStep(ctx, step)
}

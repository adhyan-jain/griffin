package research

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/budget"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/playbooks"
)

// ResearchStore defines the persistence interface required by the research engine.
type ResearchStore interface {
	CreateRun(ctx context.Context, run *domain.ResearchRun) error
	GetRun(ctx context.Context, userID, runID uuid.UUID) (*domain.ResearchRun, error)
	// UpdateRun writes the run only while it is active and unchanged since it was read; otherwise it returns
	// domain.ErrResearchRunInactive and leaves the run alone.
	UpdateRun(ctx context.Context, run *domain.ResearchRun) error
	// TransitionRun moves a run to a new status only if its current status is one of from; it reports whether it did.
	TransitionRun(ctx context.Context, userID, runID uuid.UUID, to domain.ResearchPhase, from []domain.ResearchPhase, stopReason string, errMsg *string) (bool, error)
	ListRuns(ctx context.Context, userID uuid.UUID, limit int) ([]*domain.ResearchRun, error)
	// RecordStep upserts a step by (run, step_index) and returns domain.ErrResearchStepDone for a step already done.
	RecordStep(ctx context.Context, step *domain.ResearchStep) error
	ListSteps(ctx context.Context, userID, runID uuid.UUID) ([]*domain.ResearchStep, error)
	// AddSource and AddEvidence are idempotent: the same page (canonical URL) or the same claim from the same page
	// is stored once per run, and the existing row's id is returned.
	AddSource(ctx context.Context, s *domain.ResearchSource) error
	AddEvidence(ctx context.Context, e *domain.EvidenceItem) error
	SetEvidenceStatus(ctx context.Context, userID, evidenceID uuid.UUID, status domain.EvidenceStatus) error
	ListEvidenceForRun(ctx context.Context, userID, runID uuid.UUID) ([]*domain.EvidenceItem, error)
}

// KnowledgeStore defines the persistence interface for canonical entities and graph relationships.
type KnowledgeStore interface {
	UpsertEntity(ctx context.Context, e *domain.CanonicalEntity) error
	GetEntity(ctx context.Context, userID, id uuid.UUID) (*domain.CanonicalEntity, error)
	FindEntityByKey(ctx context.Context, userID uuid.UUID, kind domain.EntityKind, key string) (*domain.CanonicalEntity, error)
	ListEntities(ctx context.Context, userID uuid.UUID, kind domain.EntityKind, limit int) ([]*domain.CanonicalEntity, error)
	AddAlias(ctx context.Context, a *domain.EntityAlias) error
	FindEntityByAliasKind(ctx context.Context, userID uuid.UUID, kind domain.EntityKind, alias string) (*domain.CanonicalEntity, error)
	UpsertRelationship(ctx context.Context, rel *domain.EntityRelationship) error
	ListRelationships(ctx context.Context, userID, entityID uuid.UUID) ([]*domain.EntityRelationship, error)
	RecordObservation(ctx context.Context, obs *domain.EntityObservation) error
	ListObservations(ctx context.Context, userID, entityID uuid.UUID) ([]*domain.EntityObservation, error)
}

const (
	searchHitLimit     = 5
	maxURLsPerSearch   = 5
	queriesPerRound    = 3
	maxFindingsPerPage = 20
	maxFindingsToModel = 80
	maxRawTextBytes    = 100_000
	maxQueryRunes      = 200
	maxStepsPerCall    = 500
)

// Engine runs the iterative research lifecycle as a resumable state machine. All of a run's state lives in the
// database (see domain.ResearchRun), so any worker can advance it one step at a time and a crash loses at most
// the step in flight. The engine keeps no per-run state of its own and is safe to share between runs.
type Engine struct {
	Repo      ResearchStore
	Knowledge KnowledgeStore
	Tools     ToolBackend
	Router    *Router
	Logger    *slog.Logger
	Now       func() time.Time
	// Playbooks adds domain wording and result handling for the playbooks a request may name. The engine works
	// with none: a nil registry, or a run whose playbook has no entry, is the generic engine.
	Playbooks playbooks.Registry
}

// NewEngine constructs a new Deep Research Engine.
func NewEngine(
	repo ResearchStore,
	knowledge KnowledgeStore,
	tools ToolBackend,
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

func (e *Engine) logger() *slog.Logger {
	if e.Logger != nil {
		return e.Logger
	}
	return slog.Default()
}

// activePhases are the statuses of a run that can still be advanced.
var activePhases = []domain.ResearchPhase{
	domain.PhaseRequestReceived, domain.PhaseContextLoading, domain.PhaseObjectiveAnalysis, domain.PhasePlanning,
	domain.PhaseSourceDiscovery, domain.PhaseInformationRetrieval, domain.PhaseContentInspection, domain.PhaseExtraction,
	domain.PhaseEvidenceValidation, domain.PhaseGapAnalysis, domain.PhaseFollowUpResearch, domain.PhaseSynthesis,
	domain.PhasePersistence,
}

func isActive(p domain.ResearchPhase) bool {
	for _, a := range activePhases {
		if a == p {
			return true
		}
	}
	return false
}

// StartRun validates the request and stores a new run, ready for its first step. It makes no model or network
// call: the plan is the run's first step, taken by the research routine, so starting a run is fast and a failure
// to plan is recorded on the run instead of lost in a request.
func (e *Engine) StartRun(ctx context.Context, userID uuid.UUID, req domain.ResearchRequest) (*domain.ResearchRun, error) {
	if e == nil || e.Repo == nil {
		return nil, errors.New("research engine is not configured")
	}
	if err := req.Normalize(); err != nil {
		return nil, err
	}

	budgets := domain.BudgetDefaults(req.Depth)
	for key, v := range map[string]int{"max_searches": req.MaxSearches, "max_pages": req.MaxPages,
		"max_model_calls": req.MaxModelCalls, "max_time_seconds": req.MaxTimeSeconds} {
		if v > 0 {
			budgets[key] = v
		}
	}

	rqs := req.ResearchQuestions
	if pb := e.Playbooks.For(req.Playbook); pb != nil && len(rqs) == 0 {
		rqs = pb.DefaultQuestions()
	}
	var questions []domain.ResearchQuestion
	for i, rq := range rqs {
		questions = append(questions, domain.ResearchQuestion{
			ID: fmt.Sprintf("user_q%d", i+1), Question: rq, Category: "user_specified", Status: "pending",
		})
	}
	run := &domain.ResearchRun{
		UserID:          userID,
		Playbook:        req.Playbook,
		Objective:       req.Objective,
		Depth:           req.Depth,
		Status:          domain.PhaseRequestReceived,
		Plan:            domain.ResearchPlan{Questions: questions, InitialQueries: []string{}},
		ContextData:     req.Context,
		ConstraintsList: req.Constraints,
		BudgetConfig:    budgets,
		ProgressMetrics: map[string]int{"searches_done": 0, "pages_inspected": 0, "model_calls": 0, "findings_count": 0, "time_ms": 0},
		Findings:        []domain.ResearchFinding{},
		NextStepIndex:   1,
	}
	refreshQuestions(run)

	if err := e.Repo.CreateRun(ctx, run); err != nil {
		return nil, fmt.Errorf("create run: %w", err)
	}
	return run, nil
}

// RunSteps advances a run step by step until it finishes, is paused or cancelled, or the context's deadline is
// near (the last 3 seconds are kept for persisting). The run is re-read before every step, so a pause or cancel
// takes effect at the next step boundary. The returned run is its latest state; when it is still active, the
// caller should schedule another invocation.
func (e *Engine) RunSteps(ctx context.Context, userID, runID uuid.UUID) (*domain.ResearchRun, domain.ResearchAdvance, error) {
	var adv domain.ResearchAdvance
	if e == nil || e.Repo == nil {
		return nil, adv, errors.New("research engine is not configured")
	}
	work, cancel := budget.Work(ctx)
	defer cancel()

	stale := 0
	for i := 0; i < maxStepsPerCall; i++ {
		run, err := e.Repo.GetRun(ctx, userID, runID)
		if err != nil {
			return nil, adv, err
		}
		if !isActive(run.Status) || ctx.Err() != nil {
			return run, adv, nil
		}
		// Always take the first step, so every invocation makes progress; start later ones only with time left.
		if adv.Steps > 0 && budget.Short(work) {
			return run, adv, nil
		}

		var stepErr error
		if e.preflight(run) {
			stepErr = e.Repo.UpdateRun(ctx, run)
		} else {
			stepErr = e.step(work, ctx, run)
		}
		switch {
		case stepErr == nil:
			adv.Steps++
		case errors.Is(stepErr, domain.ErrResearchStepDone):
			// Another worker already finished this step: read the run again and carry on from there.
			if stale++; stale > 5 {
				adv.Contended = true
				return run, adv, nil
			}
		case errors.Is(stepErr, domain.ErrResearchRunInactive):
			latest, gerr := e.Repo.GetRun(ctx, userID, runID)
			if gerr != nil {
				return nil, adv, gerr
			}
			adv.Contended = isActive(latest.Status)
			return latest, adv, nil
		default:
			return run, adv, stepErr
		}
	}
	run, err := e.Repo.GetRun(ctx, userID, runID)
	return run, adv, err
}

type action string

const (
	actPlan   action = "plan"
	actSearch action = "search"
	actFetch  action = "fetch_extract"
	actGap    action = "gap_analysis"
	actSynth  action = "synthesis"
)

// nextAction is the next step, derived only from the run's persisted state.
func nextAction(run *domain.ResearchRun) action {
	switch {
	case run.Round == 0:
		return actPlan
	case len(run.PendingQueries) > 0:
		return actSearch
	case len(run.PendingURLs) > 0:
		return actFetch
	case run.StopReason == "":
		return actGap
	default:
		return actSynth
	}
}

func phaseOf(a action, run *domain.ResearchRun) domain.ResearchPhase {
	switch a {
	case actPlan:
		return domain.PhasePlanning
	case actSearch:
		if run.Round > 1 {
			return domain.PhaseFollowUpResearch
		}
		return domain.PhaseSourceDiscovery
	case actFetch:
		return domain.PhaseExtraction
	case actGap:
		return domain.PhaseGapAnalysis
	default:
		return domain.PhaseSynthesis
	}
}

func ensureMaps(run *domain.ResearchRun) {
	if run.ProgressMetrics == nil {
		run.ProgressMetrics = map[string]int{}
	}
	if run.BudgetConfig == nil {
		run.BudgetConfig = map[string]int{}
	}
	if run.NextStepIndex < 1 {
		run.NextStepIndex = 1
	}
}

// limit is a budget of the run; a run without one uses its depth's default.
func limit(run *domain.ResearchRun, key string) int {
	if v := run.BudgetConfig[key]; v > 0 {
		return v
	}
	return domain.BudgetDefaults(run.Depth)[key]
}

// modelOK reports whether another model call fits the budget. Calls other than the final synthesis leave one call
// in reserve for it.
func modelOK(run *domain.ResearchRun, forSynthesis bool) bool {
	need := 2
	if forSynthesis {
		need = 1
	}
	return run.ProgressMetrics["model_calls"]+need <= limit(run, "max_model_calls")
}

func timeUp(run *domain.ResearchRun) bool {
	return run.ProgressMetrics["time_ms"] >= limit(run, "max_time_seconds")*1000
}

// stopRun records why the run stops searching and drops the work that can no longer be done. The first reason wins.
func stopRun(run *domain.ResearchRun, reason string, dropQueries, dropURLs bool) {
	if run.StopReason == "" {
		run.StopReason = reason
	}
	if dropQueries {
		run.PendingQueries = nil
	}
	if dropURLs {
		run.PendingURLs = nil
	}
	if run.Round == 0 {
		run.Round = 1
	}
}

// preflight enforces the budgets before a step starts. When one is used up it changes the run (a stop reason,
// work dropped) and reports true; the caller saves the run and asks again.
func (e *Engine) preflight(run *domain.ResearchRun) bool {
	ensureMaps(run)
	a := nextAction(run)
	switch {
	case a == actSynth:
		return false
	case timeUp(run):
		stopRun(run, domain.StopBudgetTime, true, true)
	case a == actPlan && !modelOK(run, false):
		stopRun(run, domain.StopBudgetModelCalls, true, true)
	case a == actSearch && run.ProgressMetrics["searches_done"] >= limit(run, "max_searches"):
		stopRun(run, domain.StopBudgetSearches, true, false)
	case a == actFetch && run.ProgressMetrics["pages_inspected"] >= limit(run, "max_pages"):
		stopRun(run, domain.StopBudgetPages, true, true)
	case a == actFetch && !modelOK(run, false):
		stopRun(run, domain.StopBudgetModelCalls, true, true)
	default:
		return false
	}
	return true
}

// step performs the run's next step: record it as pending, do the work (each side effect idempotent), save the
// run's new state, then mark the step done or failed. Order matters: the run is saved before the step is marked
// done, so a done step always means the run moved past it. work ends before base, leaving time to save.
func (e *Engine) step(work, base context.Context, run *domain.ResearchRun) error {
	ensureMaps(run)
	start := e.now()
	idx := run.NextStepIndex
	a := nextAction(run)
	run.Status = phaseOf(a, run)
	rec := &domain.ResearchStep{
		RunID: run.ID, UserID: run.UserID, StepIndex: idx, Phase: run.Status, Action: string(a),
		InputData: map[string]any{"round": run.Round, "pending_queries": len(run.PendingQueries), "pending_urls": len(run.PendingURLs)},
		Status:    "pending", CreatedAt: start,
	}
	if err := e.Repo.RecordStep(base, rec); err != nil {
		return err
	}

	var out map[string]any
	var aerr error
	switch a {
	case actPlan:
		out, aerr = e.doPlan(work, run)
	case actSearch:
		out, aerr = e.doSearch(work, run)
	case actFetch:
		out, aerr = e.doFetchExtract(work, run)
	case actGap:
		out, aerr = e.doGap(work, run)
	default:
		out, aerr = e.doSynthesis(work, run)
	}
	if out == nil {
		out = map[string]any{}
	}
	elapsed := e.now().Sub(start)
	run.ProgressMetrics["time_ms"] += int(elapsed.Milliseconds())
	run.NextStepIndex = idx + 1
	status := "done"
	if aerr != nil {
		status = "failed"
		out["error"] = aerr.Error()
	}
	if err := e.Repo.UpdateRun(base, run); err != nil {
		return err
	}
	rec.Status, rec.OutputData, rec.DurationMs = status, out, int(elapsed.Milliseconds())
	if err := e.Repo.RecordStep(base, rec); err != nil && !errors.Is(err, domain.ErrResearchStepDone) {
		e.logger().Warn("Could not mark research step", "run_id", run.ID, "step", idx, "error", err)
	}
	return nil
}

func (e *Engine) failRun(run *domain.ResearchRun, msg string) {
	run.Status = domain.PhaseFailed
	run.StopReason = domain.StopFailed
	run.ErrorMessage = &msg
	run.PendingQueries, run.PendingURLs = nil, nil
	if run.Round == 0 {
		run.Round = 1
	}
	now := e.now()
	run.CompletedAt = &now
	refreshQuestions(run)
	run.ReportMarkdown = e.composeReport(run, "", "")
	run.ReportStructured = e.structuredReport(run, nil)
}

// ProcessPlaybook hands a finished run's supported findings to its playbook, once, after synthesis. A run with no
// playbook (or an unfinished one) returns nil: the generic engine needs none.
func (e *Engine) ProcessPlaybook(ctx context.Context, run *domain.ResearchRun) (*playbooks.PlaybookResult, error) {
	pb := e.Playbooks.For(run.Playbook)
	if pb == nil || run.Status != domain.PhaseCompleted {
		return nil, nil
	}
	only := *run
	only.Findings = playbooks.Supported(run.Findings)
	return pb.ProcessResults(ctx, &only)
}

// doPlan is the first step: the strong model turns the objective into questions and first queries.
func (e *Engine) doPlan(ctx context.Context, run *domain.ResearchRun) (map[string]any, error) {
	out := map[string]any{}
	if len(run.Plan.InitialQueries) == 0 {
		run.ProgressMetrics["model_calls"]++
		guidance := ""
		if pb := e.Playbooks.For(run.Playbook); pb != nil {
			guidance = pb.PlanGuidance()
		}
		resp, err := e.Router.AskPlan(ctx, run.Objective, run.ConstraintsList, string(run.Depth), guidance)
		if err != nil {
			e.failRun(run, "planning failed: "+err.Error())
			return out, err
		}
		used := map[string]bool{}
		for _, q := range run.Plan.Questions {
			used[q.ID] = true
		}
		for i, q := range resp.Questions {
			if i >= 12 || strings.TrimSpace(q.Question) == "" {
				continue
			}
			id := strings.TrimSpace(q.ID)
			if id == "" || used[id] {
				id = fmt.Sprintf("q%d", len(run.Plan.Questions)+1)
				for used[id] {
					id += "x"
				}
			}
			used[id] = true
			run.Plan.Questions = append(run.Plan.Questions, domain.ResearchQuestion{
				ID: id, Question: strings.TrimSpace(q.Question), Category: q.Category, Status: "pending",
			})
		}
		run.Plan.InitialQueries = cleanQueries(resp.InitialQueries)
	}
	if len(run.Plan.InitialQueries) == 0 {
		run.Plan.InitialQueries = cleanQueries([]string{run.Objective})
	}
	exp := NewQueryExpanderFrom(run.ExecutedQueries)
	if pb := e.Playbooks.For(run.Playbook); pb != nil {
		// The playbook's own angles on its subject, after the model's queries, without repeating one.
		known := NewQueryExpanderFrom(run.Plan.InitialQueries)
		for _, q := range cleanQueries(known.ExpandTemplates(pb.QueryTemplates(), pb.Subject(run.ContextData))) {
			run.Plan.InitialQueries = append(run.Plan.InitialQueries, q)
		}
	}
	for _, q := range run.Plan.InitialQueries {
		if !exp.HasExecuted(q) {
			run.PendingQueries = append(run.PendingQueries, q)
		}
	}
	run.Round = 1
	run.Status = domain.PhaseSourceDiscovery
	refreshQuestions(run)
	out["questions"] = len(run.Plan.Questions)
	out["queries"] = run.PendingQueries
	return out, nil
}

func cleanQueries(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, q := range in {
		q = strings.TrimSpace(q)
		if r := []rune(q); len(r) > maxQueryRunes {
			q = string(r[:maxQueryRunes])
		}
		k := strings.ToLower(q)
		if q == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, q)
	}
	if len(out) > 6 {
		out = out[:6]
	}
	return out
}

// doSearch runs one query. Its hits only feed the URL queue and this step's output; a page becomes a source only
// once it has been fetched.
func (e *Engine) doSearch(ctx context.Context, run *domain.ResearchRun) (map[string]any, error) {
	out := map[string]any{}
	q := run.PendingQueries[0]
	run.PendingQueries = run.PendingQueries[1:]
	out["query"] = q
	if NewQueryExpanderFrom(run.ExecutedQueries).HasExecuted(q) {
		out["skipped"] = "query already run"
		return out, nil
	}
	run.ExecutedQueries = append(run.ExecutedQueries, q)
	run.ProgressMetrics["searches_done"]++
	if e.Tools == nil {
		return out, ErrNoTools
	}
	hits, err := e.Tools.Search(ctx, q, searchHitLimit)
	if err != nil {
		return out, fmt.Errorf("search: %w", err)
	}
	seen := map[string]bool{}
	for _, u := range run.SeenURLs {
		seen[u] = true
	}
	var shown []map[string]string
	added := 0
	for _, h := range hits {
		canon := CanonicalURL(h.URL)
		if canon == "" || seen[canon] {
			continue
		}
		if len(shown) < 10 {
			shown = append(shown, map[string]string{"title": h.Title, "url": h.URL, "domain": h.Domain})
		}
		if added >= maxURLsPerSearch {
			continue
		}
		seen[canon] = true
		run.SeenURLs = append(run.SeenURLs, canon)
		run.PendingURLs = append(run.PendingURLs, h.URL)
		added++
	}
	out["hits"] = shown
	out["urls_queued"] = added
	return out, nil
}

// doFetchExtract fetches one page, records it as a source, and extracts claims from it. Extraction happens once
// per source: if evidence for the source already exists (a step repeated after a crash), it is reused.
func (e *Engine) doFetchExtract(ctx context.Context, run *domain.ResearchRun) (map[string]any, error) {
	out := map[string]any{}
	rawURL := run.PendingURLs[0]
	run.PendingURLs = run.PendingURLs[1:]
	out["url"] = rawURL
	run.ProgressMetrics["pages_inspected"]++
	if e.Tools == nil {
		return out, ErrNoTools
	}
	page, err := e.Tools.Fetch(ctx, rawURL)
	if err != nil {
		return out, fmt.Errorf("fetch: %w", err)
	}
	if page == nil {
		return out, errors.New("fetch returned no page")
	}

	canon := CanonicalURL(rawURL)
	if canon == "" {
		return out, errors.New("not an http(s) address")
	}
	hash := page.ContentHash
	if hash == "" {
		hash = domain.DescriptionHash(page.Text)
	}
	srcType, tier := ClassifySourceType(rawURL)
	raw := page.Text
	if len(raw) > maxRawTextBytes {
		raw = raw[:maxRawTextBytes]
	}
	src := &domain.ResearchSource{
		UserID: run.UserID, RunID: &run.ID, URL: rawURL, CanonicalURL: canon, Title: page.Title,
		Domain: page.Domain, SourceType: srcType, ReliabilityTier: tier, ContentHash: hash,
		RawText: raw, RetrievedAt: e.now(),
	}
	if src.Domain == "" {
		src.Domain = extractDomain(rawURL)
	}
	if err := e.Repo.AddSource(ctx, src); err != nil {
		return out, fmt.Errorf("record source: %w", err)
	}
	out["title"], out["content_hash"], out["source_id"] = page.Title, hash, src.ID.String()

	existing, err := e.Repo.ListEvidenceForRun(ctx, run.UserID, run.ID)
	if err != nil {
		return out, fmt.Errorf("list evidence: %w", err)
	}
	var already []*domain.EvidenceItem
	for _, ev := range existing {
		if ev.SourceID != nil && *ev.SourceID == src.ID {
			already = append(already, ev)
		}
	}
	if len(already) > 0 {
		e.restoreFindings(run, already)
		out["reused_evidence"] = len(already)
		return out, nil
	}

	run.ProgressMetrics["model_calls"]++
	var open []domain.ResearchQuestion
	for _, q := range run.Plan.Questions {
		if q.Status != "answered" {
			open = append(open, q)
		}
	}
	ext, err := e.Router.ExtractClaims(ctx, run.Objective, rawURL, page.Text, open)
	if err != nil {
		return out, fmt.Errorf("extract: %w", err)
	}

	known := map[string]bool{}
	for _, q := range run.Plan.Questions {
		known[q.ID] = true
	}
	var supported, unverified, errCount int
	for i, f := range ext.Findings {
		if i >= maxFindingsPerPage {
			break
		}
		claim, quote := strings.TrimSpace(f.Claim), strings.TrimSpace(f.Quote)
		if claim == "" {
			continue
		}
		status, verified := domain.EvidenceUnverified, (*time.Time)(nil)
		reliability := domain.ReliabilityUnverified
		if QuoteInText(quote, page.Text) {
			now := e.now()
			status, verified, reliability = domain.EvidenceSupported, &now, tier
		}
		ev := &domain.EvidenceItem{
			UserID: run.UserID, RunID: &run.ID, SourceID: &src.ID, Claim: claim, ExtractedValue: strings.TrimSpace(f.ExtractedValue),
			ExcerptQuote: quote, SourceURL: rawURL, SourceDomain: src.Domain, SourceType: srcType,
			RetrievedAt: src.RetrievedAt, VerifiedAt: verified, Reliability: reliability,
			Confidence: clampConfidence(f.Confidence), Status: status, ExtractionMethod: "model_extraction",
			DedupeKey: dedupeKey(canon, claim, quote),
		}
		if err := e.Repo.AddEvidence(ctx, ev); err != nil {
			e.logger().Warn("Could not store evidence", "run_id", run.ID, "url", rawURL, "error", err)
			errCount++
			continue
		}
		qid := strings.TrimSpace(f.QuestionID)
		if !known[qid] {
			qid = ""
		}
		id := ev.ID
		run.Findings = append(run.Findings, domain.ResearchFinding{
			ID: "f_" + ev.DedupeKey[:12], Topic: f.Topic, Claim: claim, ExtractedValue: ev.ExtractedValue, Quote: quote,
			EvidenceID: &id, SourceURL: rawURL, Confidence: ev.Confidence, IsContradiction: f.IsContradiction,
			Status: ev.Status, QuestionID: qid, Subject: strings.TrimSpace(f.Subject), Property: strings.TrimSpace(f.Property),
		})
		if ev.Status == domain.EvidenceSupported {
			supported++
		} else {
			unverified++
		}
	}
	e.markConflicts(ctx, run)
	run.ProgressMetrics["findings_count"] = len(run.Findings)
	out["supported"], out["unverified"] = supported, unverified
	if errCount > 0 {
		out["evidence_errors"] = errCount
		return out, fmt.Errorf("%d evidence rows could not be stored", errCount)
	}
	return out, nil
}

// restoreFindings rebuilds the findings of evidence a repeated step found already stored. The question and
// subject links the model gave are not kept on evidence, so they are lost for these findings.
func (e *Engine) restoreFindings(run *domain.ResearchRun, items []*domain.EvidenceItem) {
	have := map[string]bool{}
	for _, f := range run.Findings {
		if f.EvidenceID != nil {
			have[f.EvidenceID.String()] = true
		}
	}
	for _, ev := range items {
		if have[ev.ID.String()] {
			continue
		}
		id := ev.ID
		run.Findings = append(run.Findings, domain.ResearchFinding{
			ID: "f_" + strings.ReplaceAll(ev.ID.String(), "-", "")[:12], Claim: ev.Claim, ExtractedValue: ev.ExtractedValue,
			Quote: ev.ExcerptQuote, EvidenceID: &id, SourceURL: ev.SourceURL, Confidence: ev.Confidence, Status: ev.Status,
		})
	}
	run.ProgressMetrics["findings_count"] = len(run.Findings)
}

// markConflicts finds values that different sources disagree on and marks every finding involved, and its
// evidence row, conflicting. Neither value is chosen.
func (e *Engine) markConflicts(ctx context.Context, run *domain.ResearchRun) {
	_, changed := DetectConflicts(run.Findings)
	for _, i := range changed {
		f := run.Findings[i]
		if f.EvidenceID == nil {
			continue
		}
		if err := e.Repo.SetEvidenceStatus(ctx, run.UserID, *f.EvidenceID, domain.EvidenceConflicting); err != nil {
			e.logger().Warn("Could not mark evidence conflicting", "run_id", run.ID, "error", err)
		}
	}
}

// refreshQuestions recomputes which questions are answered: a question is answered only if at least one supported
// finding is linked to it, by the question id the extraction returned or by an accepted gap check.
func refreshQuestions(run *domain.ResearchRun) {
	supported := map[string]domain.ResearchFinding{}
	for _, f := range run.Findings {
		if f.Status == domain.EvidenceSupported {
			supported[f.ID] = f
		}
	}
	var open []string
	for i := range run.Plan.Questions {
		q := &run.Plan.Questions[i]
		var ids []string
		have := map[string]bool{}
		add := func(id string) {
			if _, ok := supported[id]; ok && !have[id] {
				have[id] = true
				ids = append(ids, id)
			}
		}
		for _, id := range q.FindingIDs {
			add(id)
		}
		for _, f := range run.Findings {
			if f.QuestionID == q.ID {
				add(f.ID)
			}
		}
		q.FindingIDs = ids
		q.SupportingEvidenceIDs = nil
		for _, id := range ids {
			if ev := supported[id].EvidenceID; ev != nil {
				q.SupportingEvidenceIDs = append(q.SupportingEvidenceIDs, *ev)
			}
		}
		if len(ids) > 0 {
			q.Status = "answered"
		} else {
			q.Status = "pending"
			open = append(open, q.Question)
		}
	}
	run.UnresolvedQuestions = open
}

func openQuestions(run *domain.ResearchRun) []domain.ResearchQuestion {
	var open []domain.ResearchQuestion
	for _, q := range run.Plan.Questions {
		if q.Status != "answered" {
			open = append(open, q)
		}
	}
	return open
}

func supportedFindings(run *domain.ResearchRun, n int) []domain.ResearchFinding {
	var s []domain.ResearchFinding
	for _, f := range run.Findings {
		if f.Status == domain.EvidenceSupported {
			s = append(s, f)
		}
	}
	sort.SliceStable(s, func(i, j int) bool { return s[i].Confidence > s[j].Confidence })
	if n > 0 && len(s) > n {
		s = s[:n]
	}
	return s
}

// doGap decides what is still unanswered and what to search next, or why to stop.
func (e *Engine) doGap(ctx context.Context, run *domain.ResearchRun) (map[string]any, error) {
	out := map[string]any{}
	refreshQuestions(run)
	open := openQuestions(run)
	var gapErr error
	if sup := supportedFindings(run, maxFindingsToModel); len(open) > 0 && len(sup) > 0 && modelOK(run, false) {
		run.ProgressMetrics["model_calls"]++
		resp, err := e.Router.AskGapCheck(ctx, run.Objective, open, sup)
		if err != nil {
			gapErr = fmt.Errorf("gap check: %w", err)
		} else {
			e.applyGapCheck(run, open, resp)
			refreshQuestions(run)
			open = openQuestions(run)
		}
	}
	ids := make([]string, 0, len(open))
	for _, q := range open {
		ids = append(ids, q.ID)
	}
	out["open_questions"] = ids

	switch {
	case len(open) == 0:
		run.StopReason = domain.StopAnswered
	case run.ProgressMetrics["searches_done"] >= limit(run, "max_searches"):
		run.StopReason = domain.StopBudgetSearches
	default:
		exp := NewQueryExpanderFrom(run.ExecutedQueries, run.PendingQueries)
		var fresh []string
		for _, q := range open {
			for _, cand := range exp.ExpandGap(q.Question) {
				cand = cleanQueryOne(cand)
				if cand != "" && !exp.HasExecuted(cand) {
					exp.MarkExecuted(cand)
					fresh = append(fresh, cand)
				}
			}
		}
		room := limit(run, "max_searches") - run.ProgressMetrics["searches_done"]
		if len(fresh) > queriesPerRound {
			fresh = fresh[:queriesPerRound]
		}
		if len(fresh) > room {
			fresh = fresh[:room]
		}
		if len(fresh) == 0 {
			run.StopReason = domain.StopExhausted
		} else {
			run.Round++
			run.PendingQueries = fresh
			out["new_queries"] = fresh
		}
	}
	out["stop_reason"] = run.StopReason
	return out, gapErr
}

func cleanQueryOne(q string) string {
	c := cleanQueries([]string{q})
	if len(c) == 0 {
		return ""
	}
	return c[0]
}

// applyGapCheck accepts a model's claim that a question is answered only when it cites supported findings that exist.
func (e *Engine) applyGapCheck(run *domain.ResearchRun, open []domain.ResearchQuestion, resp *GapCheckResponse) {
	isOpen := map[string]bool{}
	for _, q := range open {
		isOpen[q.ID] = true
	}
	supported := map[string]bool{}
	for _, f := range run.Findings {
		if f.Status == domain.EvidenceSupported {
			supported[f.ID] = true
		}
	}
	for _, a := range resp.Answers {
		if !isOpen[a.QuestionID] {
			continue
		}
		var cited []string
		for _, id := range a.FindingIDs {
			if supported[id] {
				cited = append(cited, id)
			}
		}
		if len(cited) == 0 {
			continue
		}
		for i := range run.Plan.Questions {
			if run.Plan.Questions[i].ID == a.QuestionID {
				run.Plan.Questions[i].FindingIDs = append(run.Plan.Questions[i].FindingIDs, cited...)
			}
		}
	}
}

// doSynthesis writes the report and finishes the run. The model's analysis is only part of the report: the stop
// reason, coverage, open questions, conflicts and findings are written by code, so a budget stop can never read as
// complete coverage. A run whose synthesis fails is FAILED with an incomplete report, never COMPLETED with a stub.
func (e *Engine) doSynthesis(ctx context.Context, run *domain.ResearchRun) (map[string]any, error) {
	out := map[string]any{}
	refreshQuestions(run)
	conflicts, _ := DetectConflicts(run.Findings)
	if run.StopReason == "" {
		run.StopReason = domain.StopExhausted
	}

	var narrative, note string
	var modelStructured map[string]any
	var synthErr error
	switch {
	case timeUp(run):
		note = "The analysis was not written: the time budget was used up."
	case !modelOK(run, true):
		note = "The analysis was not written: the model-call budget was used up."
	default:
		run.ProgressMetrics["model_calls"]++
		fj, err := json.Marshal(topFindings(run.Findings, maxFindingsToModel))
		if err == nil {
			narrative, modelStructured, err = e.Router.SynthesizeReport(ctx, run.Objective, fj, run.UnresolvedQuestions, e.now())
		}
		synthErr = err
	}
	if synthErr != nil {
		e.failRun(run, "synthesis failed: "+synthErr.Error())
		return out, synthErr
	}

	now := e.now()
	run.Status = domain.PhaseCompleted
	run.CompletedAt = &now
	run.ReportMarkdown = e.composeReport(run, narrative, note)
	run.ReportStructured = e.structuredReport(run, modelStructured)
	run.ReportStructured["conflicts"] = conflicts
	out["stop_reason"] = run.StopReason
	out["findings"] = len(run.Findings)
	out["open_questions"] = len(run.UnresolvedQuestions)
	return out, nil
}

func topFindings(fs []domain.ResearchFinding, n int) []domain.ResearchFinding {
	rank := func(s domain.EvidenceStatus) int {
		switch s {
		case domain.EvidenceSupported:
			return 0
		case domain.EvidenceConflicting:
			return 1
		}
		return 2
	}
	out := append([]domain.ResearchFinding(nil), fs...)
	sort.SliceStable(out, func(i, j int) bool {
		if ri, rj := rank(out[i].Status), rank(out[j].Status); ri != rj {
			return ri < rj
		}
		return out[i].Confidence > out[j].Confidence
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// PauseRun pauses a run that has not finished. The change is made against the stored status, and the routine
// re-reads the status before every step, so it takes effect at the next step boundary and cannot be overwritten.
func (e *Engine) PauseRun(ctx context.Context, userID, runID uuid.UUID) error {
	ok, err := e.Repo.TransitionRun(ctx, userID, runID, domain.PhasePaused, activePhases, "", nil)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	run, err := e.Repo.GetRun(ctx, userID, runID)
	if err != nil {
		return err
	}
	if run.Status == domain.PhasePaused {
		return nil
	}
	return fmt.Errorf("%w: cannot pause a %s run", domain.ErrConflict, strings.ToLower(string(run.Status)))
}

// CancelRun cancels a run that has not finished, active or paused. Work in flight stops at its next step boundary.
func (e *Engine) CancelRun(ctx context.Context, userID, runID uuid.UUID) error {
	from := append(append([]domain.ResearchPhase(nil), activePhases...), domain.PhasePaused)
	ok, err := e.Repo.TransitionRun(ctx, userID, runID, domain.PhaseCancelled, from, domain.StopCancelled, nil)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	run, err := e.Repo.GetRun(ctx, userID, runID)
	if err != nil {
		return err
	}
	if run.Status == domain.PhaseCancelled {
		return nil
	}
	return fmt.Errorf("%w: cannot cancel a %s run", domain.ErrConflict, strings.ToLower(string(run.Status)))
}

// FailRun marks an unfinished run failed with a reason (used when its work could not be scheduled).
func (e *Engine) FailRun(ctx context.Context, userID, runID uuid.UUID, msg string) error {
	from := append(append([]domain.ResearchPhase(nil), activePhases...), domain.PhasePaused)
	_, err := e.Repo.TransitionRun(ctx, userID, runID, domain.PhaseFailed, from, domain.StopFailed, &msg)
	return err
}

// ResumeRun sets a paused run active again and returns it. It never runs any step: the caller schedules the task
// that does. A run that is not paused is returned unchanged.
func (e *Engine) ResumeRun(ctx context.Context, userID, runID uuid.UUID) (*domain.ResearchRun, error) {
	run, err := e.Repo.GetRun(ctx, userID, runID)
	if err != nil {
		return nil, err
	}
	if run.Status != domain.PhasePaused {
		return run, nil
	}
	to := domain.PhaseSourceDiscovery
	if run.Round == 0 {
		to = domain.PhaseRequestReceived
	}
	if _, err := e.Repo.TransitionRun(ctx, userID, runID, to, []domain.ResearchPhase{domain.PhasePaused}, "", nil); err != nil {
		return nil, err
	}
	return e.Repo.GetRun(ctx, userID, runID)
}

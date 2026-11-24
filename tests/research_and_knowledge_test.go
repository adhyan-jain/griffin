package tests

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/api"
	"github.com/griffin/core/internal/database"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/provider/gemini"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/research"
	"github.com/griffin/core/internal/service"
	"github.com/griffin/core/internal/storage"
)

func TestResearchRepositoryAgainstDatabase(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()

	owner := CreateTestUser(t, env.DB, "research-user@example.com")
	repo := repository.NewResearchRepository(env.DB.Pool)

	run := &domain.ResearchRun{
		UserID:    owner,
		Playbook:  "company_intelligence",
		Objective: "Research procurement tech startups in India",
		Depth:     domain.DepthDeep,
		Status:    domain.PhasePlanning,
		Plan: domain.ResearchPlan{
			Questions: []domain.ResearchQuestion{
				{ID: "q1", Question: "What companies operate in this market?", Category: "market", Status: "pending"},
			},
			InitialQueries: []string{"procurement tech startups india"},
		},
		BudgetConfig:    map[string]int{"max_searches": 10, "max_pages": 15},
		ProgressMetrics: map[string]int{"searches_done": 1, "pages_inspected": 2},
		Findings: []domain.ResearchFinding{
			{ID: "f1", Topic: "Startups", Claim: "ProcurePro is a B2B SaaS startup", Confidence: 90},
		},
		UnresolvedQuestions: []string{"Who are the founders?"},
		ReportMarkdown:      "# Procurement Startups\n\nInitial findings...",
	}

	require.NoError(t, repo.CreateRun(ctx, run))
	require.NotEqual(t, uuid.Nil, run.ID)

	// Fetch run
	got, err := repo.GetRun(ctx, owner, run.ID)
	require.NoError(t, err)
	require.Equal(t, run.Objective, got.Objective)
	require.Equal(t, domain.DepthDeep, got.Depth)
	require.Equal(t, 1, len(got.Plan.Questions))
	require.Equal(t, 1, len(got.Findings))
	require.Equal(t, 10, got.BudgetConfig["max_searches"])

	// Record an audit step
	step := &domain.ResearchStep{
		RunID:      run.ID,
		UserID:     owner,
		StepIndex:  1,
		Phase:      domain.PhasePlanning,
		Action:     "plan_generated",
		InputData:  map[string]any{"depth": "deep"},
		OutputData: map[string]any{"questions": 1},
		DurationMs: 150,
		Status:     "completed",
	}
	require.NoError(t, repo.RecordStep(ctx, step))

	steps, err := repo.ListSteps(ctx, owner, run.ID)
	require.NoError(t, err)
	require.Len(t, steps, 1)
	require.Equal(t, "plan_generated", steps[0].Action)

	// Add source
	source := &domain.ResearchSource{
		UserID:          owner,
		RunID:           &run.ID,
		URL:             "https://example.com/procurepro",
		CanonicalURL:    "https://example.com/procurepro",
		Title:           "ProcurePro - Enterprise B2B Sourcing",
		Domain:          "example.com",
		SourceType:      domain.SourceOfficialCareerPage,
		ReliabilityTier: domain.ReliabilityPrimaryOfficial,
	}
	require.NoError(t, repo.AddSource(ctx, source))
	require.NotEqual(t, uuid.Nil, source.ID)

	// Add evidence
	evidence := &domain.EvidenceItem{
		UserID:           owner,
		RunID:            &run.ID,
		SourceID:         &source.ID,
		Claim:            "ProcurePro operates a reverse-auction platform",
		ExtractedValue:   "reverse-auction",
		ExcerptQuote:     "ProcurePro pioneers reverse-auctions for enterprise raw material sourcing.",
		SourceURL:        source.URL,
		SourceDomain:     source.Domain,
		SourceType:       source.SourceType,
		Reliability:      source.ReliabilityTier,
		Confidence:       95,
		Status:           domain.EvidenceSupported,
		ExtractionMethod: "model",
	}
	require.NoError(t, repo.AddEvidence(ctx, evidence))
	require.NotEqual(t, uuid.Nil, evidence.ID)

	evidenceList, err := repo.ListEvidenceForRun(ctx, owner, run.ID)
	require.NoError(t, err)
	require.Len(t, evidenceList, 1)
	require.Equal(t, 95, evidenceList[0].Confidence)

	// Update run to completed
	run.Status = domain.PhaseCompleted
	run.ReportMarkdown = "# Completed Report"
	require.NoError(t, repo.UpdateRun(ctx, run))

	completed, err := repo.GetRun(ctx, owner, run.ID)
	require.NoError(t, err)
	require.Equal(t, domain.PhaseCompleted, completed.Status)
	require.NotNil(t, completed.CompletedAt)
}

func TestKnowledgeRepositoryAgainstDatabase(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()

	owner := CreateTestUser(t, env.DB, "knowledge-user@example.com")
	repo := repository.NewKnowledgeRepository(env.DB.Pool)

	// 1. Canonical company entity
	company := &domain.CanonicalEntity{
		UserID:       owner,
		Kind:         domain.EntityCompany,
		CanonicalKey: "acmeprocure.com",
		Name:         "AcmeProcure Technologies",
		Summary:      "AI procurement automation",
		Metadata:     map[string]any{"funding_stage": "Series A", "location": "Bengaluru"},
	}
	require.NoError(t, repo.UpsertEntity(ctx, company))
	require.NotEqual(t, uuid.Nil, company.ID)

	// 2. Add Alias
	alias := &domain.EntityAlias{
		EntityID: company.ID,
		UserID:   owner,
		Alias:    "Acme Procure",
		Kind:     "name",
	}
	require.NoError(t, repo.AddAlias(ctx, alias))

	foundByAlias, err := repo.FindEntityByAliasKind(ctx, owner, "", "acme procure")
	require.NoError(t, err)
	require.NotNil(t, foundByAlias)
	require.Equal(t, company.ID, foundByAlias.ID)

	// 3. Canonical person entity
	person := &domain.CanonicalEntity{
		UserID:       owner,
		Kind:         domain.EntityPerson,
		CanonicalKey: "rahul-sharma-vit-2020",
		Name:         "Rahul Sharma",
		Summary:      "Co-Founder & CTO",
		Metadata:     map[string]any{"grad_year": 2020, "institution": "VIT"},
	}
	require.NoError(t, repo.UpsertEntity(ctx, person))

	// 4. Directed relationship: Person -> works_at -> Company
	rel := &domain.EntityRelationship{
		UserID:     owner,
		SubjectID:  person.ID,
		Predicate:  domain.RelWorksAt,
		ObjectID:   company.ID,
		Confidence: 90,
		Metadata:   map[string]any{"role": "CTO"},
	}
	require.NoError(t, repo.UpsertRelationship(ctx, rel))

	rels, err := repo.ListRelationships(ctx, owner, company.ID)
	require.NoError(t, err)
	require.Len(t, rels, 1)
	require.Equal(t, domain.RelWorksAt, rels[0].Predicate)
	require.Equal(t, person.ID, rels[0].SubjectID)

	// 5. Temporal observation
	obs := &domain.EntityObservation{
		UserID:     owner,
		EntityID:   company.ID,
		Property:   "headcount",
		Value:      "45 employees",
		ObservedAt: time.Now(),
	}
	require.NoError(t, repo.RecordObservation(ctx, obs))

	observations, err := repo.ListObservations(ctx, owner, company.ID)
	require.NoError(t, err)
	require.Len(t, observations, 1)
	require.Equal(t, "headcount", observations[0].Property)
	require.Equal(t, "45 employees", observations[0].Value)
}

func TestResearchRepositoryDurabilityGuarantees(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	owner := CreateTestUser(t, env.DB, "research-durable@example.com")
	other := CreateTestUser(t, env.DB, "research-other@example.com")
	repo := repository.NewResearchRepository(env.DB.Pool)

	run := &domain.ResearchRun{
		UserID: owner, Playbook: "generic", Objective: "durability", Depth: domain.DepthQuick, Status: domain.PhaseSourceDiscovery,
		ContextData: map[string]any{"k": "v"}, ConstraintsList: []string{"india only"}, BudgetConfig: map[string]int{"max_searches": 3},
		PendingQueries: []string{"q1"}, ExecutedQueries: []string{"q0"}, PendingURLs: []string{"https://a.example.com/x"},
		SeenURLs: []string{"https://a.example.com/x"}, Round: 1, NextStepIndex: 2,
	}
	require.NoError(t, repo.CreateRun(ctx, run))

	// UpdateRun persists context, constraints, budgets and the queues, and bumps the revision.
	run.ContextData, run.ConstraintsList = map[string]any{"k": "v2"}, []string{"remote"}
	run.BudgetConfig = map[string]int{"max_searches": 5}
	run.PendingQueries, run.StopReason = nil, domain.StopBudgetSearches
	require.NoError(t, repo.UpdateRun(ctx, run))
	got, err := repo.GetRun(ctx, owner, run.ID)
	require.NoError(t, err)
	require.Equal(t, "v2", got.ContextData["k"])
	require.Equal(t, []string{"remote"}, got.ConstraintsList)
	require.Equal(t, 5, got.BudgetConfig["max_searches"])
	require.Equal(t, []string{"q0"}, got.ExecutedQueries)
	require.Equal(t, []string{"https://a.example.com/x"}, got.PendingURLs)
	require.Equal(t, domain.StopBudgetSearches, got.StopReason)
	require.EqualValues(t, 1, got.Revision)

	// a copy read before that write is stale: its write is refused
	stale := *got
	stale.Revision = 0
	require.ErrorIs(t, repo.UpdateRun(ctx, &stale), domain.ErrResearchRunInactive)

	// pause is a compare-and-set, and a late UpdateRun cannot undo it
	paused, err := repo.TransitionRun(ctx, owner, run.ID, domain.PhasePaused, []domain.ResearchPhase{domain.PhaseSourceDiscovery}, "", nil)
	require.NoError(t, err)
	require.True(t, paused)
	late := *got
	late.Revision = got.Revision + 1
	late.Status = domain.PhaseCompleted
	require.ErrorIs(t, repo.UpdateRun(ctx, &late), domain.ErrResearchRunInactive)
	again, _ := repo.GetRun(ctx, owner, run.ID)
	require.Equal(t, domain.PhasePaused, again.Status)
	require.Nil(t, again.CompletedAt)
	notPaused, err := repo.TransitionRun(ctx, owner, run.ID, domain.PhaseCancelled, []domain.ResearchPhase{domain.PhasePlanning}, "", nil)
	require.NoError(t, err)
	require.False(t, notPaused, "the compare-and-set did not match the stored status")
	moved, err := repo.TransitionRun(ctx, other, run.ID, domain.PhaseCancelled, []domain.ResearchPhase{domain.PhasePaused}, domain.StopCancelled, nil)
	require.NoError(t, err)
	require.False(t, moved, "another user cannot transition the run")

	// steps: one row per (run, index); a done step cannot be redone
	step := &domain.ResearchStep{RunID: run.ID, UserID: owner, StepIndex: 1, Phase: domain.PhasePlanning, Action: "plan", Status: "pending"}
	require.NoError(t, repo.RecordStep(ctx, step))
	step.Status = "done"
	require.NoError(t, repo.RecordStep(ctx, step))
	dup := &domain.ResearchStep{RunID: run.ID, UserID: owner, StepIndex: 1, Phase: domain.PhasePlanning, Action: "plan", Status: "pending"}
	require.ErrorIs(t, repo.RecordStep(ctx, dup), domain.ErrResearchStepDone)
	steps, _ := repo.ListSteps(ctx, owner, run.ID)
	require.Len(t, steps, 1)
	require.Equal(t, "done", steps[0].Status)

	// sources: once per (run, canonical url)
	src := func() *domain.ResearchSource {
		return &domain.ResearchSource{UserID: owner, RunID: &run.ID, URL: "https://a.example.com/x?utm_source=z", CanonicalURL: "https://a.example.com/x",
			Title: "T", Domain: "a.example.com", SourceType: domain.SourceUnverifiedWeb, ReliabilityTier: domain.ReliabilityUnverified,
			ContentHash: "h1", RetrievedAt: time.Now()}
	}
	s1, s2 := src(), src()
	s2.ContentHash = "h2"
	require.NoError(t, repo.AddSource(ctx, s1))
	require.NoError(t, repo.AddSource(ctx, s2))
	require.Equal(t, s1.ID, s2.ID)
	require.Equal(t, 1, countRows(t, ctx, env.DB.Pool, `SELECT count(*) FROM research_sources WHERE run_id=$1`, run.ID))
	require.Equal(t, "h2", func() string {
		var h string
		require.NoError(t, env.DB.Pool.QueryRow(ctx, `SELECT content_hash FROM research_sources WHERE id=$1`, s1.ID).Scan(&h))
		return h
	}())

	// evidence: the same claim and quote from the same page is one row; verified_at is stored
	now := time.Now()
	ev := func() *domain.EvidenceItem {
		return &domain.EvidenceItem{UserID: owner, RunID: &run.ID, SourceID: &s1.ID, Claim: "c", ExcerptQuote: "a quote that is long enough",
			SourceURL: s1.URL, SourceDomain: s1.Domain, SourceType: s1.SourceType, Reliability: s1.ReliabilityTier, Confidence: 80,
			Status: domain.EvidenceSupported, VerifiedAt: &now, RetrievedAt: now, ExtractionMethod: "model_extraction", DedupeKey: "k1"}
	}
	e1, e2 := ev(), ev()
	require.NoError(t, repo.AddEvidence(ctx, e1))
	require.NoError(t, repo.AddEvidence(ctx, e2))
	require.Equal(t, e1.ID, e2.ID)
	require.Equal(t, 1, countRows(t, ctx, env.DB.Pool, `SELECT count(*) FROM evidence_items WHERE run_id=$1 AND verified_at IS NOT NULL`, run.ID))
	require.NoError(t, repo.SetEvidenceStatus(ctx, owner, e1.ID, domain.EvidenceConflicting))
	require.ErrorIs(t, repo.SetEvidenceStatus(ctx, other, e1.ID, domain.EvidenceRejected), domain.ErrNotFound)

	// a user cannot attach evidence or sources to another user's run or source
	foreign := ev()
	foreign.UserID, foreign.DedupeKey = other, "k2"
	require.ErrorIs(t, repo.AddEvidence(ctx, foreign), domain.ErrValidation)
	foreignSrc := src()
	foreignSrc.UserID = other
	require.ErrorIs(t, repo.AddSource(ctx, foreignSrc), domain.ErrValidation)
}

func TestKnowledgeRepositoryRejectsCrossUserReferences(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	a := CreateTestUser(t, env.DB, "kn-a@example.com")
	b := CreateTestUser(t, env.DB, "kn-b@example.com")
	repo := repository.NewKnowledgeRepository(env.DB.Pool)

	mk := func(user uuid.UUID, kind domain.EntityKind, key string) *domain.CanonicalEntity {
		e := &domain.CanonicalEntity{UserID: user, Kind: kind, CanonicalKey: key, Name: key}
		require.NoError(t, repo.UpsertEntity(ctx, e))
		return e
	}
	aco, bco, bperson := mk(a, domain.EntityCompany, "acme.com"), mk(b, domain.EntityCompany, "beta.com"), mk(b, domain.EntityPerson, "acme-person")

	require.ErrorIs(t, repo.AddAlias(ctx, &domain.EntityAlias{EntityID: bco.ID, UserID: a, Alias: "x", Kind: "name"}), domain.ErrValidation)
	require.NoError(t, repo.AddAlias(ctx, &domain.EntityAlias{EntityID: aco.ID, UserID: a, Alias: "Acme", Kind: "name"}))
	require.NoError(t, repo.AddAlias(ctx, &domain.EntityAlias{EntityID: aco.ID, UserID: a, Alias: "Acme", Kind: "name"}), "an alias that exists is not an error")

	require.ErrorIs(t, repo.UpsertRelationship(ctx, &domain.EntityRelationship{UserID: a, SubjectID: aco.ID, Predicate: domain.RelCompetesWith, ObjectID: bco.ID, Confidence: 50}), domain.ErrValidation)
	require.ErrorIs(t, repo.RecordObservation(ctx, &domain.EntityObservation{UserID: a, EntityID: bco.ID, Property: "p", Value: "v", ObservedAt: time.Now()}), domain.ErrValidation)

	// kind filter: the same alias names a company for A only when asked for a company
	require.NoError(t, repo.AddAlias(ctx, &domain.EntityAlias{EntityID: bperson.ID, UserID: b, Alias: "Acme", Kind: "name"}))
	require.NoError(t, repo.AddAlias(ctx, &domain.EntityAlias{EntityID: bco.ID, UserID: b, Alias: "Acme", Kind: "name"}))
	p, err := repo.FindEntityByAliasKind(ctx, b, domain.EntityPerson, "acme")
	require.NoError(t, err)
	require.Equal(t, bperson.ID, p.ID)
	c, err := repo.FindEntityByAliasKind(ctx, b, domain.EntityCompany, "ACME")
	require.NoError(t, err)
	require.Equal(t, bco.ID, c.ID)
	none, err := repo.FindEntityByAliasKind(ctx, b, domain.EntityTopic, "acme")
	require.NoError(t, err)
	require.Nil(t, none)
	other, err := repo.FindEntityByAliasKind(ctx, a, "", "acme")
	require.NoError(t, err)
	require.Equal(t, aco.ID, other.ID, "a user only ever finds their own entities")
}

// ---- the whole path: HTTP create -> task -> routine -> report, with RLS enforced ----

type e2eLLM struct{}

func (e2eLLM) Chat(_ context.Context, system string, _ []gemini.Turn) (string, error) {
	switch {
	case strings.Contains(system, "principal research investigator"):
		return `{"questions":[{"id":"q1","question":"What does AcmeProcure do?"}],"initial_queries":["acmeprocure overview"]}`, nil
	case strings.Contains(system, "evidence extraction engine"):
		return `{"findings":[{"topic":"Acme","claim":"AcmeProcure builds procurement software","quote":"AcmeProcure builds procurement software","question_id":"q1","confidence":90},
			{"topic":"Acme","claim":"AcmeProcure is profitable","quote":"AcmeProcure has been profitable since launch","confidence":99}]}`, nil
	case strings.Contains(system, "research gap auditor"):
		return `{"answers":[]}`, nil
	}
	return "Analysis of the findings.", nil
}

type e2eTools struct{}

func (e2eTools) Search(context.Context, string, int) ([]research.SearchHit, error) {
	return []research.SearchHit{{Title: "About", URL: "https://acme.example.com/about", Domain: "acme.example.com"}}, nil
}

func (e2eTools) Fetch(_ context.Context, u string) (*research.FetchedContent, error) {
	if u != "https://acme.example.com/about" {
		return nil, errors.New("unexpected fetch " + u)
	}
	text := "AcmeProcure builds procurement software for mid-size manufacturers."
	return &research.FetchedContent{URL: u, Title: "About", Domain: "acme.example.com", Text: text, Status: 200, ContentHash: domain.DescriptionHash(text)}, nil
}

func (e *rlsEnv) researchServer(t *testing.T, eng *research.Engine, repo *repository.ResearchRepository) *httptest.Server {
	t.Helper()
	taskRepo := repository.NewPostgresTaskRepository(e.Scoped)
	approvalRepo := repository.NewPostgresApprovalRepository(e.Scoped)
	agentRepo := repository.NewPostgresAgentRepository(e.Scoped)
	artifactRepo := repository.NewPostgresArtifactRepository(e.Scoped)
	workerDefRepo := repository.NewPostgresWorkerDefinitionRepository(e.Scoped)
	orchRepo := repository.NewPostgresOrchestrationRepository(e.Scoped)
	hostRepo := repository.NewPostgresHostRepository(e.Scoped)
	engine := service.NewTaskEngine(taskRepo, approvalRepo, nil)
	factory := service.NewAgentFactoryService(workerDefRepo, e.ManagerReg, nil)
	orch := service.NewJJOrchestrator(engine, orchRepo, taskRepo, e.ManagerReg, factory, nil, nil)
	storageSvc := storage.NewStorageService(storage.NewMockStorageProvider(), artifactRepo, "griffin-test-artifacts", nil)
	cfg := *e.Config
	cfg.MaxUploadBytes = 1 << 20
	cfg.HostsEnabled = true
	srv := api.NewServer(api.ServerDependencies{
		Config: &cfg, DB: e.DB, Engine: engine, TaskRepo: taskRepo, ApprovalRepo: approvalRepo, AgentRepo: agentRepo,
		ArtifactRepo: artifactRepo, StorageService: storageSvc, WorkerDefRepo: workerDefRepo, OrchRepo: orchRepo,
		Orchestrator: orch, Factory: factory, ManagerRegistry: e.ManagerReg, UserTx: e.UserTx, HostRepo: hostRepo,
		PolicyRepo: repository.NewPostgresPolicyRepository(e.Scoped), ResearchRepo: repo, ResearchEngine: eng,
	})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func TestResearchRunEndToEndUnderRLS(t *testing.T) {
	e := setupRLS(t, 2, "")
	ctx := context.Background()
	repo := repository.NewResearchRepository(e.Scoped)
	eng := research.NewEngine(repo, nil, e2eTools{}, research.NewRouter(e2eLLM{}, e2eLLM{}), nil)
	ts := e.researchServer(t, eng, repo)

	a := CreateTestUser(t, e.DB, "research_e2e_a@example.com")
	b := CreateTestUser(t, e.DB, "research_e2e_b@example.com")
	jwtA, jwtB := GenerateTestJWT(a, "research_e2e_a@example.com"), GenerateTestJWT(b, "research_e2e_b@example.com")

	// 1. Create through the route: it returns at once, and a task (not a goroutine) carries the run.
	code, body := doJSON(t, ts, "POST", "/api/v1/research", jwtA, nil, map[string]any{"objective": "Research AcmeProcure", "depth": "quick"})
	require.Equal(t, http.StatusAccepted, code, string(body))
	var created struct {
		Data domain.ResearchRun `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &created))
	runID := created.Data.ID
	require.Equal(t, domain.PhaseRequestReceived, created.Data.Status)
	title := domain.ResearchRunTitlePrefix + runID.String()
	require.Equal(t, 1, countRows(t, ctx, e.DB.Pool, `SELECT count(*) FROM tasks WHERE user_id=$1 AND title=$2 AND status='QUEUED'`, a, title))
	require.Zero(t, countRows(t, ctx, e.DB.Pool, `SELECT count(*) FROM research_steps WHERE run_id=$1`, runID), "nothing ran in the request")

	// 2. One invocation of the routine under a short deadline does one step and schedules the next task.
	var taskID uuid.UUID
	require.NoError(t, e.DB.Pool.QueryRow(ctx, `SELECT id FROM tasks WHERE user_id=$1 AND title=$2 AND status='QUEUED'`, a, title).Scan(&taskID))
	task, err := e.Engine.GetTask(ctx, taskID)
	require.NoError(t, err)
	short, cancel := context.WithTimeout(database.WithSystem(ctx), 6*time.Second)
	defer cancel()
	lists := &service.TrackerLists{ResearchRunner: eng}
	answer, how := lists.RunInCode(short, task, nil, func(c context.Context, nt *domain.Task) error {
		_, err := e.Engine.CreateTask(c, nt, nil)
		return err
	})
	require.Equal(t, service.Answered, how)
	require.Contains(t, answer, "next task")
	_, err = e.DB.Pool.Exec(ctx, `UPDATE tasks SET status='COMPLETED' WHERE id=$1`, taskID)
	require.NoError(t, err)
	require.Equal(t, 1, countRows(t, ctx, e.DB.Pool, `SELECT count(*) FROM tasks WHERE user_id=$1 AND title=$2 AND status='QUEUED'`, a, title), "the continuation task")
	require.Equal(t, 1, countRows(t, ctx, e.DB.Pool, `SELECT count(*) FROM research_steps WHERE run_id=$1 AND status='done'`, runID))

	// 3. A host claims the rest through the real claim route, until the run is finished.
	code, body = doJSON(t, ts, "POST", "/api/v1/hosts", jwtA, nil, map[string]any{"name": "a-laptop", "kind": "laptop", "capabilities": []string{"agent"}})
	require.Equal(t, http.StatusCreated, code, string(body))
	var env envData
	require.NoError(t, json.Unmarshal(body, &env))
	var reg struct{ Token string }
	require.NoError(t, json.Unmarshal(env.Data, &reg))
	bearer := map[string]string{"Authorization": "Bearer " + reg.Token}
	code, body = doJSON(t, ts, "POST", "/api/v1/host/heartbeat", "", bearer, map[string]any{"capabilities": []string{"agent"}})
	require.Equal(t, http.StatusOK, code, string(body))
	for i := 0; i < 20; i++ {
		code, body = doJSON(t, ts, "POST", "/api/v1/host/claim", "", bearer, nil)
		if code == http.StatusNoContent && countRows(t, ctx, e.DB.Pool, `SELECT count(*) FROM tasks WHERE user_id=$1 AND title=$2 AND status='QUEUED'`, a, title) == 0 {
			break
		}
		require.Contains(t, []int{http.StatusNoContent, http.StatusOK}, code, string(body))
	}

	// 4. A reads a finished, honest run.
	code, body = doJSON(t, ts, "GET", "/api/v1/research/"+runID.String(), jwtA, nil, nil)
	require.Equal(t, http.StatusOK, code)
	var got struct {
		Data domain.ResearchRun `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &got))
	require.Equal(t, domain.PhaseCompleted, got.Data.Status)
	require.Equal(t, domain.StopAnswered, got.Data.StopReason)
	require.Len(t, got.Data.Findings, 2)
	require.Contains(t, got.Data.ReportMarkdown, "Coverage:** complete")

	code, body = doJSON(t, ts, "GET", "/api/v1/research/"+runID.String()+"/evidence", jwtA, nil, nil)
	require.Equal(t, http.StatusOK, code)
	var evs struct {
		Data []domain.EvidenceItem `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &evs))
	require.Len(t, evs.Data, 2)
	byStatus := map[domain.EvidenceStatus]int{}
	for _, it := range evs.Data {
		byStatus[it.Status]++
		if it.Status == domain.EvidenceSupported {
			require.NotNil(t, it.VerifiedAt)
			require.NotNil(t, it.SourceID)
		}
	}
	require.Equal(t, 1, byStatus[domain.EvidenceSupported])
	require.Equal(t, 1, byStatus[domain.EvidenceUnverified], "the invented quote is not supported")
	require.Equal(t, 1, countRows(t, ctx, e.DB.Pool, `SELECT count(*) FROM research_sources WHERE run_id=$1 AND content_hash IS NOT NULL AND canonical_url='https://acme.example.com/about'`, runID))

	// 5. B cannot see or touch A's run, over HTTP or through an RLS-scoped connection.
	for _, path := range []string{"/api/v1/research/" + runID.String(), "/api/v1/research/" + runID.String() + "/evidence"} {
		code, body = doJSON(t, ts, "GET", path, jwtB, nil, nil)
		if path == "/api/v1/research/"+runID.String() {
			require.Equal(t, http.StatusNotFound, code, path)
		}
		require.NotContains(t, string(body), "AcmeProcure builds procurement software", path)
	}
	code, body = doJSON(t, ts, "GET", "/api/v1/research", jwtB, nil, nil)
	require.Equal(t, http.StatusOK, code)
	require.NotContains(t, string(body), runID.String())
	code, _ = doJSON(t, ts, "POST", "/api/v1/research/"+runID.String()+"/cancel", jwtB, nil, nil)
	require.Equal(t, http.StatusNotFound, code)

	tx, err := e.UserTx.Begin(ctx, b)
	require.NoError(t, err)
	for _, tbl := range []string{"research_runs", "research_steps", "research_sources", "evidence_items"} {
		require.Zero(t, countRows(t, ctx, tx, `SELECT count(*) FROM `+tbl), "user B sees rows of %s", tbl)
	}
	require.NoError(t, tx.Rollback(ctx))
	txA, err := e.UserTx.Begin(ctx, a)
	require.NoError(t, err)
	require.Equal(t, 1, countRows(t, ctx, txA, `SELECT count(*) FROM research_runs`))
	require.Equal(t, 2, countRows(t, ctx, txA, `SELECT count(*) FROM evidence_items`))
	require.NoError(t, txA.Rollback(ctx))
}

package tests

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
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

	foundByAlias, err := repo.FindEntityByAlias(ctx, owner, "acme procure")
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

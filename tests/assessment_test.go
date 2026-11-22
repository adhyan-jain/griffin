package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/api"
	"github.com/griffin/core/internal/database"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/jobs"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/service"
)

const (
	assessStrong = `You will design and build backend services in Go and Python for our payments platform at scale. You will own features end to end: the API, the Postgres schema and migrations, Redis caching and the rollout, and watch latency and throughput after release.
You will work on distributed systems problems such as retries and idempotency across microservices on AWS with Docker, CI/CD and observability. You will pair program with senior engineers and have a mentor. Open to students graduating in 2028. This is a 6 month internship in Bengaluru, India.`
	assessUSOnly = `This is a remote role for our backend team. Candidates must be authorized to work in the United States. You will build APIs in Go with Postgres and Redis, review code with senior engineers, and ship services to production on AWS with Docker.`
)

// exerciseAssessments runs the assess routine against a real database through the given pool: assessments are
// written and read back (JSON, numeric, keys), a second run assesses nothing, a changed description is assessed again,
// the evidence rows carry the quotes, the filters and the ordering work, and a model answer is stored once.
func exerciseAssessments(t *testing.T, ctx context.Context, pool database.Querier, owner uuid.UUID) {
	t.Helper()
	repo := repository.NewOpportunityRepository(pool)
	var _ service.AssessStore = repo
	at := time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC)
	in := &service.Ingester{Store: repo, Now: func() time.Time { return at }}

	strong := ghJob("Razorpay", "Backend Engineer Intern", "https://boards.greenhouse.io/acme/jobs/1", "1", assessStrong, at)
	usOnly := ghJob("Acme Cloud", "Backend Intern", "https://boards.greenhouse.io/acme/jobs/2", "2", assessUSOnly, at)
	usOnly.Locations, usOnly.Compensation = []string{"Remote"}, ""
	lead := jobs.RawJob{Source: "internshala", SourceKind: jobs.Aggregator, EmployerName: "Pledge India Foundation", Title: "Software Development Intern",
		URL: "https://internshala.com/internship/detail/pledge", Locations: []string{"Remote (India)"}, Compensation: "Unpaid", FetchedAt: at}
	_, err := in.Ingest(ctx, owner, scan("greenhouse", "acme", jobs.ScanOK, at, strong, usOnly))
	require.NoError(t, err)
	_, err = in.Ingest(ctx, owner, scan("internshala", "/internships/x/", jobs.ScanOK, at, lead))
	require.NoError(t, err)

	asked := 0
	routine := &service.OpportunityAssess{Store: repo, ModelName: "test-model", Now: func() time.Time { return at }}
	res, err := routine.Run(ctx, owner, "")
	require.NoError(t, err)
	require.Equal(t, 3, res.Assessed, "%+v", res)
	require.Empty(t, res.Problems)
	require.Equal(t, 1, res.Review, "the lead has no text to judge eligibility from")
	require.Equal(t, 1, res.Ineligible)

	again, err := routine.Run(ctx, owner, "")
	require.NoError(t, err)
	require.Equal(t, 0, again.Assessed, "a second run assesses nothing: the stored keys match what the listings now say")

	// Read back: ordered by value, assessment attached, JSON and numeric columns intact.
	listed, err := repo.ListAssessed(ctx, owner, domain.AssessedFilter{})
	require.NoError(t, err)
	require.Len(t, listed, 3)
	require.Equal(t, "Razorpay", listed[0].Company, "best value first")
	a := listed[0].Assessment
	require.NotNil(t, a)
	require.Equal(t, domain.EligibilityConfirmed, a.Eligibility)
	require.Equal(t, domain.RoleBackend, a.RoleClass)
	require.NotNil(t, a.CompensationMonthlyINR)
	require.Equal(t, 60000.0, *a.CompensationMonthlyINR)
	require.NotEmpty(t, a.CompensationQuote)
	require.NotEmpty(t, a.SkillsMatched)
	require.Len(t, a.ValueBreakdown, 7)
	require.NotNil(t, a.Duration)
	require.Equal(t, "6 months", *a.Duration)
	require.True(t, listed[0].Assessment.ValueScore >= listed[1].Assessment.ValueScore && listed[1].Assessment.ValueScore >= listed[2].Assessment.ValueScore)

	// Filters.
	only := func(f domain.AssessedFilter) []string {
		rows, err := repo.ListAssessed(ctx, owner, f)
		require.NoError(t, err)
		var names []string
		for _, l := range rows {
			names = append(names, l.Company)
		}
		return names
	}
	require.Equal(t, []string{"Acme Cloud"}, only(domain.AssessedFilter{Eligibility: []string{domain.EligibilityIneligible}}))
	require.Equal(t, []string{"Pledge India Foundation"}, only(domain.AssessedFilter{Eligibility: []string{domain.EligibilityUnknown}}))
	require.Equal(t, []string{"Razorpay"}, only(domain.AssessedFilter{Classes: []string{a.ValueClass}}))
	require.ElementsMatch(t, []string{"Razorpay", "Pledge India Foundation"}, only(domain.AssessedFilter{Eligibility: []string{domain.EligibilityConfirmed, domain.EligibilityUnknown}}))
	require.Len(t, only(domain.AssessedFilter{Stages: []string{domain.StageVerified}}), 3)

	// Evidence: the pay and the blocker carry their quotes and the listing's link.
	var strongID, usID, leadID uuid.UUID
	for _, l := range listed {
		switch l.Company {
		case "Razorpay":
			strongID = l.ID
		case "Acme Cloud":
			usID = l.ID
		default:
			leadID = l.ID
		}
	}
	ev, err := repo.Evidence(ctx, owner, strongID)
	require.NoError(t, err)
	claims := map[string]string{}
	for _, e := range ev {
		claims[e.Claim] = e.Quote
		require.NotEmpty(t, e.SourceURL)
	}
	require.Contains(t, claims["compensation_monthly_inr"], "60,000")
	require.NotEmpty(t, claims["eligibility_fit"])
	ev, err = repo.Evidence(ctx, owner, usID)
	require.NoError(t, err)
	require.Len(t, ev, 1)
	require.Equal(t, "eligibility_blocker", ev[0].Claim)
	require.True(t, strings.Contains(strings.ToLower(ev[0].Quote), "authorized to work in the united states"), ev[0].Quote)
	ev, err = repo.Evidence(ctx, owner, leadID)
	require.NoError(t, err)
	require.Equal(t, "Unpaid", ev[0].Quote, "unpaid is stated pay, with its quote")

	// A changed description is assessed again, once; the evidence is replaced, not piled up.
	changed := ghJob("Razorpay", "Backend Engineer Intern", "https://boards.greenhouse.io/acme/jobs/1", "1", assessStrong+" We also use Kafka.", at.Add(time.Hour))
	_, err = in.Ingest(ctx, owner, scan("greenhouse", "acme", jobs.ScanOK, at.Add(time.Hour), changed, usOnly))
	require.NoError(t, err)
	third, err := routine.Run(ctx, owner, "")
	require.NoError(t, err)
	require.Equal(t, 1, third.Assessed)
	ev, err = repo.Evidence(ctx, owner, strongID)
	require.NoError(t, err)
	n := 0
	for _, e := range ev {
		if e.Claim == "compensation_monthly_inr" {
			n++
		}
	}
	require.Equal(t, 1, n)

	// The model is asked once per description: its verified answer is stored with the key it was asked at.
	vague := ghJob("Open Labs", "Research Intern", "https://boards.greenhouse.io/acme/jobs/3", "3",
		"Join our small research team as an intern. The work is on evaluating retrieval pipelines and writing the evaluation harness in Python. You will read papers and run experiments each week with the team. The team is fully remote across several time zones. Applicants should be enrolled in a degree programme in computer science or a related field.", at)
	vague.Locations, vague.Compensation = []string{"Remote"}, ""
	_, err = in.Ingest(ctx, owner, scan("greenhouse", "openlabs", jobs.ScanOK, at, vague))
	require.NoError(t, err)
	routine.Model = func(context.Context, string, string) (string, error) {
		asked++
		b, _ := json.Marshal(map[string]any{
			"eligibility":         "likely",
			"eligibility_reasons": []map[string]string{{"reason": "open to degree students", "quote": "enrolled in a degree programme in computer science"}},
			"ppo":                 map[string]string{"level": "yes", "quote": "every intern gets a return offer"}, // not in the posting: dropped
		})
		return string(b), nil
	}
	m, err := routine.Run(ctx, owner, "")
	require.NoError(t, err)
	require.Equal(t, 1, m.Assessed)
	require.Equal(t, 1, m.ModelCalls)
	rows, err := repo.ListAssessed(ctx, owner, domain.AssessedFilter{Eligibility: []string{domain.EligibilityLikely}})
	require.NoError(t, err)
	var labs *domain.Assessment
	for _, l := range rows {
		if l.Company == "Open Labs" {
			labs = l.Assessment
		}
	}
	require.NotNil(t, labs, "the verified model answer made it likely")
	require.NotNil(t, labs.ModelUsed)
	require.Equal(t, "test-model", *labs.ModelUsed)
	got, err := repo.GetAssessment(ctx, owner, rowID(t, repo, owner, "Open Labs"))
	require.NoError(t, err)
	require.NotNil(t, got.ModelKey)
	require.NotContains(t, string(got.ModelOutput), "return offer")
	// Rules change: re-assessed, but the model is not asked about the same description again.
	_, err = pool.Exec(ctx, `UPDATE listing_assessments SET assessor_version = 0 WHERE user_id = $1`, owner)
	require.NoError(t, err)
	r2, err := routine.Run(ctx, owner, "")
	require.NoError(t, err)
	require.Equal(t, 4, r2.Assessed)
	require.Equal(t, 0, r2.ModelCalls)
	require.Equal(t, 1, asked)

	// Another owner sees none of it.
	other := repository.NewOpportunityRepository(pool)
	none, err := other.ListAssessed(ctx, uuid.New(), domain.AssessedFilter{})
	require.NoError(t, err)
	require.Empty(t, none)
}

func rowID(t *testing.T, repo *repository.OpportunityRepository, owner uuid.UUID, company string) uuid.UUID {
	t.Helper()
	rows, err := repo.ListAssessed(context.Background(), owner, domain.AssessedFilter{})
	require.NoError(t, err)
	for _, l := range rows {
		if l.Company == company {
			return l.ID
		}
	}
	t.Fatalf("no listing for %s", company)
	return uuid.Nil
}

func TestOpportunityAssessmentsWorkAgainstTheDatabase(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	owner := CreateTestUser(t, env.DB, "assess@example.com")
	exerciseAssessments(t, context.Background(), env.DB.Pool, owner)
}

func TestOpportunityAssessmentsWorkThroughThePooler(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	owner := CreateTestUser(t, env.DB, "pooler-assess@example.com")

	url := os.Getenv("GRIFFIN_TEST_DB_URL")
	if url == "" {
		url = TestDBURL
	}
	cfg, err := pgxpool.ParseConfig(url)
	require.NoError(t, err)
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	defer pool.Close()
	exerciseAssessments(t, ctx, pool, owner)
}

func TestListingsRouteServesAssessmentsWithClassAndEligibilityFilters(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	owner := CreateTestUser(t, env.DB, "assess-api@example.com")
	repo := repository.NewOpportunityRepository(env.DB.Pool)
	env.Config.HostsEnabled = true
	at := time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC)
	in := &service.Ingester{Store: repo, Now: func() time.Time { return at }}
	usOnly := ghJob("Acme Cloud", "Backend Intern", "https://boards.greenhouse.io/acme/jobs/2", "2", assessUSOnly, at)
	usOnly.Locations, usOnly.Compensation = []string{"Remote"}, ""
	_, err := in.Ingest(ctx, owner, scan("greenhouse", "acme", jobs.ScanOK, at,
		ghJob("Razorpay", "Backend Engineer Intern", "https://boards.greenhouse.io/acme/jobs/1", "1", assessStrong, at), usOnly))
	require.NoError(t, err)
	_, err = in.Ingest(ctx, owner, scan("internshala", "/internships/x/", jobs.ScanOK, at, jobs.RawJob{Source: "internshala", SourceKind: jobs.Aggregator,
		EmployerName: "Pledge India Foundation", Title: "Software Development Intern", URL: "https://internshala.com/internship/detail/pledge", Locations: []string{"Remote (India)"}, Compensation: "Unpaid", FetchedAt: at}))
	require.NoError(t, err)

	server := api.NewServer(api.ServerDependencies{
		Config: env.Config, DB: env.DB, Engine: env.Engine, TaskRepo: env.TaskRepo, ApprovalRepo: env.ApprovalRepo, AgentRepo: env.AgentRepo,
		ArtifactRepo: env.ArtifactRepo, StorageService: env.StorageService, WorkerDefRepo: env.WorkerDefRepo, OrchRepo: env.OrchRepo,
		Orchestrator: env.Orchestrator, Factory: env.AgentFactory, ManagerRegistry: env.ManagerReg, ListingRepo: repo,
		HostRepo: repository.NewPostgresHostRepository(env.DB.Pool), PolicyRepo: repository.NewPostgresPolicyRepository(env.DB.Pool),
	})
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()
	auth := GenerateTestJWT(owner, "assess-api@example.com")
	get := func(path string) (int, []map[string]any) {
		req, _ := http.NewRequest("GET", ts.URL+path, nil)
		req.Header.Set("Authorization", auth)
		resp, err := ts.Client().Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		var body struct {
			Data []map[string]any `json:"data"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return resp.StatusCode, body.Data
	}

	// Before any assessment the listings are served, unassessed, and count as waiting for review.
	code, rows := get("/api/v1/listings")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, rows, 3)
	require.Nil(t, rows[0]["assessment"])
	_, rows = get("/api/v1/listings?eligibility=unknown")
	require.Len(t, rows, 3)

	_, err = (&service.OpportunityAssess{Store: repo, Now: func() time.Time { return at }}).Run(ctx, owner, "")
	require.NoError(t, err)

	code, rows = get("/api/v1/listings")
	require.Equal(t, http.StatusOK, code)
	require.Len(t, rows, 3)
	require.Equal(t, "Razorpay", rows[0]["company"], "sorted by value score, best first")
	var last float64 = 101
	for _, r := range rows {
		a := r["assessment"].(map[string]any)
		score := a["value_score"].(float64)
		require.LessOrEqual(t, score, last)
		last = score
		for _, k := range []string{"employment_class", "role_class", "eligibility", "eligibility_reasons", "location_fit", "grad_year_fit", "compensation_monthly_inr", "skills_matched", "value_breakdown", "value_class", "assessed_at"} {
			_, ok := a[k]
			require.True(t, ok, k)
		}
	}
	top := rows[0]["assessment"].(map[string]any)
	require.Equal(t, 60000.0, top["compensation_monthly_inr"])

	_, rows = get("/api/v1/listings?eligibility=ineligible")
	require.Len(t, rows, 1)
	require.Equal(t, "Acme Cloud", rows[0]["company"])
	_, rows = get("/api/v1/listings?eligibility=unknown")
	require.Len(t, rows, 1)
	require.Equal(t, "Pledge India Foundation", rows[0]["company"])
	_, rows = get("/api/v1/listings?class=" + top["value_class"].(string))
	require.Len(t, rows, 1)
	require.Equal(t, "Razorpay", rows[0]["company"])
	_, rows = get("/api/v1/listings?class=exceptional,strong,good,exploratory,low&eligibility=confirmed,likely")
	require.Len(t, rows, 1)
	code, _ = get("/api/v1/listings?class=amazing")
	require.Equal(t, http.StatusBadRequest, code, "a misspelt filter is an error, never a wider list")
	code, _ = get("/api/v1/listings?eligibility=maybe")
	require.Equal(t, http.StatusBadRequest, code)
}

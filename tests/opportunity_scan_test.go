package tests

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/database"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/jobs"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/service"
	"github.com/griffin/core/internal/sources/ats"
)

// scriptedBoards answers each token with the next scan of its script, the last one repeating.
type scriptedBoards struct {
	provider string
	script   map[string][]jobs.Scan
	n        map[string]int
	mu       sync.Mutex
}

func (s *scriptedBoards) Provider() string { return s.provider }

func (s *scriptedBoards) Read(_ context.Context, token string) jobs.Scan {
	s.mu.Lock()
	q := s.script[token]
	i := s.n[token]
	s.n[token]++
	s.mu.Unlock()
	var sc jobs.Scan
	switch {
	case len(q) == 0:
		sc = jobs.Scan{Status: jobs.ScanFailed, Error: "HTTP 404"}
	case i < len(q):
		sc = q[i]
	default:
		sc = q[len(q)-1]
	}
	sc.Source, sc.Target = s.provider, token
	now := time.Now().UTC()
	sc.StartedAt, sc.FinishedAt = now, now.Add(time.Second)
	return sc
}

func postingOf(token, id, title string) jobs.RawJob {
	return jobs.RawJob{Source: "lever", SourceKind: jobs.EmployerATS, BoardToken: token, ExternalID: id, Title: title,
		URL: "https://jobs.lever.co/" + token + "/" + id, Locations: []string{"Remote"}, Description: "Work with us.", FetchedAt: time.Now().UTC()}
}

// exerciseOpportunityScan runs the OPPORTUNITY SCAN routine against a real database: seeding, early-career
// filtering, an unnamed (Lever-style) posting taking its board's company, health after failures, closing only on a
// complete read, and a careers-page try recorded once. It runs through the normal and the simple (pooler) protocol.
func exerciseOpportunityScan(t *testing.T, ctx context.Context, pool database.Querier, owner uuid.UUID) {
	t.Helper()
	repo := repository.NewOpportunityRepository(pool)
	var _ service.OpportunityStore = repo

	lever := &scriptedBoards{provider: "lever", n: map[string]int{}, script: map[string][]jobs.Scan{
		"acme": {
			{Status: jobs.ScanOK, Pages: 1, Jobs: []jobs.RawJob{postingOf("acme", "1", "Software Engineer Intern"), postingOf("acme", "2", "Research Intern"), postingOf("acme", "3", "Internal Tools Engineer"), postingOf("acme", "4", "Staff Engineer")}},
			{Status: jobs.ScanOK, Pages: 1, Jobs: []jobs.RawJob{postingOf("acme", "1", "Software Engineer Intern")}}, // Research Intern was taken down
			{Status: jobs.ScanFailed, Error: "HTTP 503"},
		},
		"globex": {{Status: jobs.ScanFailed, Error: "HTTP 404"}},
	}}
	scan := &service.OpportunityScan{Store: repo, Adapters: map[string]ats.Adapter{"lever": lever}, Workers: 2, // default probing: no candidate has a domain here, so no page is fetched, but its queries run
		Seeds: []ats.Seed{
			{Name: "Acme Technologies", Domain: "acme.com", Kind: "startup", Region: "india", Provider: "lever", Token: "acme", VerifiedOn: "2026-11-22", Identity: "t"},
			{Name: "Globex", Domain: "globex.com", Kind: "enterprise", Region: "global", Provider: "lever", Token: "globex", VerifiedOn: "2026-11-22", Identity: "t"},
		}}

	res, err := scan.Run(ctx, owner)
	require.NoError(t, err)
	require.Equal(t, 2, res.Seeded)
	require.Equal(t, 2, res.Read)
	require.Equal(t, 1, res.OK)
	require.Equal(t, 1, res.Failed)
	require.Len(t, res.New, 2, res.Summary())

	boards, err := repo.ListBoards(ctx, owner)
	require.NoError(t, err)
	require.Len(t, boards, 2)
	byToken := map[string]*domain.ATSBoard{}
	for _, b := range boards {
		byToken[b.TokenOrURL] = b
	}
	require.Equal(t, domain.BoardActive, byToken["acme"].Status)
	require.NotNil(t, byToken["acme"].LastOKAt)
	require.NotNil(t, byToken["acme"].LastScanAt)
	require.Equal(t, 1, byToken["globex"].ConsecutiveFailures)
	require.NotNil(t, byToken["globex"].CompanyID)

	companies, err := repo.ListCompanies(ctx, owner, nil)
	require.NoError(t, err)
	require.Len(t, companies, 2)
	for _, c := range companies {
		require.Equal(t, domain.CompanyActive, c.Status)
		require.Equal(t, "seed", c.DiscoveredVia)
	}

	listings, err := repo.List(ctx, owner, nil, 50)
	require.NoError(t, err)
	require.Len(t, listings, 2)
	for _, l := range listings {
		require.Equal(t, "Acme Technologies", l.Company, "Lever gives no employer name: the board's company supplies it")
		require.Equal(t, domain.VerificationEmployer, l.Verification)
	}

	// a second run: Research Intern is gone from a complete read and closes; nothing else changes
	again, err := scan.Run(ctx, owner)
	require.NoError(t, err)
	require.Equal(t, 0, again.Seeded, "seeding is idempotent")
	require.Equal(t, 1, again.Closed, again.Summary())
	require.Empty(t, again.New)

	// a failed read of acme closes nothing
	third, err := scan.Run(ctx, owner)
	require.NoError(t, err)
	require.Equal(t, 0, third.Closed)
	open, err := repo.OpenFromTarget(ctx, owner, "lever", "acme")
	require.NoError(t, err)
	require.Len(t, open, 1)
	require.Equal(t, "Software Engineer Intern", open[0].Role)

	health, err := repo.SourceHealth(ctx, owner)
	require.NoError(t, err)
	require.NotEmpty(t, health)
}

func TestOpportunityScanWorksAgainstTheDatabase(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	owner := CreateTestUser(t, env.DB, "opportunity-scan@example.com")
	exerciseOpportunityScan(t, context.Background(), env.DB.Pool, owner)
}

func TestOpportunityScanWorksThroughThePooler(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	owner := CreateTestUser(t, env.DB, "opportunity-scan-pooler@example.com")
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
	exerciseOpportunityScan(t, ctx, pool, owner)
}

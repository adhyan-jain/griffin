package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/api"
	"github.com/griffin/core/internal/database"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/jobs"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/service"
)

func ghJob(company, title, link, id, description string, at time.Time) jobs.RawJob {
	return jobs.RawJob{Source: "greenhouse", SourceKind: jobs.EmployerATS, BoardToken: "acme", ExternalID: id, EmployerName: company, EmployerDomain: "acme.com",
		Title: title, URL: link, Locations: []string{"Bengaluru"}, Description: description, Compensation: "₹ 60,000 /month", FetchedAt: at}
}

func scan(source, target string, status jobs.ScanStatus, at time.Time, js ...jobs.RawJob) jobs.Scan {
	return jobs.Scan{Source: source, Target: target, Status: status, Jobs: js, Pages: 1, StartedAt: at, FinishedAt: at.Add(time.Second)}
}

// exerciseOpportunities runs the whole stage against a real database through the given pool: a legacy aggregator
// listing, an unkeyed legacy row, an employer board that takes over, change snapshots, closing only on a complete
// scan, reopening, scan history and source health. It runs through the normal and the simple (pooler) protocol.
func exerciseOpportunities(t *testing.T, ctx context.Context, pool database.Querier, owner uuid.UUID) {
	t.Helper()
	repo := repository.NewOpportunityRepository(pool)
	var _ service.IngestStore = repo
	day := time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC)
	at := func(h int) time.Time { return day.Add(time.Duration(h) * time.Hour) }
	clock := at(0)
	in := &service.Ingester{Store: repo, Now: func() time.Time { return clock }}

	// A listing the old sync wrote, and one written before the identity keys existed.
	legacy := domain.ListingFromPosting(owner, "internshala", "B", domain.Posting{Company: "Acme Technologies Pvt Ltd", Title: "Software Engineering Internship", Link: "https://internshala.com/internship/detail/acme-1?utm_source=x", Location: "Pune, India", Pay: "₹ 25,000 /month"}, at(-1))
	created, err := repo.Upsert(ctx, legacy)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, domain.SourceKindAggregator, legacy.SourceKind)
	require.Equal(t, domain.VerificationLead, legacy.Verification)
	require.Equal(t, "acme", legacy.EmployerKey)
	require.NotNil(t, legacy.CanonicalURL)
	require.Equal(t, "https://internshala.com/internship/detail/acme-1", *legacy.CanonicalURL)
	_, err = pool.Exec(ctx, `INSERT INTO job_discoveries (user_id, source, company, role, url, pay, stage) VALUES ($1, 'unstop', 'Beta Inc', 'Backend Intern', 'https://unstop.com/b/1', '₹ 20,000 /month', 'drafted')`, owner)
	require.NoError(t, err)

	// An aggregator page shows Beta again and a new Gamma: Beta is found (not duplicated), Gamma is a candidate company.
	res, err := in.Ingest(ctx, owner, scan("unstop", "backend", jobs.ScanOK, at(0),
		jobs.RawJob{Source: "unstop", SourceKind: jobs.Aggregator, EmployerName: "Beta", Title: "Backend Internship", URL: "https://unstop.com/b/1?ref=feed", Compensation: "₹ 20,000 /month", FetchedAt: at(0)},
		jobs.RawJob{Source: "unstop", SourceKind: jobs.Aggregator, EmployerName: "Gamma LLP", Title: "ML Intern", URL: "https://unstop.com/g/1", FetchedAt: at(0)}))
	require.NoError(t, err)
	require.Len(t, res.Created, 1)
	require.Equal(t, 1, res.Updated)
	all, err := repo.List(ctx, owner, nil, 50)
	require.NoError(t, err)
	require.Len(t, all, 3)
	for _, l := range all {
		require.Equal(t, domain.VerificationLead, l.Verification, l.Company)
		if l.Company == "Beta Inc" {
			require.Equal(t, domain.StageDrafted, l.Stage, "the application's stage survives")
			require.Equal(t, "unstop", l.Source)
			require.NotNil(t, l.LastSeenAt)
		}
	}
	cands, err := repo.ListCompanies(ctx, owner, []string{domain.CompanyCandidate})
	require.NoError(t, err)
	require.Len(t, cands, 2, "Beta and Gamma are candidates; Acme was never read by the new ingest")

	// The employer's own board lists the Acme role under a reworded title: same employer and role, one listing.
	clock = at(1)
	res, err = in.Ingest(ctx, owner, scan("greenhouse", "acme", jobs.ScanOK, at(1),
		ghJob("Acme", "Software Engineer Intern", "https://boards.greenhouse.io/acme/jobs/1?gh_src=li", "1", "Build APIs in Go.", at(1)),
		ghJob("Acme", "Data Intern", "https://boards.greenhouse.io/acme/jobs/2", "2", "Analyse things.", at(1))))
	require.NoError(t, err)
	require.Empty(t, res.Problems)
	require.Len(t, res.Created, 1, "only the Data role is new")
	all, _ = repo.List(ctx, owner, nil, 50)
	require.Len(t, all, 4)
	acme, err := repo.Get(ctx, owner, legacy.ID)
	require.NoError(t, err)
	require.Equal(t, "greenhouse", acme.Source)
	require.Equal(t, domain.SourceKindEmployerATS, acme.SourceKind)
	require.Equal(t, domain.VerificationEmployer, acme.Verification)
	require.Equal(t, "https://boards.greenhouse.io/acme/jobs/1?gh_src=li", acme.URL)
	require.NotNil(t, acme.EmployerURL)
	require.NotNil(t, acme.CompanyID)
	require.NotNil(t, acme.ATSBoardID)
	require.Equal(t, "acme", acme.SourceTarget)
	require.Equal(t, 0, acme.ReopenedCount)
	ev, err := repo.Evidence(ctx, owner, acme.ID)
	require.NoError(t, err)
	require.Len(t, ev, 1)
	require.Equal(t, "lead_url", ev[0].Claim)
	require.Equal(t, "https://internshala.com/internship/detail/acme-1?utm_source=x", ev[0].Value)
	companies, err := repo.ListCompanies(ctx, owner, []string{domain.CompanyActive})
	require.NoError(t, err)
	require.Len(t, companies, 1)
	require.Equal(t, "acme", companies[0].NormalizedName)
	require.NotNil(t, companies[0].Domain)
	boards, err := repo.ListBoards(ctx, owner)
	require.NoError(t, err)
	require.Len(t, boards, 1)
	require.Equal(t, "greenhouse", boards[0].Provider)
	require.NotNil(t, boards[0].LastOKAt)

	// Snapshots: one when first seen as the employer's, one more when the description changes, none when it does not.
	snaps, err := repo.Snapshots(ctx, owner, acme.ID)
	require.NoError(t, err)
	n := len(snaps)
	clock = at(2)
	_, err = in.Ingest(ctx, owner, scan("greenhouse", "acme", jobs.ScanOK, at(2),
		ghJob("Acme", "Software Engineer Intern", "https://boards.greenhouse.io/acme/jobs/1?gh_src=li", "1", "Build APIs in Go.", at(2)),
		ghJob("Acme", "Data Intern", "https://boards.greenhouse.io/acme/jobs/2", "2", "Analyse things.", at(2))))
	require.NoError(t, err)
	snaps, _ = repo.Snapshots(ctx, owner, acme.ID)
	require.Len(t, snaps, n, "nothing changed, nothing recorded")
	clock = at(3)
	_, err = in.Ingest(ctx, owner, scan("greenhouse", "acme", jobs.ScanOK, at(3),
		ghJob("Acme", "Software Engineer Intern", "https://boards.greenhouse.io/acme/jobs/1?gh_src=li", "1", "Build APIs in Go and Rust.", at(3)),
		ghJob("Acme", "Data Intern", "https://boards.greenhouse.io/acme/jobs/2", "2", "Analyse things.", at(3))))
	require.NoError(t, err)
	snaps, _ = repo.Snapshots(ctx, owner, acme.ID)
	require.Len(t, snaps, n+1)
	require.NotNil(t, snaps[len(snaps)-1].DescriptionHash)
	require.NotEqual(t, snaps[len(snaps)-1].DescriptionHash, snaps[len(snaps)-2].DescriptionHash)

	// A failed scan and a partial scan close nothing, even though the Data role is missing from the partial one.
	clock = at(4)
	failed := scan("greenhouse", "acme", jobs.ScanFailed, at(4))
	failed.Error, failed.Pages = "HTTP 503", 0
	_, err = in.Ingest(ctx, owner, failed)
	require.NoError(t, err)
	_, err = in.Ingest(ctx, owner, scan("greenhouse", "acme", jobs.ScanPartial, at(5), ghJob("Acme", "Software Engineer Intern", "https://boards.greenhouse.io/acme/jobs/1?gh_src=li", "1", "Build APIs in Go and Rust.", at(5))))
	require.NoError(t, err)
	open, err := repo.OpenFromTarget(ctx, owner, "greenhouse", "acme")
	require.NoError(t, err)
	require.Len(t, open, 2)
	health, err := repo.SourceHealth(ctx, owner)
	require.NoError(t, err)
	var gh *domain.SourceHealth
	for i := range health {
		if health[i].Source == "greenhouse" && health[i].Target == "acme" {
			gh = &health[i]
		}
	}
	require.NotNil(t, gh)
	require.Equal(t, "partial", gh.LastStatus)
	require.Equal(t, 1, gh.FailedSinceOK)
	require.Equal(t, 5, gh.Scans)
	require.NotNil(t, gh.LastOKAt)

	// A complete scan closes the Data role, and only it.
	clock = at(6)
	res, err = in.Ingest(ctx, owner, scan("greenhouse", "acme", jobs.ScanOK, at(6), ghJob("Acme", "Software Engineer Intern", "https://boards.greenhouse.io/acme/jobs/1?gh_src=li", "1", "Build APIs in Go and Rust.", at(6))))
	require.NoError(t, err)
	require.Equal(t, 1, res.Closed)
	closed, err := repo.List(ctx, owner, []string{domain.StageClosed}, 10)
	require.NoError(t, err)
	require.Len(t, closed, 1)
	require.Equal(t, "Data Intern", closed[0].Role)
	require.NotNil(t, closed[0].ClosedAt)
	untouched, err := repo.Get(ctx, owner, acme.ID)
	require.NoError(t, err)
	require.Nil(t, untouched.ClosedAt)
	beta := ""
	for _, l := range all {
		if l.Company == "Beta Inc" {
			beta = l.ID.String()
		}
	}
	bl, err := repo.Get(ctx, owner, uuid.MustParse(beta))
	require.NoError(t, err)
	require.Equal(t, domain.StageDrafted, bl.Stage, "another source's listing is never closed by this board")
	health, _ = repo.SourceHealth(ctx, owner)
	for _, h := range health {
		if h.Source == "greenhouse" {
			require.Equal(t, "ok", h.LastStatus)
			require.Equal(t, 0, h.FailedSinceOK)
			require.Equal(t, 6, h.Scans)
		}
	}

	// The role comes back: it reopens and is counted.
	clock = at(7)
	res, err = in.Ingest(ctx, owner, scan("greenhouse", "acme", jobs.ScanOK, at(7),
		ghJob("Acme", "Software Engineer Intern", "https://boards.greenhouse.io/acme/jobs/1?gh_src=li", "1", "Build APIs in Go and Rust.", at(7)),
		ghJob("Acme", "Data Intern", "https://boards.greenhouse.io/acme/jobs/2", "2", "Analyse things.", at(7))))
	require.NoError(t, err)
	require.Equal(t, 1, res.Reopened)
	back, err := repo.Get(ctx, owner, closed[0].ID)
	require.NoError(t, err)
	require.Equal(t, domain.StageVerified, back.Stage)
	require.Nil(t, back.ClosedAt)
	require.Equal(t, 1, back.ReopenedCount)

	// An application already confirmed keeps its stage when its posting disappears.
	require.NoError(t, repo.Move(ctx, owner, back.ID, domain.StageVerified, domain.StageDrafted, "d", nil))
	require.NoError(t, repo.Move(ctx, owner, back.ID, domain.StageDrafted, domain.StageApproved, "a", nil))
	require.NoError(t, repo.Move(ctx, owner, back.ID, domain.StageApproved, domain.StageSubmitting, "s", nil))
	require.NoError(t, repo.Move(ctx, owner, back.ID, domain.StageSubmitting, domain.StageConfirmed, "c", nil))
	clock = at(8)
	_, err = in.Ingest(ctx, owner, scan("greenhouse", "acme", jobs.ScanOK, at(8), ghJob("Acme", "Software Engineer Intern", "https://boards.greenhouse.io/acme/jobs/1?gh_src=li", "1", "Build APIs in Go and Rust.", at(8))))
	require.NoError(t, err)
	conf, err := repo.Get(ctx, owner, back.ID)
	require.NoError(t, err)
	require.Equal(t, domain.StageConfirmed, conf.Stage)
	require.NotNil(t, conf.ClosedAt)

	// The database itself refuses an employer-verified aggregator record.
	_, err = pool.Exec(ctx, `UPDATE job_discoveries SET verification = 'employer_verified' WHERE user_id = $1 AND id = $2`, owner, uuid.MustParse(beta))
	require.Error(t, err)
}

func TestOpportunityIngestWorksAgainstTheDatabase(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	owner := CreateTestUser(t, env.DB, "opportunities@example.com")
	exerciseOpportunities(t, context.Background(), env.DB.Pool, owner)
}

func TestListingsAndSourceHealthAreServedWithTheirNewFields(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	owner := CreateTestUser(t, env.DB, "opportunity-api@example.com")
	other := CreateTestUser(t, env.DB, "opportunity-api-other@example.com")
	repo := repository.NewOpportunityRepository(env.DB.Pool)
	at := time.Now().UTC().Truncate(time.Second)
	in := &service.Ingester{Store: repo}
	env.Config.HostsEnabled = true // the listings routes live in the hosts group

	_, err := in.Ingest(ctx, owner, scan("greenhouse", "acme", jobs.ScanOK, at, ghJob("Acme", "Software Engineer Intern", "https://boards.greenhouse.io/acme/jobs/1", "1", "d", at)))
	require.NoError(t, err)
	_, err = in.Ingest(ctx, owner, scan("internshala", "/internships/x/", jobs.ScanFailed, at))
	require.NoError(t, err)
	_, err = in.Ingest(ctx, other, scan("lever", "secret", jobs.ScanOK, at))
	require.NoError(t, err)

	server := api.NewServer(api.ServerDependencies{
		Config: env.Config, DB: env.DB, Engine: env.Engine, TaskRepo: env.TaskRepo, ApprovalRepo: env.ApprovalRepo, AgentRepo: env.AgentRepo,
		ArtifactRepo: env.ArtifactRepo, StorageService: env.StorageService, WorkerDefRepo: env.WorkerDefRepo, OrchRepo: env.OrchRepo,
		Orchestrator: env.Orchestrator, Factory: env.AgentFactory, ManagerRegistry: env.ManagerReg, ListingRepo: repo,
		HostRepo: repository.NewPostgresHostRepository(env.DB.Pool), PolicyRepo: repository.NewPostgresPolicyRepository(env.DB.Pool),
	})
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()
	get := func(path, auth string) *http.Response {
		req, _ := http.NewRequest("GET", ts.URL+path, nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := ts.Client().Do(req)
		require.NoError(t, err)
		return resp
	}
	auth := GenerateTestJWT(owner, "opportunity-api@example.com")

	require.Equal(t, http.StatusUnauthorized, get("/api/v1/sources/health", "").StatusCode)

	var listed struct {
		Data []map[string]any `json:"data"`
	}
	resp := get("/api/v1/listings", auth)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&listed))
	require.Len(t, listed.Data, 1)
	row := listed.Data[0]
	require.Equal(t, "employer_verified", row["verification"])
	require.Equal(t, "employer_ats", row["source_kind"])
	require.Equal(t, "Acme", row["company"])
	for _, k := range []string{"first_seen_at", "last_seen_at", "company_id", "employer_url", "canonical_url"} {
		require.NotNil(t, row[k], k)
	}
	_, hasClosed := row["closed_at"]
	require.False(t, hasClosed, "an open listing has no closed_at")

	var health struct {
		Data []domain.SourceHealth `json:"data"`
	}
	resp = get("/api/v1/sources/health", auth)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&health))
	require.Len(t, health.Data, 2, "the other owner's sources are not shown")
	byTarget := map[string]domain.SourceHealth{}
	for _, h := range health.Data {
		byTarget[h.Source+"|"+h.Target] = h
	}
	require.Equal(t, "ok", byTarget["greenhouse|acme"].LastStatus)
	require.Equal(t, "failed", byTarget["internshala|/internships/x/"].LastStatus)
	require.Equal(t, 1, byTarget["internshala|/internships/x/"].FailedSinceOK)
}

package tests

import (
	"bytes"
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

const packageProfile = `# Owner profile

- Full name: Asha Verma
- GitHub: https://github.com/asha-v
- Education: B.Tech Computer Science, Northfield Institute of Technology
- Expected graduation: 2028

## Verified facts

- [f1] ComputeHive: built a distributed task runner in Go with Postgres and Redis
- [f2] Palni: interned as an AI Engineer building LLM apps with RAG
`

// letterFor stands in for Gemini Flash: a letter that names only the profile's facts and the posting's company and role.
func letterFor(user string) string {
	line := func(key string) string {
		i := strings.Index(user, key)
		if i < 0 {
			return ""
		}
		return strings.SplitN(user[i+len(key):], "\n", 2)[0]
	}
	letter := "Dear Hiring Team,\n\nI am applying for the " + line("ROLE: ") + " role at " + line("COMPANY: ") + ". I am a B.Tech Computer Science student at Northfield Institute of Technology, expected to graduate in 2028. " +
		"I built ComputeHive, a distributed task runner in Go with Postgres and Redis. That is the kind of backend work your posting describes, and I would enjoy doing it on your team.\n\nThank you for considering my application.\n\nSincerely,\nAsha Verma"
	b, _ := json.Marshal(map[string]string{"cover_letter": letter, "why_join": ""})
	return string(b)
}

// exercisePackageStore runs the package rows through the given pool: save, replace keeping the id, hold, briefs, the
// once-only status change and delete. It runs through the normal and the simple (pooler) protocol.
func exercisePackageStore(t *testing.T, ctx context.Context, pool database.Querier, owner uuid.UUID) {
	t.Helper()
	repo := repository.NewOpportunityRepository(pool)
	var _ service.PrepStore = repo
	at := time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC)
	in := &service.Ingester{Store: repo, Now: func() time.Time { return at }}
	_, err := in.Ingest(ctx, owner, scan("greenhouse", "acme", jobs.ScanOK, at, ghJob("Razorpay", "Backend Engineer Intern", "https://boards.greenhouse.io/acme/jobs/1", "1", assessStrong, at)))
	require.NoError(t, err)
	rows, err := repo.List(ctx, owner, nil, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	l := rows[0]

	deadline := at.Add(72 * time.Hour)
	pkg := &domain.ApplicationPackage{
		UserID: owner, ListingID: l.ID, ResumeFile: domain.ResumeFiles["backend"], RoleClass: domain.RoleBackend,
		SelectedProjects:   []domain.PackageProject{{Project: "ComputeHive", Facts: []domain.PackageFact{{ID: "f1", Text: "built a distributed task runner in Go"}}, Skills: []string{"Go"}, Quote: "backend services in Go"}},
		CoverLetter:        "Dear Hiring Team,\nI am applying.",
		Answers:            []domain.PackageAnswer{{Question: "What is your graduation year?", Answer: "2028", Source: "your profile"}},
		RequiredDocuments:  []domain.RequiredDocument{{Name: "Resume", Status: "ready"}},
		Deadline:           &deadline,
		MissingInformation: []string{"Transcript: Griffin does not hold your transcript"},
		Evidence:           []domain.Reason{{Reason: "ComputeHive matches on Go", Quote: "backend services in Go"}},
		Status:             domain.PackageNeedsOwner, DescriptionHash: l.DescriptionHash, ProfileHash: "p1",
	}
	require.NoError(t, repo.SavePackage(ctx, pkg))
	require.NotEqual(t, uuid.Nil, pkg.ID)

	got, err := repo.PackageFor(ctx, owner, l.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, pkg.ID, got.ID)
	require.Equal(t, "ComputeHive", got.SelectedProjects[0].Project)
	require.Equal(t, "f1", got.SelectedProjects[0].Facts[0].ID)
	require.Equal(t, "2028", got.Answers[0].Answer)
	require.Equal(t, []string{"Transcript: Griffin does not hold your transcript"}, got.MissingInformation)
	require.Equal(t, "Resume", got.RequiredDocuments[0].Name)
	require.NotNil(t, got.Deadline)
	require.True(t, got.Deadline.Equal(deadline))
	require.Equal(t, "p1", got.ProfileHash)
	byID, err := repo.GetPackage(ctx, owner, pkg.ID)
	require.NoError(t, err)
	require.Equal(t, got.CoverLetter, byID.CoverLetter)

	// Another owner cannot read it.
	_, err = repo.GetPackage(ctx, uuid.New(), pkg.ID)
	require.ErrorIs(t, err, domain.ErrNotFound)

	// Saving again replaces the package and keeps its id; a replaced package starts without an approval.
	approval, task := uuid.New(), uuid.New()
	pkg.Status = domain.PackageDraft
	require.NoError(t, repo.SavePackage(ctx, pkg))
	require.NoError(t, repo.HoldPackage(ctx, owner, pkg.ID, approval, task))
	held, err := repo.GetPackage(ctx, owner, pkg.ID)
	require.NoError(t, err)
	require.Equal(t, domain.PackageReadyForApproval, held.Status)
	require.NotNil(t, held.ApprovalID)
	require.Equal(t, approval, *held.ApprovalID)
	require.ErrorIs(t, repo.HoldPackage(ctx, owner, pkg.ID, approval, task), domain.ErrConflict, "a held package is held once")

	briefs, err := repo.PackageBriefs(ctx, owner)
	require.NoError(t, err)
	require.Len(t, briefs, 1)
	require.Equal(t, domain.PackageReadyForApproval, briefs[l.ID].Status)
	require.Equal(t, 1, briefs[l.ID].Missing)

	// A decision is applied once.
	require.NoError(t, repo.SetPackageStatus(ctx, owner, pkg.ID, []string{domain.PackageReadyForApproval}, domain.PackageApproved))
	require.ErrorIs(t, repo.SetPackageStatus(ctx, owner, pkg.ID, []string{domain.PackageReadyForApproval}, domain.PackageRejected), domain.ErrConflict)
	require.NoError(t, repo.DeletePackage(ctx, owner, pkg.ID))
	none, err := repo.PackageFor(ctx, owner, l.ID)
	require.NoError(t, err)
	require.Nil(t, none)
}

func TestApplicationPackagesWorkAgainstTheDatabase(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	owner := CreateTestUser(t, env.DB, "package-store@example.com")
	exercisePackageStore(t, context.Background(), env.DB.Pool, owner)
}

func TestApplicationPackagesWorkThroughThePooler(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	owner := CreateTestUser(t, env.DB, "package-store-pooler@example.com")
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
	exercisePackageStore(t, ctx, pool, owner)
}

// TestApplicationPackageApprovalFlow runs a package through the real routes and the real approvals table: prepare
// moves the listing to drafted and puts an approval of type application_package on the approvals page; approving it
// moves the listing to approved and makes the existing submit task, which is gated at send time; rejecting moves the
// listing back to verified. Nothing is sent.
func TestApplicationPackageApprovalFlow(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	owner := CreateTestUser(t, env.DB, "package-flow@example.com")
	repo := repository.NewOpportunityRepository(env.DB.Pool)
	env.Config.HostsEnabled = true
	at := time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC)
	in := &service.Ingester{Store: repo, Now: func() time.Time { return at }}
	_, err := in.Ingest(ctx, owner, scan("greenhouse", "acme", jobs.ScanOK, at,
		ghJob("Razorpay", "Backend Engineer Intern", "https://boards.greenhouse.io/acme/jobs/1", "1", assessStrong, at),
		ghJob("Acme Cloud", "Backend Intern", "https://boards.greenhouse.io/acme/jobs/3", "3", assessStrong+" Acme Cloud builds payments.", at)))
	require.NoError(t, err)
	_, err = (&service.OpportunityAssess{Store: repo, Now: func() time.Time { return at }}).Run(ctx, owner, "")
	require.NoError(t, err)

	calls := 0
	prep := &service.ApplicationPrep{Store: repo, Holder: env.Engine, ModelName: "fake-flash",
		Facts: func(context.Context, uuid.UUID) string { return packageProfile },
		Model: func(_ context.Context, _, user string) (string, error) { calls++; return letterFor(user), nil }}

	// What main.go wires: a decision on a package moves its listing, and an approved one starts the existing submit task.
	lists := &service.TrackerLists{Store: env.StorageService, Tasks: env.TaskRepo}
	env.Engine.OnPackageDecided(func(ctx context.Context, a *domain.Approval, approved bool) {
		prep.Decided(ctx, a, approved, func(c context.Context, sub *domain.Approval) {
			lists.StartSubmit(c, sub, func(c2 context.Context, task *domain.Task) error {
				_, err := env.Engine.CreateTask(c2, task, nil)
				return err
			})
		})
	})

	server := api.NewServer(api.ServerDependencies{
		Config: env.Config, DB: env.DB, Engine: env.Engine, TaskRepo: env.TaskRepo, ApprovalRepo: env.ApprovalRepo, AgentRepo: env.AgentRepo,
		ArtifactRepo: env.ArtifactRepo, StorageService: env.StorageService, WorkerDefRepo: env.WorkerDefRepo, OrchRepo: env.OrchRepo,
		Orchestrator: env.Orchestrator, Factory: env.AgentFactory, ManagerRegistry: env.ManagerReg, ListingRepo: repo, Prep: prep,
		HostRepo: repository.NewPostgresHostRepository(env.DB.Pool), PolicyRepo: repository.NewPostgresPolicyRepository(env.DB.Pool),
	})
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()
	auth := GenerateTestJWT(owner, "package-flow@example.com")
	do := func(method, path string, body any) (int, json.RawMessage) {
		var rd *bytes.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		} else {
			rd = bytes.NewReader(nil)
		}
		req, _ := http.NewRequest(method, ts.URL+path, rd)
		req.Header.Set("Authorization", auth)
		req.Header.Set("Content-Type", "application/json")
		resp, err := ts.Client().Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		var env struct {
			Data json.RawMessage `json:"data"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&env)
		return resp.StatusCode, env.Data
	}
	listings := func() map[string]map[string]any {
		_, data := do("GET", "/api/v1/listings", nil)
		var rows []map[string]any
		require.NoError(t, json.Unmarshal(data, &rows))
		out := map[string]map[string]any{}
		for _, r := range rows {
			out[r["company"].(string)] = r
		}
		return out
	}

	rows := listings()
	require.Nil(t, rows["Razorpay"]["package"])
	razorpay, acme := rows["Razorpay"]["id"].(string), rows["Acme Cloud"]["id"].(string)

	// Prepare: a package, a drafted listing and one approval of the new type.
	code, data := do("POST", "/api/v1/listings/"+razorpay+"/prepare", nil)
	require.Equal(t, http.StatusOK, code, string(data))
	var pkg domain.ApplicationPackage
	require.NoError(t, json.Unmarshal(data, &pkg))
	require.Equal(t, domain.PackageReadyForApproval, pkg.Status, "%v", pkg.MissingInformation)
	require.NotNil(t, pkg.ApprovalID)
	require.Contains(t, pkg.CoverLetter, "Razorpay")
	require.Equal(t, domain.ResumeFiles["backend"], pkg.ResumeFile)
	require.NotEmpty(t, pkg.SelectedProjects)
	require.Equal(t, "ComputeHive", pkg.SelectedProjects[0].Project)
	require.Equal(t, 1, calls)

	rows = listings()
	require.Equal(t, "drafted", rows["Razorpay"]["stage"])
	brief := rows["Razorpay"]["package"].(map[string]any)
	require.Equal(t, domain.PackageReadyForApproval, brief["status"])
	require.Equal(t, pkg.ApprovalID.String(), brief["approval_id"])

	code, data = do("GET", "/api/v1/listings/"+razorpay+"/package", nil)
	require.Equal(t, http.StatusOK, code)
	code, data = do("GET", "/api/v1/packages/"+pkg.ID.String(), nil)
	require.Equal(t, http.StatusOK, code, string(data))

	_, data = do("GET", "/api/v1/approvals?status=PENDING", nil)
	var approvals []domain.Approval
	require.NoError(t, json.Unmarshal(data, &approvals))
	require.Len(t, approvals, 1)
	require.Equal(t, domain.PackageApprovalAction, approvals[0].ActionType)
	pid, ok := domain.PackageIDOf(&approvals[0])
	require.True(t, ok)
	require.Equal(t, pkg.ID, pid)

	// Asking again costs nothing.
	code, _ = do("POST", "/api/v1/listings/"+razorpay+"/prepare", nil)
	require.Equal(t, http.StatusOK, code)
	require.Equal(t, 1, calls)

	// Approve: the listing is approved, the package is approved, and a submit task exists that still needs approval to act.
	code, data = do("POST", "/api/v1/approvals/"+approvals[0].ID.String()+"/respond", map[string]string{"decision": "APPROVE", "reason": "looks right"})
	require.Equal(t, http.StatusOK, code, string(data))
	require.Equal(t, "approved", listings()["Razorpay"]["stage"])
	approved, err := repo.GetPackage(ctx, owner, pkg.ID)
	require.NoError(t, err)
	require.Equal(t, domain.PackageApproved, approved.Status)
	all, err := env.TaskRepo.ListTasks(ctx, repository.TaskFilter{UserID: &owner, Limit: 100})
	require.NoError(t, err)
	var submit, held *domain.Task
	for _, tk := range all {
		if company, _ := domain.SubmitOf(tk.InputPayload); company == "Razorpay" {
			submit = tk
		}
		if domain.IsPackageTask(tk.Title) {
			held = tk
		}
	}
	require.NotNil(t, submit, "approving a package starts the existing submit task")
	require.Equal(t, domain.ToolAct, domain.ToolModeFor(submit), "the submit task acts on the owner's behalf, so it asks again before it runs")
	require.Contains(t, submit.Goal, "Dear Hiring Team")
	require.Contains(t, submit.Goal, domain.ResumeFiles["backend"])
	require.NotNil(t, held)
	require.Equal(t, domain.ToolNone, domain.ToolModeFor(held), "the task that carries the approval has no tools")
	ok, err = env.Engine.HasApprovedAction(ctx, submit, domain.ToolApprovalAction)
	require.NoError(t, err)
	require.False(t, ok, "nothing has approved the submit task's act tier: it cannot send yet")

	// Reject the other one: back to verified, no submit task.
	code, data = do("POST", "/api/v1/listings/"+acme+"/prepare", nil)
	require.Equal(t, http.StatusOK, code, string(data))
	var pkg2 domain.ApplicationPackage
	require.NoError(t, json.Unmarshal(data, &pkg2))
	require.NotNil(t, pkg2.ApprovalID)
	require.Equal(t, "drafted", listings()["Acme Cloud"]["stage"])
	code, data = do("POST", "/api/v1/approvals/"+pkg2.ApprovalID.String()+"/respond", map[string]string{"decision": "REJECT", "reason": "not this one"})
	require.Equal(t, http.StatusOK, code, string(data))
	require.Equal(t, "verified", listings()["Acme Cloud"]["stage"])
	rejected, err := repo.GetPackage(ctx, owner, pkg2.ID)
	require.NoError(t, err)
	require.Equal(t, domain.PackageRejected, rejected.Status)
	all, err = env.TaskRepo.ListTasks(ctx, repository.TaskFilter{UserID: &owner, Limit: 100})
	require.NoError(t, err)
	for _, tk := range all {
		company, _ := domain.SubmitOf(tk.InputPayload)
		require.NotEqual(t, "Acme Cloud", company, "a rejected package never starts a submission")
	}

	// Preparing the rejected listing again makes a fresh package and a fresh approval.
	code, data = do("POST", "/api/v1/listings/"+acme+"/prepare", nil)
	require.Equal(t, http.StatusOK, code, string(data))
	var pkg3 domain.ApplicationPackage
	require.NoError(t, json.Unmarshal(data, &pkg3))
	require.Equal(t, pkg2.ID, pkg3.ID)
	require.Equal(t, domain.PackageReadyForApproval, pkg3.Status)
	require.NotEqual(t, *pkg2.ApprovalID, *pkg3.ApprovalID)

	// A listing past drafted cannot be prepared again.
	code, _ = do("POST", "/api/v1/listings/"+razorpay+"/prepare", nil)
	require.Equal(t, http.StatusOK, code, "an approved package is returned as it is")
	require.Equal(t, 3, calls)
}

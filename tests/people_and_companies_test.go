package tests

import (
	"bytes"
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
	"github.com/griffin/core/internal/playbooks"
	"github.com/griffin/core/internal/playbooks/companies"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/service"
	"github.com/griffin/core/internal/storage"
)

// The Companies and Alumni pages show existing data (the companies table, the Alumni tracker list) with research
// attached. These tests run it all as the production roles do: row level security enforced.

type peopleEnv struct {
	*rlsEnv
	ts      *httptest.Server
	people  *repository.PeopleRepository
	opp     *repository.OpportunityRepository
	storage *storage.StorageService
	engine  *service.TaskEngine
}

func (e *rlsEnv) peopleServer(t *testing.T) *peopleEnv {
	t.Helper()
	taskRepo := repository.NewPostgresTaskRepository(e.Scoped)
	approvalRepo := repository.NewPostgresApprovalRepository(e.Scoped)
	agentRepo := repository.NewPostgresAgentRepository(e.Scoped)
	artifactRepo := repository.NewPostgresArtifactRepository(e.Scoped)
	workerDefRepo := repository.NewPostgresWorkerDefinitionRepository(e.Scoped)
	orchRepo := repository.NewPostgresOrchestrationRepository(e.Scoped)
	engine := service.NewTaskEngine(taskRepo, approvalRepo, nil)
	people := repository.NewPeopleRepository(e.Scoped)
	opp := repository.NewOpportunityRepository(e.Scoped)
	// the same wiring as the server's main
	engine.OnOutreachDecided((&service.AlumniPeople{People: people}).OutreachDecided)
	factory := service.NewAgentFactoryService(workerDefRepo, e.ManagerReg, nil)
	orch := service.NewJJOrchestrator(engine, orchRepo, taskRepo, e.ManagerReg, factory, nil, nil)
	storageSvc := storage.NewStorageService(storage.NewMockStorageProvider(), artifactRepo, "griffin-test-artifacts", nil)
	cfg := *e.Config
	cfg.MaxUploadBytes = 1 << 20
	cfg.AlumniGraduationCutoff = 2022
	srv := api.NewServer(api.ServerDependencies{
		Config: &cfg, DB: e.DB, Engine: engine, TaskRepo: taskRepo, ApprovalRepo: approvalRepo, AgentRepo: agentRepo,
		ArtifactRepo: artifactRepo, StorageService: storageSvc, WorkerDefRepo: workerDefRepo, OrchRepo: orchRepo,
		Orchestrator: orch, Factory: factory, ManagerRegistry: e.ManagerReg, UserTx: e.UserTx,
		ListingRepo: opp, PeopleRepo: people,
	})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return &peopleEnv{rlsEnv: e, ts: ts, people: people, opp: opp, storage: storageSvc, engine: engine}
}

func (p *peopleEnv) saveAlumniList(t *testing.T, user uuid.UUID, text string) {
	t.Helper()
	_, err := p.storage.SaveArtifact(database.WithSystem(context.Background()), storage.SaveArtifactInput{
		UserID: user, Title: "Alumni", Filename: "alumni-tracker.md", ArtifactType: "MARKDOWN", ContentType: "text/markdown; charset=utf-8",
		Body: bytes.NewReader([]byte(text)), Metadata: domain.TrackerListMetadata(storage.ListKind, "alumni"),
	})
	require.NoError(t, err)
}

const alumniFixture = domain.AlumniHeader + "\n" +
	"| Sara Iyer | 2021 | Google | ML Engineer | ACM VIT | https://www.linkedin.com/in/sara | x | not contacted | 2026-11-20 |\n" +
	"| Sara Iyer | | Google | Operations Manager | | https://www.linkedin.com/in/sara-iyer-2 | x | not contacted | 2026-11-20 |\n" +
	"| Dev Rao | 2023 | Razorpay | Backend Engineer | | https://www.linkedin.com/in/dev-rao | x | opted out 2026-11-21 | 2026-11-20 |\n" +
	"| Meera Das | 2019 | Razorpay | Software Engineer | | https://www.linkedin.com/in/meera | x | not contacted | 2026-11-20 |"

type personJSON struct {
	ID             uuid.UUID `json:"id"`
	Name           string    `json:"name"`
	GraduationYear *int      `json:"graduation_year"`
	MeetsCutoff    bool      `json:"meets_cutoff"`
	DoNotContact   bool      `json:"do_not_contact"`
	ContactState   string    `json:"contact_state"`
	Company        string    `json:"company"`
	Country        string    `json:"country"`
}

func (p *peopleEnv) list(t *testing.T, jwt, query string) []personJSON {
	t.Helper()
	code, body := doJSON(t, p.ts, "GET", "/api/v1/alumni"+query, jwt, nil, nil)
	require.Equal(t, http.StatusOK, code, string(body))
	var env struct {
		Data []personJSON `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &env))
	require.NotNil(t, env.Data, "an array, never null")
	return env.Data
}

func (p *peopleEnv) setup(t *testing.T) (uuid.UUID, string) {
	t.Helper()
	user := CreateTestUser(t, p.DB, "people_"+uuid.NewString()[:8]+"@example.com")
	return user, GenerateTestJWT(user, "x@example.com")
}

func TestAlumniImportIsIdempotentAndNeverMergesOrInvents(t *testing.T) {
	p := setupRLS(t, 2, "").peopleServer(t)
	ctx := context.Background()
	user, jwt := p.setup(t)
	p.saveAlumniList(t, user, alumniFixture)

	imp := func() (int, int) {
		code, body := doJSON(t, p.ts, "POST", "/api/v1/alumni/import", jwt, nil, nil)
		require.Equal(t, http.StatusOK, code, string(body))
		var env struct {
			Data struct{ Created, Updated, Skipped int } `json:"data"`
		}
		require.NoError(t, json.Unmarshal(body, &env))
		return env.Data.Created, env.Data.Updated
	}
	created, _ := imp()
	require.Equal(t, 4, created)
	rows := countRows(t, ctx, p.DB.Pool, `SELECT count(*) FROM canonical_entities WHERE user_id=$1 AND kind='person'`, user)
	require.Equal(t, 4, rows)

	created, updated := imp()
	require.Zero(t, created, "a second import creates nothing")
	require.Equal(t, 4, updated)
	require.Equal(t, rows, countRows(t, ctx, p.DB.Pool, `SELECT count(*) FROM canonical_entities WHERE user_id=$1 AND kind='person'`, user), "the same row count after a second import")

	// two people with one name and one company, different profile URLs, are two people
	require.Equal(t, 2, countRows(t, ctx, p.DB.Pool, `SELECT count(*) FROM canonical_entities WHERE user_id=$1 AND kind='person' AND name='Sara Iyer' AND metadata->>'company'='Google'`, user))

	// nothing invented: a row with no batch has no year, nothing has a country, nothing is marked verified
	require.Equal(t, 1, countRows(t, ctx, p.DB.Pool, `SELECT count(*) FROM canonical_entities WHERE user_id=$1 AND kind='person' AND name='Sara Iyer' AND graduation_year IS NULL`, user))
	require.Zero(t, countRows(t, ctx, p.DB.Pool, `SELECT count(*) FROM canonical_entities WHERE user_id=$1 AND kind='person' AND (metadata ? 'country' OR last_verified_at IS NOT NULL OR graduation_year_evidence_id IS NOT NULL)`, user))

	// the row that says "opted out" is do-not-contact; the role family is marked inferred
	people := p.list(t, jwt, "?do_not_contact=true")
	require.Len(t, people, 1)
	require.Equal(t, "Dev Rao", people[0].Name)
	require.Equal(t, "opted_out", people[0].ContactState)
	require.Equal(t, 3, countRows(t, ctx, p.DB.Pool, `SELECT count(*) FROM canonical_entities WHERE user_id=$1 AND kind='person' AND metadata->>'role_family'='tech' AND metadata->>'role_family_inferred'='true'`, user))

	// the owner's later changes survive a re-import
	sara := p.list(t, jwt, "?company=google")
	require.Len(t, sara, 2)
	code, body := doJSON(t, p.ts, "PATCH", "/api/v1/alumni/"+sara[0].ID.String(), jwt, nil, map[string]any{"contact_state": "replied"})
	require.Equal(t, http.StatusOK, code, string(body))
	imp()
	var state string
	require.NoError(t, p.DB.Pool.QueryRow(ctx, `SELECT contact_state FROM canonical_entities WHERE id=$1`, sara[0].ID).Scan(&state))
	require.Equal(t, "replied", state)

	// another owner sees none of it
	_, jwtB := p.setup(t)
	require.Empty(t, p.list(t, jwtB, ""))
}

func TestUnknownGraduationYearIsExcludedFromTheCutoffFilter(t *testing.T) {
	p := setupRLS(t, 2, "").peopleServer(t)
	user, jwt := p.setup(t)
	p.saveAlumniList(t, user, alumniFixture)
	code, body := doJSON(t, p.ts, "POST", "/api/v1/alumni/import", jwt, nil, nil)
	require.Equal(t, http.StatusOK, code, string(body))

	names := func(ps []personJSON) []string {
		var out []string
		for _, x := range ps {
			out = append(out, x.Name)
		}
		return out
	}
	require.ElementsMatch(t, []string{"Meera Das", "Sara Iyer"}, names(p.list(t, jwt, "?grad_year_max=2022")), "2021 and 2019 pass; 2023 and the unknown year do not")
	all := p.list(t, jwt, "")
	require.Len(t, all, 4)
	for _, x := range all {
		if x.GraduationYear == nil {
			require.False(t, x.MeetsCutoff, "an unknown year never meets the cutoff")
		}
	}
	require.Equal(t, []string{"Meera Das"}, names(p.list(t, jwt, "?grad_year_max=2020")))
	require.ElementsMatch(t, []string{"Dev Rao", "Meera Das"}, names(p.list(t, jwt, "?role_family=tech&company=razor&grad_year_max=2023")))
	require.Equal(t, []string{"Dev Rao"}, names(p.list(t, jwt, "?company=razor&grad_year_max=2023&do_not_contact=true")))
	require.Len(t, p.list(t, jwt, "?role_family=operations"), 1)
	require.Empty(t, p.list(t, jwt, "?country=India"), "no country is known, so none matches")
}

func TestDoNotContactPersonCannotBeDraftedOrProposed(t *testing.T) {
	p := setupRLS(t, 2, "").peopleServer(t)
	ctx := database.WithSystem(context.Background())
	user, jwt := p.setup(t)
	p.saveAlumniList(t, user, alumniFixture)
	doJSON(t, p.ts, "POST", "/api/v1/alumni/import", jwt, nil, nil)

	var dev, meera, sara personJSON
	for _, x := range p.list(t, jwt, "") {
		switch {
		case x.Name == "Dev Rao":
			dev = x
		case x.Name == "Meera Das":
			meera = x
		case x.Name == "Sara Iyer" && x.GraduationYear != nil:
			sara = x
		}
	}
	draft := map[string]any{"draft_text": "Hi, loved your talk.", "channel": "linkedin"}
	tasksBefore := countRows(t, ctx, p.DB.Pool, `SELECT count(*) FROM tasks WHERE user_id=$1`, user)

	// the row's own "opted out" status made Dev do-not-contact
	code, body := doJSON(t, p.ts, "POST", "/api/v1/alumni/"+dev.ID.String()+"/outreach", jwt, nil, draft)
	require.Equal(t, http.StatusConflict, code, string(body))

	// the owner flags Sara by hand: drafting is refused, and the DM batch never proposes her
	code, body = doJSON(t, p.ts, "PATCH", "/api/v1/alumni/"+sara.ID.String(), jwt, nil, map[string]any{"do_not_contact": true})
	require.Equal(t, http.StatusOK, code, string(body))
	code, body = doJSON(t, p.ts, "POST", "/api/v1/alumni/"+sara.ID.String()+"/outreach", jwt, nil, draft)
	require.Equal(t, http.StatusConflict, code, string(body))
	require.Equal(t, tasksBefore, countRows(t, ctx, p.DB.Pool, `SELECT count(*) FROM tasks WHERE user_id=$1`, user), "a refused draft makes no task")
	require.Zero(t, countRows(t, ctx, p.DB.Pool, `SELECT count(*) FROM approvals WHERE user_id=$1`, user))

	lists := &service.TrackerLists{Store: p.storage, Alumni: &service.AlumniPeople{Lists: p.storage, People: p.people}}
	answer, ok := lists.AlumniDMBatch(ctx, &domain.Task{UserID: user, Title: domain.AlumniDMBatchTitle, Goal: domain.AlumniDMBatchGoal})
	require.True(t, ok)
	require.NotContains(t, answer, "Dev Rao", "his row says opted out")
	require.NotContains(t, answer, `linkedin.com/in/sara"`, "the flagged Sara is never proposed")
	require.Contains(t, answer, "linkedin.com/in/sara-iyer-2", "the other Sara, same name and company, is another person")
	require.Contains(t, answer, "Meera Das")

	// a person who is fine can be drafted
	code, body = doJSON(t, p.ts, "POST", "/api/v1/alumni/"+meera.ID.String()+"/outreach", jwt, nil, draft)
	require.Equal(t, http.StatusCreated, code, string(body))
}

func TestApprovingAlumniOutreachSendsNothingAndOnlyRecordsApproval(t *testing.T) {
	p := setupRLS(t, 2, "").peopleServer(t)
	ctx := database.WithSystem(context.Background())
	user, jwt := p.setup(t)
	p.saveAlumniList(t, user, alumniFixture)
	doJSON(t, p.ts, "POST", "/api/v1/alumni/import", jwt, nil, nil)
	meera := p.list(t, jwt, "?company=razorpay&grad_year_max=2020")[0]

	code, body := doJSON(t, p.ts, "POST", "/api/v1/alumni/"+meera.ID.String()+"/outreach", jwt, nil, map[string]any{"draft_text": "Hi Meera", "channel": "email"})
	require.Equal(t, http.StatusCreated, code, string(body))
	var created struct {
		Data struct {
			Approval domain.Approval `json:"approval"`
			Person   personJSON      `json:"person"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &created))
	require.Equal(t, "drafted", created.Data.Person.ContactState)
	require.Equal(t, domain.AlumniOutreachApprovalAction, created.Data.Approval.ActionType)
	require.NotEqual(t, uuid.Nil, created.Data.Approval.TaskID, "the approval has a real task")
	var status string
	require.NoError(t, p.DB.Pool.QueryRow(ctx, `SELECT status FROM tasks WHERE id=$1`, created.Data.Approval.TaskID).Scan(&status))
	require.Equal(t, "AWAITING_APPROVAL", status)

	code, body = doJSON(t, p.ts, "POST", "/api/v1/approvals/"+created.Data.Approval.ID.String()+"/respond", jwt, nil, map[string]any{"decision": "APPROVE"})
	require.Equal(t, http.StatusOK, code, string(body))

	var state string
	require.NoError(t, p.DB.Pool.QueryRow(ctx, `SELECT contact_state FROM canonical_entities WHERE id=$1`, meera.ID).Scan(&state))
	require.Equal(t, "approved", state)
	require.NoError(t, p.DB.Pool.QueryRow(ctx, `SELECT status FROM tasks WHERE id=$1`, created.Data.Approval.TaskID).Scan(&status))
	require.Equal(t, "COMPLETED", status, "the carrier task is finished, never queued for a worker")
	require.Zero(t, countRows(t, ctx, p.DB.Pool, `SELECT count(*) FROM tasks WHERE user_id=$1 AND status IN ('QUEUED','RUNNING','ROUTED','CREATED')`, user), "nothing is left for anything to send")
	require.Equal(t, 1, countRows(t, ctx, p.DB.Pool, `SELECT count(*) FROM tasks WHERE user_id=$1`, user), "approving made no second task")

	// the owner sends by hand and says so
	code, body = doJSON(t, p.ts, "PATCH", "/api/v1/alumni/"+meera.ID.String(), jwt, nil, map[string]any{"contact_state": "sent_manually"})
	require.Equal(t, http.StatusOK, code, string(body))

	// approved or drafted cannot be set by hand
	code, _ = doJSON(t, p.ts, "PATCH", "/api/v1/alumni/"+meera.ID.String(), jwt, nil, map[string]any{"contact_state": "approved"})
	require.Equal(t, http.StatusBadRequest, code)
}

func TestCompanyAliasesAndDomainsResolveToOneRow(t *testing.T) {
	p := setupRLS(t, 2, "").peopleServer(t)
	ctx := database.WithSystem(context.Background())
	user, _ := p.setup(t)
	other, _ := p.setup(t)

	// the opportunity scan found "Acme Technologies Pvt. Ltd." first
	scan := &domain.Company{UserID: user, Name: "Acme Technologies Pvt. Ltd.", NormalizedName: domain.NormalizeEmployer("Acme Technologies Pvt. Ltd."), Kind: "unknown", DiscoveredVia: "greenhouse", Status: domain.CompanyActive}
	require.NoError(t, p.opp.UpsertCompany(ctx, scan))

	var ids []uuid.UUID
	for _, name := range []string{"Acme Inc", "acme.io", "ACME", "https://www.Acme.io/careers", "Acme"} {
		c, err := p.opp.ResolveCompany(ctx, user, name)
		require.NoError(t, err, name)
		ids = append(ids, c.ID)
	}
	for _, id := range ids {
		require.Equal(t, scan.ID, id, "every spelling resolves to the scan's row")
	}
	require.Equal(t, 1, countRows(t, ctx, p.DB.Pool, `SELECT count(*) FROM companies WHERE user_id=$1`, user), "no second company row")
	require.Equal(t, "acme.io", func() string {
		var d *string
		_ = p.DB.Pool.QueryRow(ctx, `SELECT domain FROM companies WHERE id=$1`, scan.ID).Scan(&d)
		if d == nil {
			return ""
		}
		return *d
	}(), "the domain is learned")

	// a domain finds its company even when the name differs
	c, err := p.opp.ResolveCompany(ctx, user, "acme.io")
	require.NoError(t, err)
	require.Equal(t, scan.ID, c.ID)

	// another owner gets their own row
	oc, err := p.opp.ResolveCompany(ctx, other, "Acme Inc")
	require.NoError(t, err)
	require.NotEqual(t, scan.ID, oc.ID)
	require.Equal(t, domain.CompanyCandidate, oc.Status, "a company only research names is a candidate, like the scan's")
}

func TestCompanyObservationsKeepHistoryAndShowOnThePages(t *testing.T) {
	p := setupRLS(t, 2, "").peopleServer(t)
	ctx := database.WithSystem(context.Background())
	user, jwt := p.setup(t)

	acmeDomain := "acme.io"
	c := &domain.Company{UserID: user, Name: "Acme", NormalizedName: "acme", Domain: &acmeDomain, Kind: "startup", DiscoveredVia: "greenhouse", Status: domain.CompanyActive}
	require.NoError(t, p.opp.UpsertCompany(ctx, c))
	_, err := p.DB.Pool.Exec(ctx, `INSERT INTO job_discoveries (user_id, source, company, role, url, company_id, source_kind, verification, employer_url)
		VALUES ($1,'greenhouse','Acme','Backend Intern','https://boards.greenhouse.io/acme/1',$2,'employer_ats','employer_verified',NULL),
		       ($1,'internshala','Acme','Old Intern','https://internshala.com/x',$2,'aggregator','lead',NULL)`, user, c.ID)
	require.NoError(t, err)
	_, err = p.DB.Pool.Exec(ctx, `UPDATE job_discoveries SET stage='closed', closed_at=NOW() WHERE role='Old Intern' AND user_id=$1`, user)
	require.NoError(t, err)

	evidence := func(claim string) *uuid.UUID {
		var id uuid.UUID
		require.NoError(t, p.DB.Pool.QueryRow(ctx, `INSERT INTO evidence_items (user_id, claim, extracted_value, excerpt_quote, source_url, status, confidence)
			VALUES ($1,$2,'v','a long enough quote','https://acme.io/blog','supported',90) RETURNING id`, user, claim).Scan(&id))
		return &id
	}
	obs := func(prop, value, src string, at time.Time, ev *uuid.UUID) bool {
		added, err := p.opp.AddCompanyObservation(ctx, &domain.CompanyObservation{UserID: user, CompanyID: c.ID, Property: prop, Value: value,
			SourceURL: src, ClaimOrigin: domain.OriginCompanyReported, ObservedAt: at, EvidenceID: ev})
		require.NoError(t, err)
		return added
	}
	old, recent := time.Now().Add(-90*24*time.Hour), time.Now().Add(-24*time.Hour)
	require.True(t, obs(domain.PropFunding, "$5M seed", "https://acme.io/blog/seed", old, evidence("seed")))
	require.True(t, obs(domain.PropFunding, "$20M Series A", "https://acme.io/blog/a", recent, evidence("series a")), "a new value adds a row")
	require.False(t, obs(domain.PropFunding, "$20M Series A", "https://acme.io/blog/a", recent, nil), "the same value from the same page is not stored twice")
	require.True(t, obs(domain.PropInternshipStipend, "INR 40,000 / month", "https://acme.io/jobs/1", recent, nil))
	require.Equal(t, 2, countRows(t, ctx, p.DB.Pool, `SELECT count(*) FROM company_observations WHERE company_id=$1 AND property='funding'`, c.ID), "both funding values are kept")
	_, err = p.opp.AddCompanyObservation(ctx, &domain.CompanyObservation{UserID: user, CompanyID: c.ID, Property: "vibes", Value: "x"})
	require.ErrorIs(t, err, domain.ErrValidation)

	// the list: the existing company, its open listings, the latest observation per property
	code, body := doJSON(t, p.ts, "GET", "/api/v1/companies", jwt, nil, nil)
	require.Equal(t, http.StatusOK, code, string(body))
	var list struct {
		Data []struct {
			ID           uuid.UUID `json:"id"`
			Name         string    `json:"name"`
			OpenListings int       `json:"open_listings"`
			Latest       []struct {
				Property    string `json:"property"`
				Value       string `json:"value"`
				ClaimOrigin string `json:"claim_origin"`
			} `json:"latest_observations"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &list))
	require.Len(t, list.Data, 1)
	require.Equal(t, 1, list.Data[0].OpenListings, "the closed listing is not counted")
	latest := map[string]string{}
	for _, o := range list.Data[0].Latest {
		latest[o.Property] = o.Value
	}
	require.Equal(t, map[string]string{"funding": "$20M Series A", "internship_stipend": "INR 40,000 / month"}, latest)

	// the detail: listings with the official link, the whole history with age, and the evidence
	code, body = doJSON(t, p.ts, "GET", "/api/v1/companies/"+c.ID.String(), jwt, nil, nil)
	require.Equal(t, http.StatusOK, code, string(body))
	var detail struct {
		Data struct {
			Listings []struct {
				Role        string  `json:"role"`
				OfficialURL *string `json:"official_url"`
				Open        bool    `json:"open"`
			} `json:"listings"`
			Observations []struct {
				Property string `json:"property"`
				Value    string `json:"value"`
				AgeDays  int    `json:"age_days"`
			} `json:"observations"`
			Evidence []struct{ Claim string } `json:"evidence"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &detail))
	require.Len(t, detail.Data.Listings, 2)
	for _, l := range detail.Data.Listings {
		if l.Role == "Backend Intern" {
			require.True(t, l.Open)
			require.NotNil(t, l.OfficialURL, "an employer's own posting is the official link")
		} else {
			require.False(t, l.Open)
			require.Nil(t, l.OfficialURL, "an aggregator's link is not official")
		}
	}
	require.Len(t, detail.Data.Observations, 3)
	require.Len(t, detail.Data.Evidence, 2)
	ages := map[string]int{}
	for _, o := range detail.Data.Observations {
		ages[o.Value] = o.AgeDays
	}
	require.GreaterOrEqual(t, ages["$5M seed"], 89)

	// another owner cannot read it
	_, jwtB := p.setup(t)
	code, _ = doJSON(t, p.ts, "GET", "/api/v1/companies/"+c.ID.String(), jwtB, nil, nil)
	require.Equal(t, http.StatusNotFound, code)
	code, body = doJSON(t, p.ts, "GET", "/api/v1/companies?limit=100000", jwtB, nil, nil)
	require.Equal(t, http.StatusOK, code)
	require.Contains(t, string(body), `"data":[]`)

	// the playbook records into the same row, from supported findings only
	pb := companies.NewPlaybook().WithStore(p.opp)
	reg := playbooks.Registry{"company_intelligence": pb}
	require.NotNil(t, reg.For("company_intelligence"))
	ev := evidence("hiring")
	run := &domain.ResearchRun{ID: uuid.New(), UserID: user, Status: domain.PhaseCompleted, Findings: []domain.ResearchFinding{
		{Topic: "ACME", Subject: "ACME", Property: "hiring signal", Claim: "Acme is hiring 12 engineers", ExtractedValue: "12 engineers", SourceURL: "https://acme.io/careers", EvidenceID: ev, Status: domain.EvidenceSupported},
		{Topic: "Acme", Subject: "Acme", Property: "revenue", Claim: "Acme earns $50M", ExtractedValue: "$50M", SourceURL: "https://blog.example.com/x", EvidenceID: ev, Status: domain.EvidenceUnverified},
		{Topic: "Acme", Subject: "Acme", Property: "profitability", Claim: "Acme is profitable", ExtractedValue: "profitable", SourceURL: "https://blog.example.com/y", EvidenceID: ev, Status: domain.EvidenceConflicting},
	}}
	res, err := pb.ProcessResults(ctx, run)
	require.NoError(t, err)
	require.Empty(t, res.Warnings)
	require.Equal(t, 1, countRows(t, ctx, p.DB.Pool, `SELECT count(*) FROM company_observations WHERE company_id=$1 AND property='hiring_signal' AND claim_origin='company_reported'`, c.ID))
	require.Zero(t, countRows(t, ctx, p.DB.Pool, `SELECT count(*) FROM company_observations WHERE company_id=$1 AND property IN ('revenue','profitability')`, c.ID), "unverified and conflicting findings record nothing")
	require.Equal(t, 1, countRows(t, ctx, p.DB.Pool, `SELECT count(*) FROM companies WHERE user_id=$1`, user), "ACME resolved to the existing row")
}

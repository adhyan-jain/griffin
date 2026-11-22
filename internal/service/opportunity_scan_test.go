package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/jobs"
	"github.com/griffin/core/internal/sources/ats"
)

// fakeAdapter answers a board's reads from a script: one scan per read, the last one repeating.
type fakeAdapter struct {
	provider string
	mu       sync.Mutex
	script   map[string][]jobs.Scan
	reads    map[string]int
	onRead   func(token string)
	// what each read's context allowed: the time left until its deadline, and how many reads had no deadline at all
	deadlines  []time.Duration
	noDeadline int
}

func newFake(provider string) *fakeAdapter {
	return &fakeAdapter{provider: provider, script: map[string][]jobs.Scan{}, reads: map[string]int{}}
}

func (f *fakeAdapter) Provider() string { return f.provider }

func (f *fakeAdapter) Read(ctx context.Context, token string) jobs.Scan {
	f.mu.Lock()
	if d, ok := ctx.Deadline(); ok {
		f.deadlines = append(f.deadlines, time.Until(d))
	} else {
		f.noDeadline++
	}
	n := f.reads[token]
	f.reads[token]++
	q := f.script[token]
	hook := f.onRead
	f.mu.Unlock()
	if hook != nil {
		hook(token)
	}
	var s jobs.Scan
	switch {
	case len(q) == 0:
		s = jobs.Scan{Status: jobs.ScanFailed, Error: "HTTP 404"}
	case n < len(q):
		s = q[n]
	default:
		s = q[len(q)-1]
	}
	s.Source, s.Target = f.provider, token
	if s.StartedAt.IsZero() {
		s.StartedAt, s.FinishedAt = ingestAt, ingestAt.Add(time.Second)
	}
	return s
}

func (f *fakeAdapter) read(token string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads[token]
}

func (f *fakeAdapter) set(token string, scans ...jobs.Scan) {
	f.mu.Lock()
	f.script[token] = scans
	f.mu.Unlock()
}

// board is a posting as an ATS adapter returns it: no employer name for providers that do not give one.
func board(provider, token, id, title string) jobs.RawJob {
	return jobs.RawJob{Source: provider, SourceKind: jobs.EmployerATS, BoardToken: token, ExternalID: id, Title: title,
		URL: "https://jobs.example/" + token + "/" + id, Locations: []string{"Bengaluru"}, FetchedAt: ingestAt}
}

func okScan(js ...jobs.RawJob) jobs.Scan {
	return jobs.Scan{Status: jobs.ScanOK, Jobs: js, Pages: 1}
}

func failScan(msg string) jobs.Scan { return jobs.Scan{Status: jobs.ScanFailed, Error: msg} }

func seeds(names ...string) []ats.Seed {
	var out []ats.Seed
	for _, n := range names {
		out = append(out, ats.Seed{Name: strings.ToUpper(n[:1]) + n[1:], Domain: n + ".com", Kind: "startup", Region: "india", Provider: "greenhouse", Token: n, VerifiedOn: "2026-11-22", Identity: "test"})
	}
	return out
}

func scanFor(m *memListings, fake *fakeAdapter, sd []ats.Seed) *OpportunityScan {
	return &OpportunityScan{Store: m, Adapters: map[string]ats.Adapter{"greenhouse": fake}, Seeds: sd, Workers: 1, MaxProbes: -1, Now: func() time.Time { return ingestAt }}
}

func boardOf(m *memListings, token string) *domain.ATSBoard {
	for _, b := range m.boards {
		if b.TokenOrURL == token {
			return b
		}
	}
	return nil
}

func TestFirstRunSeedsTheVerifiedEmployersAndLaterRunsAddNothing(t *testing.T) {
	m := &memListings{}
	fake := newFake("greenhouse")
	s := scanFor(m, fake, seeds("acme", "globex"))
	res, err := s.Run(context.Background(), ingestOwner)
	if err != nil || res.Seeded != 2 || len(m.boards) != 2 || len(m.companies) != 2 {
		t.Fatalf("%v %+v boards %d companies %d", err, res, len(m.boards), len(m.companies))
	}
	for _, c := range m.companies {
		if c.Status != domain.CompanyActive || c.DiscoveredVia != "seed" || c.Domain == nil || c.EvidenceURL == nil || !strings.HasPrefix(*c.EvidenceURL, "https://boards.greenhouse.io/") {
			t.Errorf("a seeded employer is active, with its domain and board page as evidence: %+v", c)
		}
	}
	again, _ := s.Run(context.Background(), ingestOwner)
	if again.Seeded != 0 || len(m.boards) != 2 || len(m.companies) != 2 {
		t.Fatalf("seeding is idempotent: %+v", again)
	}
}

func TestAnIgnoredCompanyIsNotSeededAgain(t *testing.T) {
	m := &memListings{}
	m.companies = append(m.companies, &domain.Company{ID: uuid.New(), Name: "Acme", NormalizedName: "acme", Status: domain.CompanyIgnored})
	s := scanFor(m, newFake("greenhouse"), seeds("acme", "globex"))
	res, _ := s.Run(context.Background(), ingestOwner)
	if res.Seeded != 1 || boardOf(m, "acme") != nil || boardOf(m, "globex") == nil {
		t.Fatalf("the owner's ignore stands: %+v", res)
	}
}

func TestOnlyInternshipsAndEarlyCareerRolesAreKeptAndTheyNameTheirEmployer(t *testing.T) {
	m := &memListings{}
	fake := newFake("greenhouse")
	fake.set("acme", okScan(
		board("greenhouse", "acme", "1", "Software Engineer Intern"),
		board("greenhouse", "acme", "2", "Internal Tools Engineer"),
		board("greenhouse", "acme", "3", "Staff Backend Engineer"),
		board("greenhouse", "acme", "4", "Intern - Frontend"),
		board("greenhouse", "acme", "5", "Sales Intern"),
	))
	s := scanFor(m, fake, seeds("acme"))
	res, err := s.Run(context.Background(), ingestOwner)
	if err != nil {
		t.Fatal(err)
	}
	if res.Postings != 5 || res.EarlyCareer != 2 || len(res.New) != 2 || len(m.rows) != 2 {
		t.Fatalf("stage 1 keeps the two internships: %+v rows %d", res, len(m.rows))
	}
	for _, l := range m.rows {
		if l.Company != "Acme" || l.Verification != domain.VerificationEmployer || l.SourceKind != domain.SourceKindEmployerATS || l.EmployerURL == nil {
			t.Errorf("an employer board posting, named for the board's company: %+v", l)
		}
	}
	if len(m.companies) != 1 {
		t.Fatalf("one employer, not one per spelling: %+v", m.companies)
	}
	sum := res.Summary()
	for _, want := range []string{"read 1 of 1", "2 are new employer-verified internships", "Software Engineer Intern at Acme", "Intern - Frontend at Acme"} {
		if !strings.Contains(sum, want) {
			t.Errorf("summary lacks %q:\n%s", want, sum)
		}
	}
}

func TestARunTwiceChangesNothingTheSecondTime(t *testing.T) {
	m := &memListings{}
	fake := newFake("greenhouse")
	fake.set("acme", okScan(board("greenhouse", "acme", "1", "Software Engineer Intern")))
	s := scanFor(m, fake, seeds("acme"))
	if _, err := s.Run(context.Background(), ingestOwner); err != nil {
		t.Fatal(err)
	}
	snaps := len(m.snapshots)
	res, _ := s.Run(context.Background(), ingestOwner)
	if len(res.New) != 0 || res.Changed != 0 || res.Closed != 0 || len(m.rows) != 1 || len(m.snapshots) != snaps {
		t.Fatalf("idempotent: %+v rows %d snapshots %d->%d", res, len(m.rows), snaps, len(m.snapshots))
	}
}

func TestAChangedPostingIsCountedAndAPostingTheEmployerRemovedIsClosed(t *testing.T) {
	m := &memListings{}
	fake := newFake("greenhouse")
	first := board("greenhouse", "acme", "1", "Software Engineer Intern")
	first.Description = "Build APIs."
	second := first
	second.Description = "Build APIs in Go. Now with a new team."
	fake.set("acme",
		okScan(first, board("greenhouse", "acme", "2", "Research Intern")),
		okScan(second, board("greenhouse", "acme", "2", "Research Intern")),
		okScan(second),
	)
	s := scanFor(m, fake, seeds("acme"))
	ctx := context.Background()
	if _, err := s.Run(ctx, ingestOwner); err != nil {
		t.Fatal(err)
	}
	res, _ := s.Run(ctx, ingestOwner)
	if res.Changed != 1 || res.Closed != 0 {
		t.Fatalf("one posting changed: %+v", res)
	}
	res, _ = s.Run(ctx, ingestOwner)
	if res.Closed != 1 || !strings.Contains(res.Summary(), "1 closed") {
		t.Fatalf("the Research Intern posting is gone from a complete read: %+v", res)
	}
	var closed int
	for _, l := range m.rows {
		if l.Stage == domain.StageClosed {
			closed++
		}
	}
	if closed != 1 {
		t.Fatalf("closed listings %d", closed)
	}
}

func TestAPartialOrFailedReadClosesNothing(t *testing.T) {
	m := &memListings{}
	fake := newFake("greenhouse")
	partial := okScan(board("greenhouse", "acme", "1", "Software Engineer Intern"))
	partial.Status, partial.Error = jobs.ScanPartial, "page 2 answered HTTP 500"
	fake.set("acme",
		okScan(board("greenhouse", "acme", "1", "Software Engineer Intern"), board("greenhouse", "acme", "2", "Research Intern")),
		partial, // lists one of the two, but is incomplete
		failScan("HTTP 503"),
	)
	s := scanFor(m, fake, seeds("acme"))
	ctx := context.Background()
	if _, err := s.Run(ctx, ingestOwner); err != nil {
		t.Fatal(err)
	}
	res, _ := s.Run(ctx, ingestOwner)
	if res.Partial != 1 || res.Closed != 0 || len(res.PartialBoards) != 1 || !strings.Contains(res.Summary(), "Read only partly, so nothing was closed") {
		t.Fatalf("a partial read closes nothing: %+v", res)
	}
	res, _ = s.Run(ctx, ingestOwner)
	if res.Failed != 1 || res.Closed != 0 || !strings.Contains(res.Summary(), "Could not read: Acme (greenhouse acme): HTTP 503") {
		t.Fatalf("a failed read closes nothing and is named: %+v\n%s", res, res.Summary())
	}
	for _, l := range m.rows {
		if l.Stage == domain.StageClosed || l.ClosedAt != nil {
			t.Fatalf("nothing was closed: %+v", l)
		}
	}
	if b := boardOf(m, "acme"); b.ConsecutiveFailures != 1 || b.Status != domain.BoardActive {
		t.Fatalf("one failure is not a dead board: %+v", b)
	}
}

func TestABoardIsBrokenAfterFiveFailuresInARowAndIsRetriedAWeekLater(t *testing.T) {
	if domain.BoardBrokenAfter != 5 {
		t.Fatalf("a board is broken after 5 failures in a row, not %d", domain.BoardBrokenAfter)
	}
	m := &memListings{}
	fake := newFake("greenhouse")
	fake.set("acme", failScan("HTTP 404"))
	s := scanFor(m, fake, seeds("acme"))
	s.Now = nil // the store stamps scans with the real clock, so the due check must use it too
	ctx := context.Background()
	for i := 1; i <= 5; i++ {
		res, _ := s.Run(ctx, ingestOwner)
		b := boardOf(m, "acme")
		if b.ConsecutiveFailures != i {
			t.Fatalf("run %d: failures %d", i, b.ConsecutiveFailures)
		}
		if (i < 5) != (b.Status == domain.BoardActive) {
			t.Fatalf("run %d: status %s", i, b.Status)
		}
		if i == 5 && (len(res.NowBroken) != 1 || !strings.Contains(res.Summary(), "Marked broken")) {
			t.Fatalf("the run that breaks a board says so: %+v", res)
		}
	}
	reads := fake.read("acme")
	if res, _ := s.Run(ctx, ingestOwner); res.Boards != 0 || fake.read("acme") != reads {
		t.Fatalf("a broken board is not read again straight away: %+v", res)
	}
	// a week later it is tried once more, and a good read makes it active again
	b := boardOf(m, "acme")
	old := time.Now().Add(-8 * 24 * time.Hour)
	b.LastScanAt = &old
	fake.set("acme", okScan(board("greenhouse", "acme", "1", "Software Engineer Intern")))
	res, _ := s.Run(ctx, ingestOwner)
	if res.OK != 1 || b.Status != domain.BoardActive || b.ConsecutiveFailures != 0 || b.LastOKAt == nil || len(m.rows) != 1 {
		t.Fatalf("recovered: %+v board %+v", res, b)
	}
}

func TestARetiredBoardIsNeverRead(t *testing.T) {
	m := &memListings{}
	fake := newFake("greenhouse")
	s := scanFor(m, fake, seeds("acme"))
	if _, err := s.Run(context.Background(), ingestOwner); err != nil {
		t.Fatal(err)
	}
	boardOf(m, "acme").Status = domain.BoardRetired
	before := fake.read("acme")
	res, _ := s.Run(context.Background(), ingestOwner)
	if res.Boards != 0 || fake.read("acme") != before {
		t.Fatalf("retired: %+v", res)
	}
}

func TestARunStopsAtItsTimeBudgetAndSaysHowManyBoardsRemain(t *testing.T) {
	m := &memListings{}
	fake := newFake("greenhouse")
	var clock atomic.Int64 // seconds since ingestAt
	fake.onRead = func(string) { clock.Add(5) }
	names := []string{"aa", "bb", "cc", "dd", "ee"}
	s := scanFor(m, fake, seeds(names...))
	s.Now = func() time.Time { return ingestAt.Add(time.Duration(clock.Load()) * time.Second) }
	s.Budget = 20 * time.Second
	res, err := s.Run(context.Background(), ingestOwner)
	// reads start at 0s, 5s and 10s; at 15s fewer than 6s of the 20s remain, so no new board starts
	if err != nil || res.Read != 3 || res.Remaining != 2 || res.Boards != 5 {
		t.Fatalf("%v %+v", err, res)
	}
	if !strings.Contains(res.Summary(), "2 boards are left for the next run") {
		t.Fatalf("the summary says how many remain:\n%s", res.Summary())
	}
	res, _ = s.Run(context.Background(), ingestOwner)
	read := map[string]int{}
	for _, n := range names {
		read[n] = fake.read(n)
	}
	// the two that were not reached come first the next time, so every board is read before any is read twice
	if read["dd"] != 1 || read["ee"] != 1 || res.Read != 3 {
		t.Fatalf("the unread boards are taken first: %v %+v", read, res)
	}
}

func TestEveryReadCarriesTheRunsDeadlineSoTheHostClaimNeverOutlivesTheGateway(t *testing.T) {
	m := &memListings{}
	fake := newFake("greenhouse")
	fake.set("acme", okScan())
	fake.set("globex", okScan())
	s := scanFor(m, fake, seeds("acme", "globex"))
	if _, err := s.Run(context.Background(), ingestOwner); err != nil {
		t.Fatal(err)
	}
	if DefaultScanBudget != 20*time.Second || scanReserve != 6*time.Second {
		t.Fatalf("the run is budgeted at 20s with nothing new started in the last 6s: %s %s", DefaultScanBudget, scanReserve)
	}
	if fake.noDeadline != 0 || len(fake.deadlines) != 2 {
		t.Fatalf("every read has a deadline: %+v", fake)
	}
	for _, d := range fake.deadlines {
		if d <= 0 || d > DefaultScanBudget {
			t.Errorf("a read may take at most the budget, not %s", d)
		}
	}
}

func TestWhenTheBudgetIsSpentBeforeSeedingEndsTheRestAreSeededNextRun(t *testing.T) {
	m := &memListings{}
	fake := newFake("greenhouse")
	s := scanFor(m, fake, seeds("aa", "bb", "cc"))
	// the clock reads 15s after the start on every call after the first, so fewer than 6s of 20s remain
	calls := 0
	s.Now = func() time.Time {
		calls++
		if calls == 1 {
			return ingestAt
		}
		return ingestAt.Add(15 * time.Second)
	}
	res, err := s.Run(context.Background(), ingestOwner)
	if err != nil || res.Seeded != 0 || res.SeedsLeft != 3 || res.Read != 0 || len(m.boards) != 0 {
		t.Fatalf("nothing is started in the last 6s: %v %+v", err, res)
	}
	if !strings.Contains(res.Summary(), "3 more verified employers are added on the next run") {
		t.Fatalf("the summary says so:\n%s", res.Summary())
	}
	s.Now = func() time.Time { return ingestAt }
	res, _ = s.Run(context.Background(), ingestOwner)
	if res.Seeded != 3 || res.SeedsLeft != 0 || len(m.boards) != 3 {
		t.Fatalf("the next run finishes seeding: %+v", res)
	}
}

func TestABoardWhoseReadWasCutOffByTheRunIsNotCountedAsFailed(t *testing.T) {
	m := &memListings{}
	fake := newFake("greenhouse")
	ctx, cancel := context.WithCancel(context.Background())
	fake.onRead = func(string) { cancel() } // the run is stopped while the board is being read
	fake.set("acme", failScan("context canceled"))
	s := scanFor(m, fake, seeds("acme"))
	res, _ := s.Run(ctx, ingestOwner)
	b := boardOf(m, "acme")
	if res.Skipped != 1 || res.Failed != 0 || b.ConsecutiveFailures != 0 || len(m.scans) != 0 || res.Remaining != 1 {
		t.Fatalf("our own stop is not the board's failure: %+v board %+v scans %d", res, b, len(m.scans))
	}
}

func TestABoardWithoutAnAdapterFailsAndNamesIt(t *testing.T) {
	m := &memListings{}
	m.boards = append(m.boards, &domain.ATSBoard{ID: uuid.New(), Provider: "workday", TokenOrURL: "x.wd1/Site", Status: domain.BoardActive})
	s := scanFor(m, newFake("greenhouse"), nil)
	s.Seeds = []ats.Seed{}
	res, _ := s.Run(context.Background(), ingestOwner)
	if res.Failed != 1 || !strings.Contains(res.Summary(), "no adapter for workday") {
		t.Fatalf("%+v", res)
	}
}

// rewrite sends every request to the test server, keeping the path, so https://<company>/careers can be served locally.
type rewrite struct{ to *url.URL }

func (r rewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	c := req.Clone(req.Context())
	c.URL.Scheme, c.URL.Host = r.to.Scheme, r.to.Host
	c.Host = r.to.Host
	return http.DefaultTransport.RoundTrip(c)
}

func fetcherTo(srv *httptest.Server) *ats.Fetcher {
	u, _ := url.Parse(srv.URL)
	return &ats.Fetcher{Client: &http.Client{Transport: rewrite{u}}, MinInterval: 0, MaxRetries: 0}
}

func TestACandidateCompanyWithADomainHasItsCareersPagesTriedOnceAndItsBoardIsAdopted(t *testing.T) {
	var careers, jobsPage atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/careers":
			careers.Add(1)
			http.NotFound(w, r)
		case "/jobs":
			jobsPage.Add(1)
			w.Write([]byte(`<html><a href="https://boards.greenhouse.io/initech">Open roles</a><a href="https://boards.greenhouse.io/ghost">Old</a></html>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	m := &memListings{}
	// an aggregator named Initech and knew its domain
	lead := lead("Initech", "Software Engineer Intern", "https://internshala.com/internship/detail/9")
	lead.EmployerDomain = "initech.example"
	ingest(t, newIngester(m), scanOf("internshala", "/internships/software/", jobs.ScanOK, lead))
	if len(m.companies) != 1 || m.companies[0].Status != domain.CompanyCandidate {
		t.Fatalf("a company first seen on an aggregator is a candidate: %+v", m.companies)
	}

	fake := newFake("greenhouse")
	fake.set("initech", okScan(board("greenhouse", "initech", "7", "Platform Engineering Intern"), board("greenhouse", "initech", "8", "Staff Engineer")))
	fake.set("ghost", failScan("HTTP 404"))
	s := &OpportunityScan{Store: m, Adapters: map[string]ats.Adapter{"greenhouse": fake}, Fetcher: fetcherTo(srv), Seeds: []ats.Seed{}, Workers: 1, Now: func() time.Time { return ingestAt }}
	res, err := s.Run(context.Background(), ingestOwner)
	if err != nil {
		t.Fatal(err)
	}
	if res.Probed != 1 || len(res.Expanded) != 1 || !strings.Contains(res.Expanded[0], "greenhouse initech") {
		t.Fatalf("expanded: %+v", res)
	}
	if boardOf(m, "initech") == nil || boardOf(m, "ghost") != nil {
		t.Fatalf("only a board that read ok is stored; the token that 404s is never added: %+v", m.boards)
	}
	co := m.companies[0]
	if co.Status != domain.CompanyActive || co.EvidenceURL == nil || !strings.HasSuffix(*co.EvidenceURL, "/jobs") {
		t.Fatalf("the company became active and keeps the page as evidence: %+v", co)
	}
	var employer *domain.Listing
	for _, l := range m.rows {
		if l.Role == "Platform Engineering Intern" {
			employer = l
		}
	}
	if employer == nil || employer.Verification != domain.VerificationEmployer || employer.Company != "Initech" {
		t.Fatalf("the board's posting was ingested as employer-verified: %+v", m.rows)
	}
	var probes int
	for _, sc := range m.scans {
		if sc.Source == probeSource {
			probes++
		}
	}
	if probes != 2 {
		t.Fatalf("both careers pages are recorded in the scan history: %+v", m.scans)
	}

	// once is once: the next run does not ask the company's site again
	again, _ := s.Run(context.Background(), ingestOwner)
	if again.Probed != 0 || careers.Load() != 1 || jobsPage.Load() != 1 {
		t.Fatalf("tried once: %+v careers %d jobs %d", again, careers.Load(), jobsPage.Load())
	}
}

func TestACompanyWithNoDomainOrNoBoardOnItsPagesStaysACandidate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><h1>Careers</h1><p>Email us.</p></html>`))
	}))
	defer srv.Close()
	m := &memListings{}
	withDomain := lead("Hooli", "Software Engineer Intern", "https://internshala.com/internship/detail/1")
	withDomain.EmployerDomain = "hooli.example"
	noDomain := lead("Pied Piper", "Software Engineer Intern", "https://internshala.com/internship/detail/2")
	ingest(t, newIngester(m), scanOf("internshala", "/x/", jobs.ScanOK, withDomain, noDomain))
	s := &OpportunityScan{Store: m, Adapters: map[string]ats.Adapter{}, Fetcher: fetcherTo(srv), Seeds: []ats.Seed{}, Workers: 1, Now: func() time.Time { return ingestAt }}
	res, _ := s.Run(context.Background(), ingestOwner)
	if res.Probed != 1 || len(res.Expanded) != 0 || len(m.boards) != 0 {
		t.Fatalf("%+v", res)
	}
	for _, c := range m.companies {
		if c.Status != domain.CompanyCandidate {
			t.Errorf("no board, no posting from the employer: %s stays a candidate", c.Name)
		}
	}
}

func TestExpandingFromACareersPageStoresOnlyTheBoardsThatRead(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><a href="https://boards.greenhouse.io/wayne">x</a><a href="https://boards.greenhouse.io/nonesuch">y</a></html>`))
	}))
	defer srv.Close()
	m := &memListings{}
	fake := newFake("greenhouse")
	fake.set("wayne", okScan()) // a board with no openings is still a board that read ok
	fake.set("nonesuch", failScan("HTTP 404"))
	s := &OpportunityScan{Store: m, Adapters: map[string]ats.Adapter{"greenhouse": fake}, Fetcher: fetcherTo(srv), Seeds: []ats.Seed{}, Now: func() time.Time { return ingestAt }}
	r, err := s.ExpandFromCareersPage(context.Background(), ingestOwner, "Wayne Enterprises", "wayne.example", "https://wayne.example/careers")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Boards) != 1 || len(r.Discovery.Rejected) != 1 || r.Discovery.Rejected[0].Ref.Token != "nonesuch" {
		t.Fatalf("%+v", r)
	}
	if len(m.boards) != 1 || m.boards[0].TokenOrURL != "wayne" || m.boards[0].Status != domain.BoardActive || m.boards[0].LastOKAt == nil {
		t.Fatalf("the board that read ok is active and has its read on record: %+v", m.boards)
	}
	co := m.companies[0]
	if co.DiscoveredVia != "careers_page" || co.EvidenceURL == nil || *co.EvidenceURL != "https://wayne.example/careers" || co.Status != domain.CompanyCandidate {
		t.Fatalf("found via the careers page, with it as evidence, and a candidate until it lists a posting: %+v", co)
	}
	if _, err := s.ExpandFromCareersPage(context.Background(), ingestOwner, "", "", "https://x.example/careers"); err == nil {
		t.Fatal("a board needs a company name")
	}
}

func TestTheRoutineIsHandledInCodeForItsTitleOnly(t *testing.T) {
	m := &memListings{}
	fake := newFake("greenhouse")
	fake.set("acme", okScan(board("greenhouse", "acme", "1", "Software Engineer Intern")))
	w := &TrackerLists{Opportunities: scanFor(m, fake, seeds("acme"))}
	task := &domain.Task{ID: uuid.New(), UserID: ingestOwner, Title: "OPPORTUNITY SCAN (every 3 hours)"}
	answer, how := w.RunInCode(context.Background(), task, nil, nil)
	if how != Answered || !strings.Contains(answer, "Software Engineer Intern at Acme") {
		t.Fatalf("%v %s", how, answer)
	}
	if _, how := w.RunInCode(context.Background(), &domain.Task{UserID: ingestOwner, Title: "Something else"}, nil, nil); how != NotInCode {
		t.Fatal("other tasks are not this routine's")
	}
	if !IsOpportunityScan("opportunity scan") || IsOpportunityScan("LISTINGS SYNC") {
		t.Fatal("title match")
	}
	if !domain.IsRoutine(&domain.Task{Title: "OPPORTUNITY SCAN"}) {
		t.Fatal("the routine is never archived by age")
	}
	if _, ok := (&TrackerLists{}).ScanOpportunities(context.Background(), task); ok {
		t.Fatal("without a scan configured the routine is left alone")
	}
}

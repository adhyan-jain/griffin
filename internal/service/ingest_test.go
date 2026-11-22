package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/jobs"
)

var (
	ingestAt    = time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC)
	ingestOwner = uuid.MustParse("00000000-0000-0000-0000-0000000000aa")
)

func newIngester(m *memListings) *Ingester {
	return &Ingester{Store: m, Now: func() time.Time { return ingestAt }}
}

func lead(company, title, link string) jobs.RawJob {
	return jobs.RawJob{Source: "internshala", SourceKind: jobs.Aggregator, EmployerName: company, Title: title, URL: link, Locations: []string{"Pune, India"}, Compensation: "₹ 25,000 /month", FetchedAt: ingestAt}
}

func atsJob(company, title, link, id string) jobs.RawJob {
	return jobs.RawJob{Source: "greenhouse", SourceKind: jobs.EmployerATS, BoardToken: strings.ToLower(company), ExternalID: id, EmployerName: company, EmployerDomain: strings.ToLower(company) + ".com",
		Title: title, URL: link, Locations: []string{"Bengaluru", "Remote"}, Description: "Build APIs in Go.", Compensation: "₹ 60,000 /month", FetchedAt: ingestAt}
}

func scanOf(source, target string, status jobs.ScanStatus, js ...jobs.RawJob) jobs.Scan {
	return jobs.Scan{Source: source, Target: target, Status: status, Jobs: js, Pages: 1, StartedAt: ingestAt, FinishedAt: ingestAt.Add(time.Second)}
}

func ingest(t *testing.T, in *Ingester, s jobs.Scan) IngestResult {
	t.Helper()
	res, err := in.Ingest(context.Background(), ingestOwner, s)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	return res
}

func TestAnAggregatorPostingBecomesALeadWithACandidateCompany(t *testing.T) {
	m := &memListings{}
	res := ingest(t, newIngester(m), scanOf("internshala", "/internships/software/", jobs.ScanOK, lead("Acme Pvt Ltd", "Software Engineer Intern", "https://internshala.com/internship/detail/1?utm_source=x")))
	if len(res.Created) != 1 || len(m.rows) != 1 {
		t.Fatalf("one listing: %+v", res)
	}
	l := m.rows[0]
	if l.SourceKind != domain.SourceKindAggregator || l.Verification != domain.VerificationLead || l.EmployerURL != nil {
		t.Fatalf("an aggregator record is a lead and has no employer link: %+v", l)
	}
	if l.CanonicalURL == nil || *l.CanonicalURL != "https://internshala.com/internship/detail/1" || l.EmployerKey != "acme" || l.RoleKey != "software engineer intern" {
		t.Fatalf("identity: %+v", l)
	}
	if l.Stage != domain.StageVerified || l.SourceTarget != "/internships/software/" || l.Tier != "B" || l.Pay != "₹ 25,000 /month" || l.Location != "Pune, India" {
		t.Fatalf("fields: %+v", l)
	}
	if l.Deadline != nil || l.DescriptionHash != nil || l.PostedAt != nil {
		t.Fatalf("unknown stays unknown: %+v", l)
	}
	if len(m.companies) != 1 || m.companies[0].Status != domain.CompanyCandidate || m.companies[0].Domain != nil || m.companies[0].NormalizedName != "acme" || l.CompanyID == nil || *l.CompanyID != m.companies[0].ID {
		t.Fatalf("an aggregator only makes a candidate company: %+v", m.companies)
	}
	if len(m.boards) != 0 {
		t.Fatalf("an aggregator page is not an ATS board: %+v", m.boards)
	}
	if len(m.scans) != 1 || m.scans[0].Status != "ok" || m.scans[0].JobsSeen != 1 || m.scans[0].Source != "internshala" {
		t.Fatalf("the scan is recorded: %+v", m.scans)
	}
	if snaps := m.snapshotsOf(l.ID); len(snaps) != 1 || snaps[0].Title != "Software Engineer Intern" || snaps[0].CompensationText == nil {
		t.Fatalf("first sight leaves one snapshot: %+v", snaps)
	}
}

func TestIngestingTheSameScanTwiceChangesNothing(t *testing.T) {
	m := &memListings{}
	in := newIngester(m)
	s := scanOf("internshala", "/p/", jobs.ScanOK, lead("Acme", "Software Engineer Intern", "https://internshala.com/internship/detail/1"), lead("Beta", "Backend Intern", "https://internshala.com/internship/detail/2"))
	ingest(t, in, s)
	res := ingest(t, in, s)
	if len(res.Created) != 0 || res.Updated != 2 || res.Changed != 0 || len(m.rows) != 2 || len(m.companies) != 2 {
		t.Fatalf("a second pass adds nothing: %+v rows %d companies %d", res, len(m.rows), len(m.companies))
	}
	if len(m.snapshots) != 2 {
		t.Fatalf("no change, no snapshot: %d", len(m.snapshots))
	}
	if len(m.scans) != 2 {
		t.Fatalf("both scans are in the history: %d", len(m.scans))
	}
}

func TestEachChangeLeavesASnapshotAndAnUnknownNeverErasesAKnownValue(t *testing.T) {
	m := &memListings{}
	in := newIngester(m)
	base := atsJob("Acme", "Software Engineer Intern", "https://boards.greenhouse.io/acme/jobs/1", "1")
	ingest(t, in, scanOf("greenhouse", "acme", jobs.ScanOK, base))
	id := m.rows[0].ID

	deadline := ingestAt.Add(72 * time.Hour)
	steps := []struct {
		name   string
		mutate func(j *jobs.RawJob)
	}{
		{"description", func(j *jobs.RawJob) { j.Description = "Build APIs in Go and Rust." }},
		{"compensation", func(j *jobs.RawJob) { j.Compensation = "₹ 80,000 /month" }},
		{"deadline", func(j *jobs.RawJob) { j.Deadline = &deadline }},
		{"title", func(j *jobs.RawJob) { j.Title = "Software Engineer Intern, Platform" }},
		{"location", func(j *jobs.RawJob) { j.Locations = []string{"Hyderabad"} }},
	}
	cur := base
	for i, st := range steps {
		st.mutate(&cur)
		res := ingest(t, in, scanOf("greenhouse", "acme", jobs.ScanOK, cur))
		if res.Changed != 1 || len(m.snapshotsOf(id)) != i+2 {
			t.Fatalf("a %s change is one snapshot: %+v snapshots %d", st.name, res, len(m.snapshotsOf(id)))
		}
	}
	last := m.snapshotsOf(id)[len(steps)]
	if last.Title != "Software Engineer Intern, Platform" || last.Location != "Hyderabad" || last.CompensationText == nil || *last.CompensationText != "₹ 80,000 /month" || last.Deadline == nil || last.DescriptionHash == nil {
		t.Fatalf("a snapshot holds everything as it was then: %+v", last)
	}
	before := len(m.snapshotsOf(id))
	if res := ingest(t, in, scanOf("greenhouse", "acme", jobs.ScanOK, cur)); res.Changed != 0 || len(m.snapshotsOf(id)) != before {
		t.Fatalf("seeing it unchanged leaves no snapshot: %+v", res)
	}

	// The source stops giving pay, deadline and description (a summary-only read): those are unknown now, not gone.
	bare := cur
	bare.Compensation, bare.Deadline, bare.Description = "", nil, ""
	if res := ingest(t, in, scanOf("greenhouse", "acme", jobs.ScanOK, bare)); res.Changed != 0 {
		t.Fatalf("unknown is not a change: %+v", res)
	}
	got := m.rows[0]
	if got.CompensationText == nil || got.Deadline == nil || got.DescriptionHash == nil || got.Pay != "₹ 80,000 /month" {
		t.Fatalf("a known value is kept when the source says nothing: %+v", got)
	}
	if got.Description != "Build APIs in Go and Rust." {
		t.Fatalf("the stored description is kept: %q", got.Description)
	}
}

func TestOnlyAFetchedEmployerPostingIsVerifiedAndItActivatesTheCompany(t *testing.T) {
	m := &memListings{}
	in := newIngester(m)
	ingest(t, in, scanOf("greenhouse", "acme", jobs.ScanOK, atsJob("Acme", "Software Engineer Intern", "https://boards.greenhouse.io/acme/jobs/1?gh_src=li", "1")))
	l := m.rows[0]
	if l.Verification != domain.VerificationEmployer || l.SourceKind != domain.SourceKindEmployerATS || l.EmployerURL == nil || *l.EmployerURL != "https://boards.greenhouse.io/acme/jobs/1?gh_src=li" {
		t.Fatalf("an employer posting is verified: %+v", l)
	}
	if l.ExternalID != "1" || l.DescriptionHash == nil || l.Description != "Build APIs in Go." || l.SourceTarget != "acme" || l.Location != "Bengaluru; Remote" {
		t.Fatalf("fields: %+v", l)
	}
	if len(m.companies) != 1 || m.companies[0].Status != domain.CompanyActive || m.companies[0].Domain == nil || *m.companies[0].Domain != "acme.com" {
		t.Fatalf("an employer posting makes the company active: %+v", m.companies[0])
	}
	if len(m.boards) != 1 || m.boards[0].Provider != "greenhouse" || m.boards[0].TokenOrURL != "acme" || m.boards[0].CompanyID == nil || l.ATSBoardID == nil || *l.ATSBoardID != m.boards[0].ID {
		t.Fatalf("the board is recorded: %+v", m.boards)
	}

	// Without a title or without the link it was fetched from, nothing is stored and nothing is verified.
	noTitle := atsJob("Beta", "", "https://boards.greenhouse.io/beta/jobs/2", "2")
	noURL := atsJob("Gamma", "Backend Intern", "", "3")
	noEmployer := atsJob("", "Backend Intern", "https://boards.greenhouse.io/x/jobs/4", "4")
	res := ingest(t, in, scanOf("greenhouse", "beta", jobs.ScanOK, noTitle, noURL, noEmployer))
	if res.Skipped != 3 || len(m.rows) != 1 {
		t.Fatalf("records that cannot be identified are skipped: %+v rows %d", res, len(m.rows))
	}
	if verificationFor(domain.SourceKindEmployerATS, noTitle) != domain.VerificationLead || verificationFor(domain.SourceKindEmployerSite, noURL) != domain.VerificationLead || verificationFor(domain.SourceKindAggregator, atsJob("A", "T", "https://x", "1")) != domain.VerificationLead {
		t.Fatal("verification needs an employer kind, a title and the fetched link")
	}
}

func TestAnEmployerSiteWithoutABoardIsVerifiedToo(t *testing.T) {
	m := &memListings{}
	j := jobs.RawJob{Source: "careers_jsonld", SourceKind: jobs.EmployerSite, EmployerName: "Lab Co", Title: "Research Intern", URL: "https://labco.org/careers/7/", FetchedAt: ingestAt}
	ingest(t, newIngester(m), scanOf("careers_jsonld", "https://labco.org/careers", jobs.ScanOK, j))
	l := m.rows[0]
	if l.SourceKind != domain.SourceKindEmployerSite || l.Verification != domain.VerificationEmployer || l.Pay != "" || l.CompensationText != nil || l.Tier != "C" {
		t.Fatalf("%+v", l)
	}
	if len(m.boards) != 1 || m.boards[0].Provider != "careers_jsonld" || m.boards[0].TokenOrURL != "https://labco.org/careers" {
		t.Fatalf("the careers page is the board: %+v", m.boards)
	}
}

func TestTheEmployerPostingWinsAndKeepsTheApplicationStatus(t *testing.T) {
	m := &memListings{}
	in := newIngester(m)
	ingest(t, in, scanOf("internshala", "/p/", jobs.ScanOK, lead("Acme Technologies Pvt. Ltd.", "Software Engineering Internship", "https://internshala.com/internship/detail/9")))
	row := m.rows[0]
	row.Stage, row.StageNote = domain.StageDrafted, "form filled"
	taskID := uuid.New()
	row.TaskID = &taskID

	res := ingest(t, in, scanOf("greenhouse", "acme", jobs.ScanOK, atsJob("Acme", "Software Engineer Intern", "https://boards.greenhouse.io/acme/jobs/1", "1")))
	if len(m.rows) != 1 || len(res.Created) != 0 || res.Updated != 1 {
		t.Fatalf("the same employer and role from two sources is one listing: rows %d %+v", len(m.rows), res)
	}
	l := m.rows[0]
	if l.Source != "greenhouse" || l.URL != "https://boards.greenhouse.io/acme/jobs/1" || l.SourceKind != domain.SourceKindEmployerATS || l.Verification != domain.VerificationEmployer {
		t.Fatalf("the employer record becomes the primary: %+v", l)
	}
	if l.Company != "Acme" || l.EmployerURL == nil || l.SourceTarget != "acme" {
		t.Fatalf("%+v", l)
	}
	if l.Stage != domain.StageDrafted || l.StageNote != "form filled" || l.TaskID == nil || *l.TaskID != taskID {
		t.Fatalf("the application's progress is kept: %+v", l)
	}
	if len(m.evidence) != 1 || m.evidence[0].Claim != "lead_url" || m.evidence[0].Value != "https://internshala.com/internship/detail/9" || m.evidence[0].Quote != "Software Engineering Internship" {
		t.Fatalf("the lead's link is kept as evidence: %+v", m.evidence)
	}
	if len(m.companies) != 1 || m.companies[0].Status != domain.CompanyActive {
		t.Fatalf("one company, now active: %+v", m.companies)
	}

	// The aggregator showing it again changes nothing: it is merged, the employer URL stays primary.
	res = ingest(t, in, scanOf("internshala", "/p/", jobs.ScanOK, lead("Acme Technologies Pvt. Ltd.", "Software Engineering Internship", "https://internshala.com/internship/detail/9")))
	l = m.rows[0]
	if len(m.rows) != 1 || res.Merged != 1 || l.Source != "greenhouse" || l.Verification != domain.VerificationEmployer || l.URL != "https://boards.greenhouse.io/acme/jobs/1" {
		t.Fatalf("a lead never replaces the employer record: %+v %+v", res, l)
	}
}

func TestTheSameCanonicalURLIsTheSameListingEvenWithADifferentTitle(t *testing.T) {
	m := &memListings{}
	in := newIngester(m)
	ingest(t, in, scanOf("greenhouse", "acme", jobs.ScanOK, atsJob("Acme", "Platform Apprentice", "https://boards.greenhouse.io/acme/jobs/5", "5")))
	// A different source and external id, but the same posting link once tracking is dropped.
	other := atsJob("Acme Inc", "Platform Apprentice (Summer)", "https://Boards.Greenhouse.io/acme/jobs/5/?gh_src=abc#apply", "x")
	other.Source = "lever"
	res := ingest(t, in, scanOf("lever", "acme", jobs.ScanOK, other))
	if len(m.rows) != 1 || res.Merged != 1 {
		t.Fatalf("one posting seen by two ATS records: rows %d %+v", len(m.rows), res)
	}
}

func TestTheSourcesOwnIdMatchesWithinTheSourceOnly(t *testing.T) {
	m := &memListings{}
	in := newIngester(m)
	a := atsJob("Acme", "Role A", "https://boards.greenhouse.io/acme/jobs/100", "100")
	ingest(t, in, scanOf("greenhouse", "acme", jobs.ScanOK, a))
	// The same id, re-titled and re-linked by the employer: still the same posting at that source.
	moved := atsJob("Acme", "Role A (renamed)", "https://boards.greenhouse.io/acme/jobs/100-new", "100")
	res := ingest(t, in, scanOf("greenhouse", "acme", jobs.ScanOK, moved))
	if len(m.rows) != 1 || res.Changed != 1 || m.rows[0].Role != "Role A (renamed)" || m.rows[0].URL != "https://boards.greenhouse.io/acme/jobs/100-new" {
		t.Fatalf("the id follows the posting through a rename: rows %d %+v %+v", len(m.rows), res, m.rows[0])
	}
	other := atsJob("Other Co", "Different", "https://jobs.lever.co/other/100", "100")
	other.Source = "lever"
	ingest(t, in, scanOf("lever", "other", jobs.ScanOK, other))
	if len(m.rows) != 2 {
		t.Fatalf("an id from another source means nothing: %d", len(m.rows))
	}
}

func TestOnlyACompleteScanClosesAndOnlyWhatItsOwnTargetHeld(t *testing.T) {
	m := &memListings{}
	in := newIngester(m)
	acme1 := atsJob("Acme", "Backend Intern", "https://boards.greenhouse.io/acme/jobs/1", "1")
	acme2 := atsJob("Acme", "ML Intern", "https://boards.greenhouse.io/acme/jobs/2", "2")
	beta := atsJob("Beta", "Backend Intern", "https://boards.greenhouse.io/beta/jobs/9", "9")
	ingest(t, in, scanOf("greenhouse", "acme", jobs.ScanOK, acme1, acme2))
	ingest(t, in, scanOf("greenhouse", "beta", jobs.ScanOK, beta))
	find := func(role, company string) *domain.Listing {
		for _, r := range m.rows {
			if r.Role == role && r.Company == company {
				return r
			}
		}
		t.Fatalf("no listing %s at %s", role, company)
		return nil
	}

	// Failed: it read nothing, and nothing is closed. Not even with no jobs at all.
	if res := ingest(t, in, scanOf("greenhouse", "acme", jobs.ScanFailed)); res.Closed != 0 {
		t.Fatalf("a failed scan closes nothing: %+v", res)
	}
	// Partial: read one job, the other is not shown; that is not "gone".
	if res := ingest(t, in, scanOf("greenhouse", "acme", jobs.ScanPartial, acme1)); res.Closed != 0 || find("ML Intern", "Acme").ClosedAt != nil {
		t.Fatalf("a partial scan closes nothing: %+v", res)
	}
	// An unknown status is a failed scan.
	if res := ingest(t, in, scanOf("greenhouse", "acme", jobs.ScanStatus("weird"), acme1)); res.Closed != 0 || m.scans[len(m.scans)-1].Status != "failed" || !strings.Contains(m.scans[len(m.scans)-1].Error, "weird") {
		t.Fatalf("an unknown status is not a good scan: %+v %+v", res, m.scans[len(m.scans)-1])
	}
	if find("Backend Intern", "Acme").Stage == domain.StageClosed || find("ML Intern", "Acme").Stage == domain.StageClosed {
		t.Fatal("nothing closed so far")
	}

	// Complete: the ML role is gone from Acme's board, so it closes; Acme's backend role and Beta's role stay.
	res := ingest(t, in, scanOf("greenhouse", "acme", jobs.ScanOK, acme1))
	ml := find("ML Intern", "Acme")
	if res.Closed != 1 || ml.Stage != domain.StageClosed || ml.ClosedAt == nil || ml.StageNote == "" {
		t.Fatalf("the missing role closes: %+v %+v", res, ml)
	}
	if find("Backend Intern", "Acme").Stage != domain.StageVerified || find("Backend Intern", "Beta").Stage != domain.StageVerified {
		t.Fatal("only the listing missing from its own board closes")
	}
	// Running it again closes nothing more and does not touch the closed one.
	if res := ingest(t, in, scanOf("greenhouse", "acme", jobs.ScanOK, acme1)); res.Closed != 0 {
		t.Fatalf("closing is idempotent: %+v", res)
	}
}

func TestACompleteBoardWithNoOpeningsClosesItsListings(t *testing.T) {
	m := &memListings{}
	in := newIngester(m)
	ingest(t, in, scanOf("greenhouse", "acme", jobs.ScanOK, atsJob("Acme", "Backend Intern", "https://boards.greenhouse.io/acme/jobs/1", "1")))
	if res := ingest(t, in, scanOf("greenhouse", "acme", jobs.ScanOK)); res.Closed != 1 || m.rows[0].Stage != domain.StageClosed {
		t.Fatalf("a board that was read in full and lists nothing has nothing open: %+v", res)
	}
}

func TestARecordThatFailsToSaveBlocksClosingInThatScan(t *testing.T) {
	m := &memListings{}
	in := newIngester(m)
	ingest(t, in, scanOf("greenhouse", "acme", jobs.ScanOK, atsJob("Acme", "Backend Intern", "https://boards.greenhouse.io/acme/jobs/1", "1"), atsJob("Acme", "ML Intern", "https://boards.greenhouse.io/acme/jobs/2", "2")))
	m.failOn = "Acme"
	res := ingest(t, in, scanOf("greenhouse", "acme", jobs.ScanOK, atsJob("Acme", "Backend Intern", "https://boards.greenhouse.io/acme/jobs/1", "1")))
	if res.Closed != 0 || len(res.Problems) == 0 || m.rows[0].Stage == domain.StageClosed || m.rows[1].Stage == domain.StageClosed {
		t.Fatalf("a listing we failed to look at is not a listing that is gone: %+v", res)
	}
}

func TestAnApplicationAlreadyConfirmedIsNotClosedButTheGonePostingIsRecorded(t *testing.T) {
	m := &memListings{}
	in := newIngester(m)
	ingest(t, in, scanOf("greenhouse", "acme", jobs.ScanOK, atsJob("Acme", "Backend Intern", "https://boards.greenhouse.io/acme/jobs/1", "1")))
	m.rows[0].Stage = domain.StageConfirmed
	if res := ingest(t, in, scanOf("greenhouse", "acme", jobs.ScanOK)); res.Closed != 1 {
		t.Fatalf("%+v", res)
	}
	if m.rows[0].Stage != domain.StageConfirmed || m.rows[0].ClosedAt == nil {
		t.Fatalf("a submitted application keeps its stage, and the posting is marked gone: %+v", m.rows[0])
	}
	// And it comes back: the posting reopens without touching the stage.
	ingest(t, in, scanOf("greenhouse", "acme", jobs.ScanOK, atsJob("Acme", "Backend Intern", "https://boards.greenhouse.io/acme/jobs/1", "1")))
	if m.rows[0].Stage != domain.StageConfirmed || m.rows[0].ClosedAt != nil || m.rows[0].ReopenedCount != 1 {
		t.Fatalf("%+v", m.rows[0])
	}
}

func TestAClosedListingSeenAgainReopensAndIsCounted(t *testing.T) {
	m := &memListings{}
	in := newIngester(m)
	j := atsJob("Acme", "Backend Intern", "https://boards.greenhouse.io/acme/jobs/1", "1")
	ingest(t, in, scanOf("greenhouse", "acme", jobs.ScanOK, j))
	ingest(t, in, scanOf("greenhouse", "acme", jobs.ScanOK))
	if m.rows[0].Stage != domain.StageClosed {
		t.Fatal("closed")
	}
	res := ingest(t, in, scanOf("greenhouse", "acme", jobs.ScanOK, j))
	l := m.rows[0]
	if res.Reopened != 1 || l.Stage != domain.StageVerified || l.ClosedAt != nil || l.ReopenedCount != 1 || len(m.rows) != 1 {
		t.Fatalf("the posting is back: %+v %+v", res, l)
	}
	ingest(t, in, scanOf("greenhouse", "acme", jobs.ScanOK))
	ingest(t, in, scanOf("greenhouse", "acme", jobs.ScanOK, j))
	if m.rows[0].ReopenedCount != 2 {
		t.Fatalf("each reopening is counted: %d", m.rows[0].ReopenedCount)
	}
}

func TestAListingTheOwnerClosedStaysClosedAndALeadNeverReopensAnEmployerListing(t *testing.T) {
	m := &memListings{}
	in := newIngester(m)
	j := atsJob("Acme", "Backend Intern", "https://boards.greenhouse.io/acme/jobs/1", "1")
	ingest(t, in, scanOf("greenhouse", "acme", jobs.ScanOK, j))
	if err := m.Move(context.Background(), ingestOwner, m.rows[0].ID, domain.StageVerified, domain.StageClosed, domain.ClosedByOwnerNote, nil); err != nil {
		t.Fatal(err)
	}
	if res := ingest(t, in, scanOf("greenhouse", "acme", jobs.ScanOK, j)); res.Reopened != 0 || m.rows[0].Stage != domain.StageClosed {
		t.Fatalf("the ingestOwner dropped it; the posting did not go away: %+v", res)
	}

	m2 := &memListings{}
	in2 := newIngester(m2)
	ingest(t, in2, scanOf("greenhouse", "acme", jobs.ScanOK, j))
	ingest(t, in2, scanOf("greenhouse", "acme", jobs.ScanOK)) // the employer took it down
	res := ingest(t, in2, scanOf("internshala", "/p/", jobs.ScanOK, lead("Acme", "Backend Intern", "https://internshala.com/internship/detail/3")))
	if res.Reopened != 0 || m2.rows[0].Stage != domain.StageClosed || res.Merged != 1 {
		t.Fatalf("a lead on a job board does not say the employer reopened it: %+v", res)
	}
}

func TestAnAggregatorPageIsAWindowSoItsMissesCloseNothingUnlessAskedTo(t *testing.T) {
	m := &memListings{}
	in := newIngester(m)
	ingest(t, in, scanOf("internshala", "/p/", jobs.ScanOK, lead("Acme", "Backend Intern", "https://internshala.com/internship/detail/1"), lead("Beta", "ML Intern", "https://internshala.com/internship/detail/2")))
	res := ingest(t, in, scanOf("internshala", "/p/", jobs.ScanOK, lead("Acme", "Backend Intern", "https://internshala.com/internship/detail/1")))
	if res.Closed != 0 || m.rows[1].Stage == domain.StageClosed {
		t.Fatalf("a listing missing from a search page may only have moved off it: %+v", res)
	}
	in.CloseAggregatorMisses = true
	res = ingest(t, in, scanOf("internshala", "/p/", jobs.ScanOK, lead("Acme", "Backend Intern", "https://internshala.com/internship/detail/1")))
	if res.Closed != 1 || m.rows[1].Stage != domain.StageClosed || m.rows[0].Stage == domain.StageClosed {
		t.Fatalf("when enabled, only the missing one closes: %+v", res)
	}
}

func TestOldRowsWithoutIdentityKeysAreMatchedAfterABackfill(t *testing.T) {
	old := &domain.Listing{ID: uuid.New(), Source: "unstop", Company: "Acme Pvt Ltd", Role: "SDE Intern", URL: "https://unstop.com/internships/acme-1", Stage: domain.StageDrafted, SourceKind: domain.SourceKindAggregator, Verification: domain.VerificationLead}
	m := &memListings{rows: []*domain.Listing{old}}
	ingest(t, newIngester(m), scanOf("unstop", "software", jobs.ScanOK, jobs.RawJob{Source: "unstop", SourceKind: jobs.Aggregator, EmployerName: "Acme", Title: "SDE Intern", URL: "https://unstop.com/internships/acme-1?utm_source=a", FetchedAt: ingestAt}))
	if len(m.rows) != 1 || m.rows[0].Stage != domain.StageDrafted || m.rows[0].EmployerKey != "acme" || m.rows[0].SourceTarget != "software" {
		t.Fatalf("an existing listing is found, not duplicated, and keeps its stage: %+v", m.rows)
	}
}

func TestBoardsBreakAfterRepeatedFailuresAndRecoverOnAGoodScan(t *testing.T) {
	m := &memListings{}
	in := newIngester(m)
	ingest(t, in, scanOf("greenhouse", "acme", jobs.ScanOK, atsJob("Acme", "Backend Intern", "https://boards.greenhouse.io/acme/jobs/1", "1")))
	b := m.boards[0]
	for i := 0; i < domain.BoardBrokenAfter-1; i++ {
		ingest(t, in, scanOf("greenhouse", "acme", jobs.ScanFailed))
	}
	if b.Status != domain.BoardActive || b.ConsecutiveFailures != domain.BoardBrokenAfter-1 {
		t.Fatalf("one bad night is not a dead board: %+v", b)
	}
	ingest(t, in, scanOf("greenhouse", "acme", jobs.ScanPartial))
	if b.ConsecutiveFailures != domain.BoardBrokenAfter-1 {
		t.Fatalf("a partial scan is not a failure: %+v", b)
	}
	ingest(t, in, scanOf("greenhouse", "acme", jobs.ScanFailed))
	if b.Status != domain.BoardBroken || m.rows[0].Stage == domain.StageClosed {
		t.Fatalf("broken after repeated failures, and still nothing closed: %+v", b)
	}
	ingest(t, in, scanOf("greenhouse", "acme", jobs.ScanOK, atsJob("Acme", "Backend Intern", "https://boards.greenhouse.io/acme/jobs/1", "1")))
	if b.Status != domain.BoardActive || b.ConsecutiveFailures != 0 || b.LastOKAt == nil {
		t.Fatalf("a good scan recovers it: %+v", b)
	}
}

func TestEveryScanIsRecordedWithItsStatusAndError(t *testing.T) {
	m := &memListings{}
	in := newIngester(m)
	failed := scanOf("greenhouse", "acme", jobs.ScanFailed)
	failed.Error, failed.Pages = "HTTP 503", 0
	res := ingest(t, in, failed)
	if len(m.rows) != 0 || len(m.companies) != 0 || res.ScanState.Status != "failed" {
		t.Fatalf("a failed scan stores no listings: %+v", res)
	}
	partial := scanOf("greenhouse", "acme", jobs.ScanPartial, atsJob("Acme", "Backend Intern", "https://boards.greenhouse.io/acme/jobs/1", "1"))
	partial.Error, partial.Pages = "page 2 timed out", 1
	ingest(t, in, partial)
	if len(m.scans) != 2 || m.scans[0].Status != "failed" || m.scans[0].Error != "HTTP 503" || m.scans[1].Status != "partial" || m.scans[1].JobsSeen != 1 || m.scans[1].Error != "page 2 timed out" {
		t.Fatalf("%+v %+v", m.scans[0], m.scans[1])
	}
	if len(m.rows) != 1 {
		t.Fatalf("a partial scan still adds what it read: %d", len(m.rows))
	}
}

func TestTheSeenTimeIsWhenTheAdapterFetchedItNotWhenWeIngestedIt(t *testing.T) {
	m := &memListings{}
	j := atsJob("Acme", "Backend Intern", "https://boards.greenhouse.io/acme/jobs/1", "1")
	fetched := ingestAt.Add(-3 * time.Hour)
	j.FetchedAt = fetched
	ingest(t, newIngester(m), scanOf("greenhouse", "acme", jobs.ScanOK, j))
	if !m.rows[0].FirstSeenAt.Equal(fetched) || m.rows[0].LastSeenAt == nil || !m.rows[0].LastSeenAt.Equal(fetched) {
		t.Fatalf("%+v", m.rows[0])
	}
}

func TestIngestWithoutAStoreIsAnError(t *testing.T) {
	var in *Ingester
	if _, err := in.Ingest(context.Background(), ingestOwner, jobs.Scan{}); err == nil {
		t.Fatal("no store, no ingest")
	}
}

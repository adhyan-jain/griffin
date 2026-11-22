package ats

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/griffin/core/internal/jobs"
)

func ctx() context.Context { return context.Background() }

func mustOK(t *testing.T, s jobs.Scan, wantJobs int) {
	t.Helper()
	if s.Status != jobs.ScanOK || s.Error != "" || !s.Complete() {
		t.Fatalf("want an ok scan, got %s: %s", s.Status, s.Error)
	}
	if len(s.Jobs) != wantJobs {
		t.Fatalf("want %d jobs, got %d", wantJobs, len(s.Jobs))
	}
	if s.StartedAt.IsZero() || s.FinishedAt.Before(s.StartedAt) {
		t.Fatalf("scan times: %v %v", s.StartedAt, s.FinishedAt)
	}
	for _, j := range s.Jobs {
		if j.Source != s.Source || j.FetchedAt.IsZero() || j.Title == "" || j.URL == "" {
			t.Fatalf("incomplete job: %+v", j)
		}
	}
}

func eq(t *testing.T, what string, got, want any) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("%s: got %v, want %v", what, got, want)
	}
}

func atTime(t *testing.T, what string, got *time.Time, want string) {
	t.Helper()
	if got == nil {
		t.Errorf("%s: got nil, want %s", what, want)
		return
	}
	eq(t, what, got.Format(time.RFC3339Nano), want)
}

// ---- Greenhouse (fixture: real capture of boards-api.greenhouse.io/v1/boards/stripe, trimmed to 3 jobs) -------

func TestGreenhouseReadsTheRealBoard(t *testing.T) {
	fw, f := newFakeWeb(t, func(w http.ResponseWriter, r *http.Request, c call) { serve(w, fixture(t, "greenhouse.json")) })
	s := Greenhouse{F: f}.Read(ctx(), "stripe")
	mustOK(t, s, 3)
	if c := fw.Calls[0]; c.Host != "boards-api.greenhouse.io" || c.Path != "/v1/boards/stripe/jobs" || c.Query != "content=true" {
		t.Fatalf("%+v", c)
	}
	j := s.Jobs[1]
	eq(t, "id", j.ExternalID, "8172510")
	eq(t, "title", j.Title, "Abuse Investigator")
	eq(t, "url", j.URL, "https://stripe.com/jobs/search?gh_jid=8172510")
	eq(t, "kind", j.SourceKind, jobs.EmployerATS)
	eq(t, "token", j.BoardToken, "stripe")
	eq(t, "employer", j.EmployerName, "Stripe")
	eq(t, "locations", j.Locations, "[Seattle, San Francisco, New York City]")
	eq(t, "department", j.Department, "8611 Security Analytics")
	atTime(t, "updated", j.UpdatedAt, "2026-11-19T17:12:24Z")
	atTime(t, "posted", j.PostedAt, "2026-09-09T14:50:29Z")
	if j.Deadline != nil || j.Remote != nil || j.EmploymentType != "" || j.Compensation != "" || j.ApplyURL != "" {
		t.Errorf("fields the board does not give stay empty: %+v", j)
	}
	if !strings.HasPrefix(j.Description, "Who we are") || !strings.Contains(j.Description, "Stripe is a financial infrastructure platform") ||
		strings.ContainsAny(j.Description, "<>") || strings.Contains(j.Description, "&lt;") || strings.Contains(j.Description, "&amp;") {
		t.Errorf("description is not plain text: %q", j.Description[:120])
	}
}

// ---- Lever (fixture: real capture of api.lever.co/v0/postings/palantir?limit=2, trimmed) -----------------------

func TestLeverReadsTheRealBoard(t *testing.T) {
	fw, f := newFakeWeb(t, func(w http.ResponseWriter, r *http.Request, c call) { serve(w, fixture(t, "lever.json")) })
	s := Lever{F: f}.Read(ctx(), "palantir")
	mustOK(t, s, 2)
	if c := fw.Calls[0]; c.Host != "api.lever.co" || c.Path != "/v0/postings/palantir" || !strings.Contains(c.Query, "mode=json") {
		t.Fatalf("%+v", c)
	}
	j := s.Jobs[0]
	eq(t, "id", j.ExternalID, "6ed76ce8-4156-4b60-b120-403538bd66cd")
	eq(t, "title", j.Title, "Administrative Business Partner")
	eq(t, "employment", j.EmploymentType, "Full-time")
	eq(t, "department", j.Department, "Administrative")
	eq(t, "locations", j.Locations, "[Singapore, Singapore]")
	if j.Remote == nil || *j.Remote {
		t.Errorf("hybrid is not remote: %v", j.Remote)
	}
	atTime(t, "posted", j.PostedAt, time.UnixMilli(1786469891368).UTC().Format(time.RFC3339Nano))
	if !strings.HasPrefix(j.URL, "https://jobs.lever.co/palantir/") || !strings.HasSuffix(j.ApplyURL, "/apply") {
		t.Errorf("urls: %q %q", j.URL, j.ApplyURL)
	}
	for _, want := range []string{"A World-Changing Company", "What We Value", "- Ability to adjust quickly"} {
		if !strings.Contains(j.Description, want) {
			t.Errorf("description lacks %q", want)
		}
	}
	if strings.ContainsAny(j.Description, "<>") || strings.Contains(j.Description, " ") {
		t.Errorf("description not clean")
	}
}

func TestLeverEUUsesTheEUHostAndProvider(t *testing.T) {
	// hand-built: the EU API has the same format; the capture of the US board is served from the EU host
	fw, f := newFakeWeb(t, func(w http.ResponseWriter, r *http.Request, c call) { serve(w, fixture(t, "lever.json")) })
	a := Lever{F: f, EU: true}
	eq(t, "provider", a.Provider(), "lever_eu")
	s := a.Read(ctx(), "mistral")
	mustOK(t, s, 2)
	eq(t, "host", fw.Calls[0].Host, "api.eu.lever.co")
	eq(t, "job source", s.Jobs[0].Source, "lever_eu")
}

func TestLeverWorkplaceSalaryAndSparseFields(t *testing.T) {
	_, f := newFakeWeb(t, func(w http.ResponseWriter, r *http.Request, c call) {
		serve(w, []byte(`[{"id":"a1","text":"Intern","hostedUrl":"https://jobs.lever.co/x/a1","workplaceType":"remote",
		"categories":{"location":"Remote"},"descriptionPlain":"Hi","salaryRange":{"currency":"USD","interval":"per-month-salary","min":3000,"max":4000}},
		{"id":"a2","text":"Other","hostedUrl":"https://jobs.lever.co/x/a2","workplaceType":"unspecified","categories":{},"salaryDescriptionPlain":"$45/hr"}]`))
	})
	s := Lever{F: f}.Read(ctx(), "x")
	mustOK(t, s, 2)
	if r := s.Jobs[0].Remote; r == nil || !*r {
		t.Error("workplaceType remote")
	}
	eq(t, "range", s.Jobs[0].Compensation, "USD 3000 - 4000 per-month-salary")
	eq(t, "locations", s.Jobs[0].Locations, "[Remote]")
	if s.Jobs[1].Remote != nil || s.Jobs[1].PostedAt != nil || len(s.Jobs[1].Locations) != 0 || s.Jobs[1].Description != "" {
		t.Errorf("unknowns stay empty: %+v", s.Jobs[1])
	}
	eq(t, "verbatim pay", s.Jobs[1].Compensation, "$45/hr")
}

func leverPages(total int) func(w http.ResponseWriter, r *http.Request, c call) {
	return func(w http.ResponseWriter, r *http.Request, c call) {
		skip, _ := strconv.Atoi(r.URL.Query().Get("skip"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		var out []map[string]string
		for i := skip; i < skip+limit && i < total; i++ {
			out = append(out, map[string]string{"id": fmt.Sprint("id", i), "text": "Job " + fmt.Sprint(i), "hostedUrl": "https://jobs.lever.co/x/" + fmt.Sprint(i)})
		}
		b, _ := json.Marshal(out)
		serve(w, b)
	}
}

func TestLeverPaginatesUntilAShortPage(t *testing.T) {
	fw, f := newFakeWeb(t, leverPages(250))
	s := Lever{F: f}.Read(ctx(), "x")
	mustOK(t, s, 250)
	eq(t, "pages", s.Pages, 3)
	eq(t, "requests", len(fw.Calls), 3)
}

func TestLeverPartialWhenALaterPageFails(t *testing.T) {
	inner := leverPages(250)
	_, f := newFakeWeb(t, func(w http.ResponseWriter, r *http.Request, c call) {
		if r.URL.Query().Get("skip") == "100" {
			fail(w, 500)
			return
		}
		inner(w, r, c)
	})
	s := Lever{F: f}.Read(ctx(), "x")
	if s.Status != jobs.ScanPartial || s.Complete() || len(s.Jobs) != 100 || s.Error == "" || s.Pages != 1 {
		t.Fatalf("want partial with the first 100: %s %d %q", s.Status, len(s.Jobs), s.Error)
	}
}

func TestLeverFailedWhenTheFirstPageFails(t *testing.T) {
	_, f := newFakeWeb(t, func(w http.ResponseWriter, r *http.Request, c call) { fail(w, 500) })
	s := Lever{F: f}.Read(ctx(), "x")
	if s.Status != jobs.ScanFailed || len(s.Jobs) != 0 || s.Error == "" {
		t.Fatalf("%+v", s)
	}
}

func TestLeverStopsWhenTheServerIgnoresSkip(t *testing.T) {
	_, f := newFakeWeb(t, func(w http.ResponseWriter, r *http.Request, c call) {
		var out []map[string]string
		for i := 0; i < 100; i++ {
			out = append(out, map[string]string{"id": fmt.Sprint(i), "text": "J", "hostedUrl": "https://x/" + fmt.Sprint(i)})
		}
		b, _ := json.Marshal(out)
		serve(w, b)
	})
	s := Lever{F: f}.Read(ctx(), "x")
	mustOK(t, s, 100)
}

// ---- Ashby (fixture: real capture of api.ashbyhq.com/posting-api/job-board/ramp, trimmed to 2 jobs) ------------

func TestAshbyReadsTheRealBoard(t *testing.T) {
	fw, f := newFakeWeb(t, func(w http.ResponseWriter, r *http.Request, c call) { serve(w, fixture(t, "ashby.json")) })
	s := Ashby{F: f}.Read(ctx(), "ramp")
	mustOK(t, s, 2)
	if c := fw.Calls[0]; c.Host != "api.ashbyhq.com" || c.Path != "/posting-api/job-board/ramp" || c.Query != "includeCompensation=true" {
		t.Fatalf("%+v", c)
	}
	j := s.Jobs[0]
	eq(t, "title", j.Title, "Software Engineer, Security, Cloud")
	eq(t, "compensation verbatim", j.Compensation, "$211.4K – $290.6K • Offers Equity")
	eq(t, "employment", j.EmploymentType, "FullTime")
	eq(t, "department", j.Department, "Engineering")
	if j.Remote == nil || !*j.Remote {
		t.Errorf("isRemote true is kept: %v", j.Remote)
	}
	eq(t, "locations", j.Locations, "[New York, NY (HQ) Remote (Canada) Remote (US) Miami, FL]")
	atTime(t, "posted", j.PostedAt, "2026-04-07T17:12:35.753Z")
	eq(t, "url", j.URL, "https://jobs.ashbyhq.com/ramp/34413f8d-26bf-4bbc-8ade-eb309a0e2245")
	eq(t, "apply", j.ApplyURL, "https://jobs.ashbyhq.com/ramp/34413f8d-26bf-4bbc-8ade-eb309a0e2245/application")
	if !strings.HasPrefix(j.Description, "ABOUT RAMP") || strings.Contains(j.Description, "<") {
		t.Errorf("description: %q", j.Description[:60])
	}
	if o := s.Jobs[1]; o.Remote == nil || *o.Remote {
		t.Errorf("isRemote false is kept, not turned into unknown: %v", o.Remote)
	}
}

func TestAshbySkipsUnlistedAndFallsBackToHTML(t *testing.T) {
	_, f := newFakeWeb(t, func(w http.ResponseWriter, r *http.Request, c call) {
		serve(w, []byte(`{"jobs":[{"id":"1","title":"Hidden","jobUrl":"https://j/1","isListed":false},
		{"id":"2","title":"Shown","jobUrl":"https://j/2","descriptionHtml":"<p>One</p><ul><li>a</li><li>b</li></ul>","compensation":{"scrapeableCompensationSalarySummary":"$10 - $20"}}]}`))
	})
	s := Ashby{F: f}.Read(ctx(), "x")
	mustOK(t, s, 1)
	eq(t, "description", s.Jobs[0].Description, "One\n\n- a\n- b")
	eq(t, "comp", s.Jobs[0].Compensation, "$10 - $20")
	if s.Jobs[0].Remote != nil {
		t.Error("isRemote absent stays nil")
	}
}

// ---- SmartRecruiters (fixtures: real capture of api.smartrecruiters.com/v1/companies/smartrecruiters) -------------

func srServer(t *testing.T) func(w http.ResponseWriter, r *http.Request, c call) {
	return func(w http.ResponseWriter, r *http.Request, c call) {
		if strings.HasSuffix(r.URL.Path, "/postings") {
			serve(w, fixture(t, "smartrecruiters_list.json"))
			return
		}
		serve(w, fixture(t, "smartrecruiters_detail.json"))
	}
}

func TestSmartRecruitersReadsListAndDetail(t *testing.T) {
	fw, f := newFakeWeb(t, srServer(t))
	s := SmartRecruiters{F: f}.Read(ctx(), "smartrecruiters")
	mustOK(t, s, 1)
	eq(t, "requests", len(fw.Calls), 2)
	j := s.Jobs[0]
	eq(t, "id", j.ExternalID, "744000158320111")
	eq(t, "employer", j.EmployerName, "SmartRecruiters Inc")
	eq(t, "url from detail", j.URL, "https://jobs.smartrecruiters.com/smartrecruiters/744000158320111-senior-lead-engineering-operations-manager")
	eq(t, "apply", j.ApplyURL, "https://jobs.smartrecruiters.com/smartrecruiters/744000158320111-senior-lead-engineering-operations-manager?oga=true")
	eq(t, "employment", j.EmploymentType, "Full-time")
	eq(t, "department", j.Department, "Engineering")
	eq(t, "locations", j.Locations, "[Poland, Remote, Poland]")
	if j.Remote == nil || !*j.Remote {
		t.Errorf("remote: %v", j.Remote)
	}
	atTime(t, "posted", j.PostedAt, "2026-10-28T15:14:43.941Z")
	if !strings.Contains(j.Description, "Company Description") || !strings.Contains(j.Description, "Job Description") || strings.Contains(j.Description, "<") {
		t.Errorf("description: %q", j.Description)
	}
}

func srList(total int) func(w http.ResponseWriter, r *http.Request, c call) {
	return func(w http.ResponseWriter, r *http.Request, c call) {
		if strings.HasSuffix(r.URL.Path, "/postings") {
			off, _ := strconv.Atoi(r.URL.Query().Get("offset"))
			lim, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			var items []map[string]any
			for i := off; i < off+lim && i < total; i++ {
				items = append(items, map[string]any{"id": fmt.Sprint(1000 + i), "name": "Role " + fmt.Sprint(i), "company": map[string]string{"identifier": "acme", "name": "Acme"}})
			}
			b, _ := json.Marshal(map[string]any{"offset": off, "limit": lim, "totalFound": total, "content": items})
			serve(w, b)
			return
		}
		serve(w, []byte(`{"postingUrl":"https://jobs.smartrecruiters.com/acme/x","jobAd":{"sections":{"jobDescription":{"title":"Job Description","text":"<p>Do things</p>"}}}}`))
	}
}

func TestSmartRecruitersPaginatesAndCapsDetailCalls(t *testing.T) {
	fw, f := newFakeWeb(t, srList(230))
	s := SmartRecruiters{F: f, MaxDetail: 5}.Read(ctx(), "acme")
	mustOK(t, s, 230)
	list := 0
	for _, c := range fw.Calls {
		if strings.HasSuffix(c.Path, "/postings") {
			list++
		}
	}
	eq(t, "list pages", list, 3)
	eq(t, "detail calls", len(fw.Calls)-list, 5)
	if s.Jobs[4].Description != "Job Description\n\nDo things" || s.Jobs[5].Description != "" {
		t.Errorf("only the first five have a description: %q / %q", s.Jobs[4].Description, s.Jobs[5].Description)
	}
	if !strings.HasPrefix(s.Jobs[5].URL, "https://jobs.smartrecruiters.com/acme/1005") {
		t.Errorf("fallback url: %q", s.Jobs[5].URL)
	}
}

func TestSmartRecruitersPartialWhenADetailFails(t *testing.T) {
	inner := srList(3)
	_, f := newFakeWeb(t, func(w http.ResponseWriter, r *http.Request, c call) {
		if strings.HasSuffix(r.URL.Path, "/1001") {
			fail(w, 500)
			return
		}
		inner(w, r, c)
	})
	s := SmartRecruiters{F: f}.Read(ctx(), "acme")
	if s.Status != jobs.ScanPartial || len(s.Jobs) != 3 || !strings.Contains(s.Error, "1001") {
		t.Fatalf("%s %d %q", s.Status, len(s.Jobs), s.Error)
	}
}

func TestSmartRecruitersPartialWhenListPageTwoFails(t *testing.T) {
	inner := srList(230)
	_, f := newFakeWeb(t, func(w http.ResponseWriter, r *http.Request, c call) {
		if r.URL.Query().Get("offset") == "100" {
			fail(w, 502)
			return
		}
		inner(w, r, c)
	})
	s := SmartRecruiters{F: f, MaxDetail: 1}.Read(ctx(), "acme")
	if s.Status != jobs.ScanPartial || len(s.Jobs) != 100 {
		t.Fatalf("%s %d %q", s.Status, len(s.Jobs), s.Error)
	}
}

// ---- Workday (fixtures: real capture of nvidia.wd5.myworkdayjobs.com, NVIDIAExternalCareerSite, searchText=intern)

func TestWorkdayReadsListAndDetail(t *testing.T) {
	fw, f := newFakeWeb(t, func(w http.ResponseWriter, r *http.Request, c call) {
		if r.Method == "POST" {
			serve(w, fixture(t, "workday_list.json"))
			return
		}
		serve(w, fixture(t, "workday_detail.json"))
	})
	f.Now = func() time.Time { return time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC) }
	s := Workday{F: f, MaxDetail: 1}.Read(ctx(), "nvidia.wd5/NVIDIAExternalCareerSite")
	mustOK(t, s, 3)
	first := fw.Calls[0]
	if first.Host != "nvidia.wd5.myworkdayjobs.com" || first.Path != "/wday/cxs/nvidia/NVIDIAExternalCareerSite/jobs" || first.Method != "POST" {
		t.Fatalf("%+v", first)
	}
	var sent map[string]any
	json.Unmarshal([]byte(first.Body), &sent)
	eq(t, "searchText", sent["searchText"], "intern")
	eq(t, "limit", sent["limit"], 20)
	eq(t, "offset", sent["offset"], 0)
	eq(t, "detail path", fw.Calls[1].Path, "/wday/cxs/nvidia/NVIDIAExternalCareerSite/job/China-Shanghai/DFT-Intern---2027_JR2027176")

	j := s.Jobs[0] // detail served for the first posting only (MaxDetail 1)
	eq(t, "title", j.Title, "DFT Intern - 2027")
	eq(t, "id", j.ExternalID, "JR2029779") // the detail fixture is a different posting; its req id wins
	eq(t, "employment", j.EmploymentType, "Full time")
	eq(t, "url from detail", j.URL, "https://nvidia.wd5.myworkdayjobs.com/NVIDIAExternalCareerSite/job/Switzerland-Zurich/GPU-Compiler-Intern_JR2029779")
	if !strings.HasPrefix(j.Description, "As GPU Compiler Intern") || strings.Contains(j.Description, "<") {
		t.Errorf("description: %q", j.Description)
	}
	// the second posting got no detail: list data only, relative date computed from the fetch time
	l := s.Jobs[1]
	eq(t, "no description", l.Description, "")
	eq(t, "list url", l.URL, "https://nvidia.wd5.myworkdayjobs.com/NVIDIAExternalCareerSite/job/Switzerland-Zurich/GPU-Compiler-Intern_JR2029779")
	eq(t, "list locations", l.Locations, "[Switzerland, Zurich]")
	eq(t, "req id from bulletFields", l.ExternalID, "JR2029779")
	atTime(t, "Posted 11 Days Ago", l.PostedAt, "2026-11-11T00:00:00Z")
	atTime(t, "detail startDate", s.Jobs[0].PostedAt, "2026-11-11T00:00:00Z")
	if s.Jobs[2].PostedAt == nil || s.Jobs[2].PostedAt.Format("2006-01-02") != "2026-11-19" {
		t.Errorf("Posted 3 Days Ago: %v", s.Jobs[2].PostedAt)
	}
}

func TestWorkdayPaginatesWithTotalOnlyOnTheFirstPage(t *testing.T) {
	fw, f := newFakeWeb(t, func(w http.ResponseWriter, r *http.Request, c call) {
		var req struct{ Offset, Limit int }
		json.Unmarshal([]byte(c.Body), &req)
		const total = 45
		n := min(req.Limit, total-req.Offset)
		var items []map[string]any
		for i := 0; i < n; i++ {
			items = append(items, map[string]any{"title": "Intern " + fmt.Sprint(req.Offset+i), "externalPath": "/job/X/Intern_" + fmt.Sprint(req.Offset+i), "postedOn": "Posted Today", "bulletFields": []string{fmt.Sprint("R", req.Offset+i)}})
		}
		t0 := total
		if req.Offset > 0 {
			t0 = 0 // Workday answers total 0 after the first page
		}
		b, _ := json.Marshal(map[string]any{"total": t0, "jobPostings": items})
		serve(w, b)
	})
	s := Workday{F: f, MaxDetail: 1}.Read(ctx(), "acme.wd3/Careers")
	// the single detail call fails (the fake serves no detail for GET) so the scan is partial, but all 45 are listed
	if len(s.Jobs) != 45 {
		t.Fatalf("jobs: %d (%s %s)", len(s.Jobs), s.Status, s.Error)
	}
	posts := 0
	for _, c := range fw.Calls {
		if c.Method == "POST" {
			posts++
		}
	}
	eq(t, "list pages", posts, 3)
}

func TestWorkdayRejectsAMalformedToken(t *testing.T) {
	s := Workday{F: &Fetcher{}}.Read(ctx(), "nvidia/NVIDIAExternalCareerSite")
	if s.Status != jobs.ScanFailed || !strings.Contains(s.Error, "tenant.wdN/site") {
		t.Fatalf("%+v", s)
	}
}

func TestParseWorkdayPostedIsRelativeToTheFetchTime(t *testing.T) {
	fetched := time.Date(2026, 11, 22, 19, 30, 0, 0, time.UTC)
	cases := []struct {
		label string
		want  string
	}{
		{"Posted Today", "2026-11-22"},
		{"Posted Yesterday", "2026-11-21"},
		{"Posted 1 Day Ago", "2026-11-21"},
		{"Posted 3 Days Ago", "2026-11-19"},
		{"posted 11 days ago", "2026-11-11"},
		{"Posted 30+ Days Ago", ""}, // only a lower bound: nothing is guessed
		{"Posted a while back", ""},
		{"", ""},
	}
	for _, c := range cases {
		got := ParseWorkdayPosted(c.label, fetched)
		switch {
		case c.want == "" && got != nil:
			t.Errorf("%q: want nil, got %v", c.label, got)
		case c.want != "" && (got == nil || got.Format("2006-01-02") != c.want):
			t.Errorf("%q: got %v want %s", c.label, got, c.want)
		}
	}
	// across a month boundary
	got := ParseWorkdayPosted("Posted 3 Days Ago", time.Date(2026, 3, 2, 1, 0, 0, 0, time.UTC))
	eq(t, "month boundary", got.Format("2006-01-02"), "2026-02-27")
}

// ---- Recruitee (fixture: real capture of bunq.recruitee.com/api/offers/, trimmed to 2 offers) ----------------

func TestRecruiteeReadsTheRealBoard(t *testing.T) {
	fw, f := newFakeWeb(t, func(w http.ResponseWriter, r *http.Request, c call) { serve(w, fixture(t, "recruitee.json")) })
	s := Recruitee{F: f}.Read(ctx(), "bunq")
	mustOK(t, s, 2)
	if c := fw.Calls[0]; c.Host != "bunq.recruitee.com" || c.Path != "/api/offers/" {
		t.Fatalf("%+v", c)
	}
	j := s.Jobs[0]
	eq(t, "id", j.ExternalID, "2832667")
	eq(t, "title", j.Title, "Copywriter")
	eq(t, "employer", j.EmployerName, "bunq")
	eq(t, "url", j.URL, "https://careers.bunq.com/o/copywriter-3")
	eq(t, "apply", j.ApplyURL, "https://careers.bunq.com/o/copywriter-3/c/new")
	eq(t, "employment", j.EmploymentType, "fulltime_permanent")
	eq(t, "department", j.Department, "Content & Design")
	eq(t, "locations", j.Locations, "[Amsterdam, Noord-Holland, Netherlands İstanbul, İstanbul, Türkiye Sofia, Sofia (stolitsa), Bulgaria]")
	if j.Remote == nil || *j.Remote {
		t.Errorf("remote false is kept: %v", j.Remote)
	}
	atTime(t, "posted", j.PostedAt, "2026-11-18T16:23:32Z")
	atTime(t, "updated", j.UpdatedAt, "2026-11-18T17:10:12Z")
	eq(t, "no salary given", j.Compensation, "")
	if j.Deadline != nil || strings.Contains(j.Description, "<") || !strings.Contains(j.Description, "bunq") {
		t.Errorf("deadline/description: %v %q", j.Deadline, j.Description)
	}
}

func TestRecruiteeSalaryAndUnpublished(t *testing.T) {
	_, f := newFakeWeb(t, func(w http.ResponseWriter, r *http.Request, c call) {
		serve(w, []byte(`{"offers":[{"id":1,"title":"A","careers_url":"https://c/a","status":"published","salary":{"min":"2000","max":"3000","period":"month","currency":"EUR"},"close_at":"2026-12-31 00:00:00 UTC"},
		{"id":2,"title":"B","careers_url":"https://c/b","status":"draft"}]}`))
	})
	s := Recruitee{F: f}.Read(ctx(), "x")
	mustOK(t, s, 1)
	eq(t, "salary", s.Jobs[0].Compensation, "EUR 2000 - 3000 month")
	atTime(t, "deadline", s.Jobs[0].Deadline, "2026-12-31T00:00:00Z")
}

// ---- Workable (fixtures: real capture of apply.workable.com account "huggingface") -------------------------------

func TestWorkablePagesByTokenAndReadsDetail(t *testing.T) {
	var pageTwoBody string
	fw, f := newFakeWeb(t, func(w http.ResponseWriter, r *http.Request, c call) {
		switch {
		case r.Method == "POST" && !strings.Contains(c.Body, `"token"`):
			serve(w, fixture(t, "workable_list.json"))
		case r.Method == "POST":
			pageTwoBody = c.Body
			serve(w, []byte(`{"results":[],"total":10}`))
		default:
			serve(w, fixture(t, "workable_detail.json"))
		}
	})
	s := Workable{F: f}.Read(ctx(), "huggingface")
	mustOK(t, s, 3)
	eq(t, "pages", s.Pages, 2+3) // two list pages and three details
	if c := fw.Calls[0]; c.Host != "apply.workable.com" || c.Path != "/api/v3/accounts/huggingface/jobs" || c.Method != "POST" {
		t.Fatalf("%+v", c)
	}
	if !strings.Contains(pageTwoBody, `"token":"WzE3MjkxMjMyMDAwMDAsMjc2OTUwNV0="`) {
		t.Errorf("the next page is asked for with the nextPage token: %s", pageTwoBody)
	}
	j := s.Jobs[0]
	eq(t, "id", j.ExternalID, "2F83B48969")
	eq(t, "title", j.Title, "Senior Software Engineer, Bot Detection (Client-Side & Signals) - EMEA Remote")
	eq(t, "url", j.URL, "https://apply.workable.com/huggingface/j/2F83B48969/")
	eq(t, "employment", j.EmploymentType, "full")
	eq(t, "department", j.Department, "Product")
	eq(t, "locations", j.Locations, "[France]")
	atTime(t, "posted", j.PostedAt, "2026-11-20T00:00:00Z")
	if j.Remote == nil || !*j.Remote {
		t.Errorf("remote: %v", j.Remote)
	}
	if !strings.Contains(j.Description, "Hugging Face") || strings.Contains(j.Description, "<") {
		t.Errorf("description: %q", j.Description)
	}
}

func TestWorkableSkipsInternalAndUnpublishedPostings(t *testing.T) {
	_, f := newFakeWeb(t, func(w http.ResponseWriter, r *http.Request, c call) {
		if r.Method == "POST" {
			serve(w, []byte(`{"results":[{"shortcode":"A","title":"Open","state":"published"},{"shortcode":"B","title":"Internal","state":"published","isInternal":true},{"shortcode":"C","title":"Draft","state":"draft"}]}`))
			return
		}
		serve(w, []byte(`{"description":"<p>Text</p>"}`))
	})
	s := Workable{F: f}.Read(ctx(), "x")
	mustOK(t, s, 1)
	eq(t, "title", s.Jobs[0].Title, "Open")
}

func TestWorkableFollowsTokensAcrossPages(t *testing.T) {
	fw, f := newFakeWeb(t, func(w http.ResponseWriter, r *http.Request, c call) {
		if r.Method != "POST" {
			serve(w, []byte(`{"description":"d"}`))
			return
		}
		switch {
		case !strings.Contains(c.Body, `"token"`):
			serve(w, []byte(`{"results":[{"shortcode":"A","title":"One"}],"nextPage":"T1"}`))
		case strings.Contains(c.Body, `"T1"`):
			serve(w, []byte(`{"results":[{"shortcode":"B","title":"Two"}],"nextPage":"T2"}`))
		default:
			serve(w, []byte(`{"results":[{"shortcode":"C","title":"Three"}]}`))
		}
	})
	s := Workable{F: f}.Read(ctx(), "x")
	mustOK(t, s, 3)
	eq(t, "list requests", fw.count("/api/v3/"), 3)
}

// ---- malformed answers: failed with the error, never ok-and-empty, never a panic ---------------------------------

var testTargets = map[string]string{
	"greenhouse": "acme", "lever": "acme", "lever_eu": "acme", "ashby": "acme", "smartrecruiters": "acme",
	"workday": "acme.wd1/Site", "recruitee": "acme", "workable": "acme", "unstop": "software",
	"careers_jsonld": "https://careers.example.com/jobs", "internshala": "/internships/x/", "interndock": "/internships/india",
}

func TestMalformedAnswersAreFailedNeverOKAndEmpty(t *testing.T) {
	bodies := map[string]string{
		"truncated json":     `{"jobs":[{"id":1,"title":"x"`,
		"html instead":       `<html><body>Service unavailable</body></html>`,
		"wrong shape":        `{"unexpected":true}`,
		"empty body":         ``,
		"error object":       `{"ok":false,"error":"Document not found"}`,
		"json null":          `null`,
		"array not object":   `[1,2,3]`,
		"object not array":   `{"jobs":"nope"}`,
		"posting wrong type": `{"jobs":[{"id":{"a":1},"title":5}]}`,
	}
	for name, body := range bodies {
		_, f := newFakeWeb(t, func(w http.ResponseWriter, r *http.Request, c call) { serve(w, []byte(body)) })
		for provider, a := range Adapters(f) {
			t.Run(provider+"/"+name, func(t *testing.T) {
				s := a.Read(ctx(), testTargets[provider])
				if s.Status == jobs.ScanOK && len(s.Jobs) == 0 {
					t.Fatalf("ok and empty on %q", name)
				}
				if s.Status != jobs.ScanOK && (s.Error == "" || len(s.Jobs) != 0) {
					t.Fatalf("a %s scan names its error and carries no jobs: %+v", s.Status, s)
				}
				if s.Source != provider || s.Target != testTargets[provider] {
					t.Fatalf("scan identity: %q %q", s.Source, s.Target)
				}
			})
		}
	}
}

func TestEveryProviderFailsWhenTheBoardIs404(t *testing.T) {
	_, f := newFakeWeb(t, func(w http.ResponseWriter, r *http.Request, c call) { fail(w, 404) })
	for provider, a := range Adapters(f) {
		s := a.Read(ctx(), testTargets[provider])
		if s.Status != jobs.ScanFailed || !strings.Contains(s.Error, "404") {
			t.Errorf("%s: %s %q", provider, s.Status, s.Error)
		}
	}
}

func TestAnEmptyButValidBoardIsOKWithNoJobs(t *testing.T) {
	_, f := newFakeWeb(t, func(w http.ResponseWriter, r *http.Request, c call) { serve(w, []byte(`{"jobs":[]}`)) })
	s := Greenhouse{F: f}.Read(ctx(), "acme")
	mustOK(t, s, 0)
}

func TestRegistryHasEveryProviderUnderItsOwnName(t *testing.T) {
	m := Adapters(nil)
	want := []string{"greenhouse", "lever", "lever_eu", "ashby", "smartrecruiters", "workday", "recruitee", "workable", "careers_jsonld", "internshala", "unstop", "interndock"}
	if len(m) != len(want) {
		t.Fatalf("%d adapters", len(m))
	}
	for _, p := range want {
		if a, ok := m[p]; !ok || a.Provider() != p {
			t.Errorf("missing or misnamed: %s", p)
		}
	}
}

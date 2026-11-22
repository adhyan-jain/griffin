package ats

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/griffin/core/internal/jobs"
)

const ghBoard = `{"jobs":[{"id":101,"title":"Software Engineer Intern","absolute_url":"https://boards.greenhouse.io/acme/jobs/101","company_name":"Acme","location":{"name":"Bengaluru"},"content":"&lt;p&gt;Build.&lt;/p&gt;"}]}`

func TestDiscoverKeepsOnlyTheBoardsThatReadOK(t *testing.T) {
	page := `<html><body>
	  <a href="https://boards.greenhouse.io/acme">Open roles</a>
	  <a href="https://boards.greenhouse.io/ghost">Old board</a>
	  <a href="https://jobs.lever.co/acme-labs">Labs</a>
	  <a href="https://boards.greenhouse.io/embed/job_app?token=1">embed route, not a board</a>
	</body></html>`
	fw, f := newFakeWeb(t, func(w http.ResponseWriter, r *http.Request, c call) {
		switch {
		case c.Path == "/careers":
			w.Write([]byte(page))
		case strings.HasPrefix(c.Path, "/v1/boards/acme/jobs"):
			w.Write([]byte(ghBoard))
		case strings.HasPrefix(c.Path, "/v1/boards/ghost/jobs"):
			http.NotFound(w, r)
		case strings.HasPrefix(c.Path, "/v0/postings/acme-labs"):
			w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	})
	d := Discover(context.Background(), f, Adapters(f), "https://acme.example/careers")
	if !d.Fetched || len(d.Verified) != 2 || len(d.Rejected) != 1 {
		t.Fatalf("%+v", d)
	}
	got := map[BoardRef]int{}
	for _, v := range d.Verified {
		if v.Scan.Status != jobs.ScanOK {
			t.Errorf("a verified board was read ok: %+v", v)
		}
		got[v.Ref] = len(v.Scan.Jobs)
	}
	if got[BoardRef{"greenhouse", "acme"}] != 1 {
		t.Errorf("the Greenhouse board and its posting: %v", got)
	}
	if n, ok := got[BoardRef{"lever", "acme-labs"}]; !ok || n != 0 {
		t.Errorf("a board with no openings still read ok: %v", got)
	}
	if r := d.Rejected[0]; r.Ref.Token != "ghost" || r.Status != jobs.ScanFailed || !strings.Contains(r.Error, "404") {
		t.Errorf("a token that 404s is rejected with its reason: %+v", r)
	}
	if fw.count("/v1/boards/embed") != 0 {
		t.Error("a route is not a board and is never asked for")
	}
}

func TestDiscoverFallsBackToJobPostingJSONLDWhenThePageNamesNoBoard(t *testing.T) {
	_, f := newFakeWeb(t, func(w http.ResponseWriter, r *http.Request, c call) {
		w.Write([]byte(ldSingle))
	})
	d := Discover(context.Background(), f, Adapters(f), "https://acme.example/careers")
	if len(d.Verified) != 1 || d.Verified[0].Ref != (BoardRef{"careers_jsonld", "https://acme.example/careers"}) || len(d.Verified[0].Scan.Jobs) != 1 {
		t.Fatalf("%+v", d)
	}
}

func TestDiscoverOnAPageWithNothingFindsNothing(t *testing.T) {
	_, f := newFakeWeb(t, func(w http.ResponseWriter, r *http.Request, c call) {
		w.Write([]byte(`<html><p>We are hiring, email us.</p></html>`))
	})
	d := Discover(context.Background(), f, Adapters(f), "https://acme.example/careers")
	if !d.Fetched || len(d.Verified) != 0 || len(d.Rejected) != 0 {
		t.Fatalf("an empty page is read, and yields no board: %+v", d)
	}
}

func TestDiscoverReportsAPageThatCannotBeRead(t *testing.T) {
	_, f := newFakeWeb(t, func(w http.ResponseWriter, r *http.Request, c call) { http.NotFound(w, r) })
	d := Discover(context.Background(), f, Adapters(f), "https://acme.example/careers")
	if d.Fetched || d.PageErr == "" || len(d.Verified) != 0 {
		t.Fatalf("%+v", d)
	}
	if d := Discover(context.Background(), f, Adapters(f), "ftp://acme.example/careers"); d.Fetched || d.PageErr == "" {
		t.Fatalf("only http(s) pages are read: %+v", d)
	}
}

func TestCareersURLsAreTheCompanysOwnPublicDomainOnly(t *testing.T) {
	got := CareersURLs("https://www.Razorpay.com/")
	if len(got) != 2 || got[0] != "https://razorpay.com/careers" || got[1] != "https://razorpay.com/jobs" {
		t.Fatalf("%v", got)
	}
	for _, bad := range []string{"", "localhost", "127.0.0.1", "10.0.0.5", "intranet", "host.local", "a.example/x?y", "user@host.com", "host.com:8080", "svc.internal"} {
		if CareersURLs(bad) != nil {
			t.Errorf("%q is not a company domain to probe", bad)
		}
	}
}

func TestBoardPageURL(t *testing.T) {
	cases := map[BoardRef]string{
		{"greenhouse", "acme"}:                    "https://boards.greenhouse.io/acme",
		{"lever", "acme"}:                         "https://jobs.lever.co/acme",
		{"lever_eu", "acme"}:                      "https://jobs.eu.lever.co/acme",
		{"ashby", "acme"}:                         "https://jobs.ashbyhq.com/acme",
		{"smartrecruiters", "Acme1"}:              "https://careers.smartrecruiters.com/Acme1",
		{"workable", "acme"}:                      "https://apply.workable.com/acme",
		{"recruitee", "acme"}:                     "https://acme.recruitee.com",
		{"workday", "acme.wd5/External"}:          "https://acme.wd5.myworkdayjobs.com/External",
		{"careers_jsonld", "https://a.example/c"}: "https://a.example/c",
		{"greenhouse", ""}:                        "",
		{"unknown", "x"}:                          "",
	}
	for ref, want := range cases {
		if got := ref.PageURL(); got != want {
			t.Errorf("%+v: %q want %q", ref, got, want)
		}
	}
}

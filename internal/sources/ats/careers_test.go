package ats

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/griffin/core/internal/jobs"
)

// The JSON-LD pages below are hand-built from schema.org's JobPosting documentation and Google's job-posting
// guidelines; they are not captures of a live site.

const ldSingle = `<html><head><title>Careers</title>
<script type="application/ld+json">
{"@context":"https://schema.org","@type":"JobPosting","title":"Backend Engineer Intern","identifier":{"@type":"PropertyValue","name":"Acme","value":"REQ-42"},
 "url":"https://acme.example/careers/backend-intern","datePosted":"2026-11-01","validThrough":"2026-12-15T23:59:00Z",
 "employmentType":["INTERN","PART_TIME"],
 "description":"&lt;p&gt;Build things&lt;/p&gt;&lt;ul&gt;&lt;li&gt;Go&lt;/li&gt;&lt;li&gt;SQL&lt;/li&gt;&lt;/ul&gt;",
 "hiringOrganization":{"@type":"Organization","name":"Acme Corp","sameAs":"https://www.acme.example/about"},
 "jobLocation":{"@type":"Place","address":{"@type":"PostalAddress","addressLocality":"Pune","addressRegion":"MH","addressCountry":"IN"}},
 "baseSalary":{"@type":"MonetaryAmount","currency":"INR","value":{"@type":"QuantitativeValue","minValue":30000,"maxValue":40000,"unitText":"MONTH"}}}
</script></head><body></body></html>`

const ldArray = `<script type="application/ld+json">[
 {"@type":"Organization","name":"Acme"},
 {"@type":"JobPosting","title":"A","url":"https://acme.example/a","description":"<p>One</p>","jobLocationType":"TELECOMMUTE","hiringOrganization":"Acme"},
 {"@type":"JobPosting","title":"B","url":"https://acme.example/b","jobLocation":[{"address":{"addressLocality":"Pune","addressCountry":{"@type":"Country","name":"IN"}}},{"address":{"addressLocality":"Delhi"}}]}
]</script>`

const ldGraph = `<script type="application/ld+json; charset=utf-8">{"@context":"https://schema.org","@graph":[
 {"@type":"WebSite","name":"Acme"},
 {"@type":["Thing","JobPosting"],"title":"C","url":"https://acme.example/c"},
 {"@type":"BreadcrumbList"}]}</script>
<script type="application/ld+json">{"@type":"JobPosting","title":"D"}</script>`

func TestJSONLDSingleObject(t *testing.T) {
	now := time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC)
	posts, problems := ParseJobPostings(ldSingle, "https://acme.example/careers", now)
	if len(problems) != 0 || len(posts) != 1 {
		t.Fatalf("%v %v", posts, problems)
	}
	j := posts[0]
	eq(t, "source", j.Source, "careers_jsonld")
	eq(t, "kind", j.SourceKind, jobs.EmployerSite)
	eq(t, "title", j.Title, "Backend Engineer Intern")
	eq(t, "id", j.ExternalID, "REQ-42")
	eq(t, "url", j.URL, "https://acme.example/careers/backend-intern")
	eq(t, "employer", j.EmployerName, "Acme Corp")
	eq(t, "domain", j.EmployerDomain, "acme.example")
	eq(t, "employment", j.EmploymentType, "INTERN, PART_TIME")
	eq(t, "locations", j.Locations, "[Pune, MH, IN]")
	eq(t, "description", j.Description, "Build things\n\n- Go\n- SQL")
	eq(t, "pay", j.Compensation, "INR 30000 - 40000 MONTH")
	atTime(t, "posted", j.PostedAt, "2026-11-01T00:00:00Z")
	atTime(t, "deadline", j.Deadline, "2026-12-15T23:59:00Z")
	if j.Remote != nil || !j.FetchedAt.Equal(now) {
		t.Errorf("remote/fetched: %v %v", j.Remote, j.FetchedAt)
	}
}

func TestJSONLDArrayAndLocations(t *testing.T) {
	posts, problems := ParseJobPostings(ldArray, "https://acme.example/careers", time.Now())
	if len(problems) != 0 || len(posts) != 2 {
		t.Fatalf("%v %v", posts, problems)
	}
	if r := posts[0].Remote; r == nil || !*r {
		t.Error("TELECOMMUTE is remote")
	}
	eq(t, "employer as plain string", posts[0].EmployerName, "Acme")
	eq(t, "locations", posts[1].Locations, "[Pune, IN Delhi]")
	if posts[1].Remote != nil {
		t.Error("remote is not guessed")
	}
}

func TestJSONLDGraphAndTypeArraysAndMultipleBlocks(t *testing.T) {
	posts, problems := ParseJobPostings(ldGraph, "https://acme.example/careers", time.Now())
	if len(problems) != 0 || len(posts) != 2 {
		t.Fatalf("%v %v", posts, problems)
	}
	eq(t, "graph job", posts[0].Title, "C")
	eq(t, "second block without url falls back to the page", posts[1].URL, "https://acme.example/careers")
}

func careersFor(t *testing.T, page string) jobs.Scan {
	_, f := newFakeWeb(t, func(w http.ResponseWriter, r *http.Request, c call) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(page))
	})
	return CareersJSONLD{F: f}.Read(ctx(), "https://acme.example/careers")
}

func TestCareersPageScan(t *testing.T) {
	s := careersFor(t, ldSingle)
	mustOK(t, s, 1)
	eq(t, "kind", s.Jobs[0].SourceKind, jobs.EmployerSite)
}

func TestCareersPageWithoutJobPostingIsFailedNotEmpty(t *testing.T) {
	s := careersFor(t, `<html><body><h1>Careers</h1><script type="application/ld+json">{"@type":"Organization"}</script></body></html>`)
	if s.Status != jobs.ScanFailed || !strings.Contains(s.Error, "no schema.org JobPosting") {
		t.Fatalf("%+v", s)
	}
}

func TestCareersPageWithBrokenJSONLDOnlyIsFailed(t *testing.T) {
	s := careersFor(t, `<script type="application/ld+json">{"@type":"JobPosting","title":</script>`)
	if s.Status != jobs.ScanFailed || !strings.Contains(s.Error, "not valid JSON") {
		t.Fatalf("%+v", s)
	}
}

func TestCareersPageWithOneBrokenBlockIsPartial(t *testing.T) {
	s := careersFor(t, `<script type="application/ld+json">{"@type":"JobPosting","title":"Good","url":"https://a/1"}</script>
	<script type="application/ld+json">{"@type":"JobPosting",</script>`)
	if s.Status != jobs.ScanPartial || len(s.Jobs) != 1 || s.Complete() {
		t.Fatalf("%+v", s)
	}
}

func TestCareersRejectsNonHTTPTargets(t *testing.T) {
	s := CareersJSONLD{F: &Fetcher{}}.Read(ctx(), "file:///etc/passwd")
	if s.Status != jobs.ScanFailed {
		t.Fatalf("%+v", s)
	}
}

func TestDetectBoards(t *testing.T) {
	page := `<html><body>
<a href="https://boards.greenhouse.io/acme/jobs/123">Apply</a>
<a href="https://job-boards.greenhouse.io/acme">dup different host</a>
<iframe src="https://boards.greenhouse.io/embed/job_board?for=widgetco&amp;b=https%3A%2F%2Fx"></iframe>
<a href="https://jobs.lever.co/palantir/abc-123">Lever</a>
<a href="https://jobs.eu.lever.co/mistral">Lever EU</a>
<a href="https://jobs.ashbyhq.com/openai">Ashby</a>
<script>fetch("https:\/\/api.ashbyhq.com\/posting-api\/job-board\/ramp")</script>
<a href="https://careers.smartrecruiters.com/Visa/">SR</a>
<a href="https://apply.workable.com/huggingface/j/ABC/">Workable</a>
<a href="https://apply.workable.com/api/v3/accounts/hugging-face/jobs">Workable api</a>
<a href="https://bunq.recruitee.com/o/job">Recruitee</a>
<a href="https://www.recruitee.com/pricing">vendor site, not a board</a>
<a href="https://nvidia.wd5.myworkdayjobs.com/en-US/NVIDIAExternalCareerSite/job/x">Workday</a>
<a href="https://acme.wd3.myworkdayjobs.com/Acme_Careers">Workday 2</a>
<link href="https://boards.greenhouse.io/embed/job_board/js?for=widgetco">
<a href="https://example.com/jobs/lever">not an ATS</a>
</body></html>`
	got := DetectBoards(page)
	want := []BoardRef{
		{"greenhouse", "acme"}, {"greenhouse", "widgetco"}, {"lever", "palantir"}, {"lever_eu", "mistral"},
		{"ashby", "openai"}, {"ashby", "ramp"}, {"smartrecruiters", "Visa"}, {"workable", "huggingface"},
		{"workable", "hugging-face"}, {"recruitee", "bunq"},
		{"workday", "nvidia.wd5/NVIDIAExternalCareerSite"}, {"workday", "acme.wd3/Acme_Careers"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d: got %v want %v", i, got[i], want[i])
		}
	}
}

func TestDetectBoardsFindsNothingOnAPlainPage(t *testing.T) {
	if got := DetectBoards(`<a href="https://example.com/careers">Careers</a> greenhouse is a word`); len(got) != 0 {
		t.Fatalf("%v", got)
	}
}

func TestDetectedBoardsAreReadableByTheRegistry(t *testing.T) {
	m := Adapters(nil)
	for _, b := range DetectBoards(`https://jobs.lever.co/x https://jobs.eu.lever.co/y https://jobs.ashbyhq.com/z https://x.recruitee.com`) {
		if _, ok := m[b.Provider]; !ok {
			t.Errorf("no adapter for %s", b.Provider)
		}
	}
}

// ---- text and dates -----------------------------------------------------------------------------------------

func TestHTMLToText(t *testing.T) {
	cases := map[string]string{
		`<p>Hello&nbsp;<b>world</b></p><p>Next &amp; last</p>`: "Hello world\n\nNext & last",
		`<h2>Title</h2><ul><li>one</li><li>two</li></ul>after`: "Title\n\n- one\n- two\nafter",
		`a<br>b<br/>c`: "a\nb\nc",
		`<style>p{x:y}</style><script>alert(1)</script>text`:                "text",
		`&lt;p&gt;Escaped &amp;amp; entity&lt;/p&gt;&lt;p&gt;Two&lt;/p&gt;`: "Escaped & entity\n\nTwo",
		"<div>  spaced \n\n   out  </div>":                                  "spaced out",
		``:                                                                  "",
		`plain text, no tags`:                                               "plain text, no tags",
		`<p>unclosed <b>bold`:                                               "unclosed bold",
	}
	for in, want := range cases {
		if got := htmlToText(in); got != want {
			t.Errorf("%q\n got %q\nwant %q", in, got, want)
		}
	}
}

func TestParseTimeFormats(t *testing.T) {
	cases := map[string]string{
		"2026-11-19T12:12:24-05:00":     "2026-11-19T17:12:24Z",
		"2026-04-07T17:12:35.753+00:00": "2026-04-07T17:12:35.753Z",
		"2026-10-28T15:14:43.941Z":      "2026-10-28T15:14:43.941Z",
		"2026-11-18 16:23:32 UTC":       "2026-11-18T16:23:32Z",
		"2026-11-11":                    "2026-11-11T00:00:00Z",
		"2026-11-20T00:00:00.000Z":      "2026-11-20T00:00:00Z",
	}
	for in, want := range cases {
		atTime(t, in, parseTime(in), want)
	}
	for _, bad := range []string{"", "yesterday", "11/19/2026", "0000"} {
		if parseTime(bad) != nil {
			t.Errorf("%q must be nil", bad)
		}
	}
	if millisTime(0) != nil || millisTime(-5) != nil {
		t.Error("zero epoch is unknown")
	}
	atTime(t, "ms", millisTime(1786469891368), time.UnixMilli(1786469891368).UTC().Format(time.RFC3339Nano))
}

// ---- aggregators --------------------------------------------------------------------------------------------

func TestAggregatorsWrapTheExistingParsers(t *testing.T) {
	internshala, err := os.ReadFile("../testdata/internshala.html")
	if err != nil {
		t.Fatal(err)
	}
	unstop, err := os.ReadFile("../testdata/unstop.json")
	if err != nil {
		t.Fatal(err)
	}
	// hand-built InternDock card in the shape domain.ParseInternDock reads
	interndock := `<a href="/internships/software-engineer-intern/acme-corp-software-engineer-intern-1234" class="card"><h3>Software Engineer Intern</h3><span>Acme Corp</span><span>Bengaluru, India</span><span>Posted Nov 20, 2026</span><span>$25 per hour</span><span>Remote</span></a>`
	_, f := newFakeWeb(t, func(w http.ResponseWriter, r *http.Request, c call) {
		switch c.Host {
		case "internshala.com":
			w.Write(internshala)
		case "unstop.com":
			serve(w, unstop)
		default:
			w.Write([]byte(interndock))
		}
	})
	f.Now = func() time.Time { return time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC) }

	s := Internshala{F: f}.Read(ctx(), "/internships/software-development-internship/")
	mustOK(t, s, 4)
	j := s.Jobs[0]
	eq(t, "kind", j.SourceKind, jobs.Aggregator)
	eq(t, "title", j.Title, "Software Development Intern")
	eq(t, "pay verbatim", j.Compensation, "₹ 5,000 /month")
	if !strings.HasPrefix(j.URL, "https://internshala.com/internship/detail/") || j.BoardToken != "" || j.ExternalID == "" || j.EmployerName == "" {
		t.Errorf("%+v", j)
	}
	if r := s.Jobs[1].Remote; r == nil || !*r || s.Jobs[1].Locations[0] != "Remote (India)" {
		t.Errorf("work from home: %+v", s.Jobs[1])
	}

	u := Unstop{F: f}.Read(ctx(), "software")
	if u.Status != jobs.ScanOK || len(u.Jobs) == 0 || u.Jobs[0].SourceKind != jobs.Aggregator || u.Jobs[0].Source != "unstop" || u.Jobs[0].PostedAt == nil {
		t.Fatalf("%+v", u)
	}

	d := InternDock{F: f}.Read(ctx(), "/internships/india")
	mustOK(t, d, 1)
	eq(t, "company", d.Jobs[0].EmployerName, "Acme Corp")
	eq(t, "pay", d.Jobs[0].Compensation, "$25 per hour")
	atTime(t, "posted", d.Jobs[0].PostedAt, "2026-11-20T00:00:00Z")
	eq(t, "url", d.Jobs[0].URL, "https://www.interndock.com/internships/software-engineer-intern/acme-corp-software-engineer-intern-1234")
}

func TestAggregatorPageWithNoCardsIsFailed(t *testing.T) {
	_, f := newFakeWeb(t, func(w http.ResponseWriter, r *http.Request, c call) { w.Write([]byte("<html>changed markup</html>")) })
	for _, a := range []Adapter{Internshala{F: f}, InternDock{F: f}} {
		if s := a.Read(ctx(), "/x"); s.Status != jobs.ScanFailed || s.Error == "" {
			t.Errorf("%s: %+v", a.Provider(), s)
		}
	}
}

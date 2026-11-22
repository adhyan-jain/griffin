package ats

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/jobs"
)

func TestTheSeedFileIsSoundAndEverySeedWasVerified(t *testing.T) {
	f := Seeds() // panics on a malformed file
	if f.Version != 1 || f.VerifiedOn == "" || len(f.Seeds) < 60 {
		t.Fatalf("a versioned file verified on a day, with at least 60 boards: version %d verified %q seeds %d", f.Version, f.VerifiedOn, len(f.Seeds))
	}
	tokens, companies := map[string]string{}, map[string]string{}
	for _, s := range f.Seeds {
		if !domain.ATSProviders[s.Provider] || !SeedProviders[s.Provider] {
			t.Errorf("%s: %q is not a provider the boards table allows", s.Name, s.Provider)
		}
		key := s.Provider + "|" + strings.ToLower(s.Token)
		if other, dup := tokens[key]; dup {
			t.Errorf("%s and %s share the board %s", s.Name, other, key)
		}
		tokens[key] = s.Name
		if strings.TrimSpace(s.Token) != s.Token || strings.ContainsAny(s.Token, " /?#") {
			t.Errorf("%s: token %q is not a bare slug", s.Name, s.Token)
		}
		if !strings.HasPrefix(s.VerifiedOn, "20") || len(s.VerifiedOn) != 10 {
			t.Errorf("%s: verification date %q", s.Name, s.VerifiedOn)
		}
		if s.Identity == "" {
			t.Errorf("%s: a seed says how its board was shown to be the company's", s.Name)
		}
		if s.Domain == "" || strings.Contains(s.Domain, "/") || !strings.Contains(s.Domain, ".") {
			t.Errorf("%s: domain %q", s.Name, s.Domain)
		}
		if s.Region != "india" && s.Region != "global" && s.Region != "remote" {
			t.Errorf("%s: region %q", s.Name, s.Region)
		}
		n := domain.NormalizeEmployer(s.Name)
		if other, dup := companies[n]; dup && other != s.Name {
			t.Errorf("%s and %s are one employer to the table", s.Name, other)
		}
		companies[n] = s.Name
	}
}

func TestParseSeedsRejectsAnythingThatIsNotAVerifiedKnownBoard(t *testing.T) {
	ok := `{"version":1,"verified_on":"2026-11-22","seeds":[{"name":"A","domain":"a.com","kind":"startup","region":"india","provider":"greenhouse","board":"a","verified_on":"2026-11-22","jobs_seen":1,"identity":"x"}]}`
	if _, err := ParseSeeds([]byte(ok)); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"unknown provider": strings.Replace(ok, `"greenhouse"`, `"taleo"`, 1),
		"aggregator":       strings.Replace(ok, `"greenhouse"`, `"internshala"`, 1),
		"no token":         strings.Replace(ok, `"board":"a"`, `"board":""`, 1),
		"no date":          strings.Replace(ok, `"verified_on":"2026-11-22","jobs_seen"`, `"verified_on":"","jobs_seen"`, 1),
		"unknown kind":     strings.Replace(ok, `"startup"`, `"unicorn"`, 1),
		"duplicate board":  strings.Replace(ok, `}]}`, `},{"name":"B","domain":"b.com","kind":"startup","region":"india","provider":"greenhouse","board":"A","verified_on":"2026-11-22","identity":"x"}]}`, 1),
		"not json":         `{`,
	}
	for name, in := range cases {
		if _, err := ParseSeeds([]byte(in)); err == nil {
			t.Errorf("%s should be rejected", name)
		}
	}
}

func TestBoardIdentityNeedsASignThatTheBoardIsTheCompanys(t *testing.T) {
	scan := func(js ...jobs.RawJob) jobs.Scan { return jobs.Scan{Status: jobs.ScanOK, Jobs: js} }
	cases := []struct {
		name   string
		scan   jobs.Scan
		wantID bool
	}{
		{"employer name", scan(jobs.RawJob{EmployerName: "Razorpay Software Private Limited", Title: "Intern"}), true},
		{"link on the company's domain", scan(jobs.RawJob{Title: "Intern", URL: "https://razorpay.com/jobs/?gh_jid=1"}), true},
		{"text names the company", scan(jobs.RawJob{Title: "Intern", Description: "At Razorpay you will build payments."}), true},
		{"the company's domain in the text", scan(jobs.RawJob{Title: "Intern", Description: "Apply at razorpay.com/careers."}), true},
		{"the name as a sentence subject", scan(jobs.RawJob{Title: "Intern", Description: "Razorpay is a payments company."}), true},
		{"another company", scan(jobs.RawJob{EmployerName: "Rzr Foods", Title: "Intern", URL: "https://boards.greenhouse.io/razorpay/jobs/1", Description: "Make snacks."}), false},
		{"the token in a hosted link is not evidence", scan(jobs.RawJob{Title: "Intern", URL: "https://jobs.lever.co/razorpay/1"}), false},
		{"an empty board cannot be told from another company's", scan(), false},
		{"the bare word is not the company", scan(jobs.RawJob{Title: "Intern", Description: "You will razorpay the invoices and work with a razorpay engine."}), false},
		{"a word inside another word is not the name", scan(jobs.RawJob{Title: "Intern", Description: "We are razorpayments-unrelated"}), false},
	}
	for _, c := range cases {
		got := BoardIdentity("Razorpay", "razorpay.com", c.scan)
		if (got != "") != c.wantID {
			t.Errorf("%s: identity %q", c.name, got)
		}
	}
}

func TestVerifyCandidateSeedsOnlyABoardThatReadsOKAndIsTheCompanys(t *testing.T) {
	_, f := newFakeWeb(t, func(w http.ResponseWriter, r *http.Request, c call) {
		switch {
		case strings.HasPrefix(c.Path, "/v1/boards/other/jobs"):
			w.Write([]byte(`{"jobs":[{"id":1,"title":"Intern","absolute_url":"https://boards.greenhouse.io/other/jobs/1","company_name":"Somebody Else","content":"Snacks."}]}`))
		case strings.HasPrefix(c.Path, "/v1/boards/empty/jobs"):
			w.Write([]byte(`{"jobs":[]}`))
		case strings.HasPrefix(c.Path, "/v1/boards/acme/jobs"):
			w.Write([]byte(ghBoard))
		default:
			http.NotFound(w, r)
		}
	})
	c := Candidate{Name: "Acme", Domain: "acme.com", Kind: "startup", Region: "india",
		Tries: []Try{{"greenhouse", "missing"}, {"greenhouse", "other"}, {"greenhouse", "empty"}, {"taleo", "acme"}, {"greenhouse", "acme"}}}
	seed, ok, outs := VerifyCandidate(context.Background(), Adapters(f), c, "2026-11-22")
	if !ok || seed.Token != "acme" || seed.Provider != "greenhouse" || seed.VerifiedOn != "2026-11-22" || seed.JobsSeen != 1 || seed.Identity == "" {
		t.Fatalf("%+v %v", seed, ok)
	}
	if len(outs) != 5 || outs[0].Status != "failed" || outs[1].Status != "ok" || outs[1].Identity != "" || outs[2].Note == "" || outs[3].Status != "skipped" || outs[4].Identity == "" {
		t.Fatalf("every try is on record: %+v", outs)
	}
	if _, ok, _ := VerifyCandidate(context.Background(), Adapters(f), Candidate{Name: "Acme", Domain: "acme.com", Tries: []Try{{"greenhouse", "missing"}, {"greenhouse", "empty"}}}, "2026-11-22"); ok {
		t.Fatal("a company with no board that reads and names it is dropped")
	}
}

func TestASharedNameNeedsTheCompanysLineOfWorkInThePostings(t *testing.T) {
	_, f := newFakeWeb(t, func(w http.ResponseWriter, r *http.Request, c call) {
		switch {
		case strings.HasPrefix(c.Path, "/v1/boards/pilots/jobs"):
			w.Write([]byte(`{"jobs":[{"id":1,"title":"Founding Engineer","absolute_url":"https://boards.greenhouse.io/pilots/jobs/1","content":"At Navi we capture cockpit audio."}]}`))
		case strings.HasPrefix(c.Path, "/v1/boards/lender/jobs"):
			w.Write([]byte(`{"jobs":[{"id":2,"title":"Backend Engineer","absolute_url":"https://boards.greenhouse.io/lender/jobs/2","content":"At Navi we build loans in Bengaluru."}]}`))
		default:
			http.NotFound(w, r)
		}
	})
	c := Candidate{Name: "Navi", Domain: "navi.com", Region: "india", Needs: []string{"Bengaluru", "loan"},
		Tries: []Try{{"greenhouse", "pilots"}, {"greenhouse", "lender"}}}
	seed, ok, outs := VerifyCandidate(context.Background(), Adapters(f), c, "2026-11-22")
	if !ok || seed.Token != "lender" || outs[0].Identity != "" || !strings.Contains(outs[0].Note, "may be another company") {
		t.Fatalf("the other Navi is not seeded: %+v %v %+v", seed, ok, outs)
	}
}

func TestAnOrdinaryWordIsNotTheCompany(t *testing.T) {
	scan := jobs.Scan{Status: jobs.ScanOK, Jobs: []jobs.RawJob{{Title: "Intern", Description: "Learn linear algebra and render pipelines."}}}
	if id := BoardIdentity("Linear", "linear.app", scan); id != "" {
		t.Errorf("identity %q", id)
	}
	scan.Jobs[0].Description = "At Linear we make issue tracking fast."
	if id := BoardIdentity("Linear", "linear.app", scan); id == "" {
		t.Error("the company's name used as a company is evidence")
	}
}

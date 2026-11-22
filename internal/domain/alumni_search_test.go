package domain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

var searchDay = time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC)

func TestRoleAndCompanyFromHeadlines(t *testing.T) {
	cases := []struct{ headline, role, company string }{
		{"SDE-2 @ Razorpay | VIT '22 | ACM", "SDE-2", "Razorpay"},
		{"Software Engineer at Google", "Software Engineer", "Google"},
		{"Backend Engineer at Zomato, Gurugram | VIT Vellore", "Backend Engineer", "Zomato"},
		{"ML Engineer @ Flipkart (ex-Amazon) • VIT", "ML Engineer", "Flipkart"},
		{"VIT '21 | Software Development Engineer II at Amazon Web Services", "Software Development Engineer II", "Amazon Web Services"},
	}
	for _, c := range cases {
		role, company, ok := RoleAndCompany(c.headline)
		if !ok || role != c.role || company != c.company {
			t.Errorf("%q: got %q at %q ok=%v, want %q at %q", c.headline, role, company, ok, c.role, c.company)
		}
	}
	for _, none := range []string{"Software Engineer", "VIT Vellore | Coder", "Engineer at Stealth Startup", "Teaching Assistant at VIT Vellore", "Engineer at Self-employed", ""} {
		if r, c, ok := RoleAndCompany(none); ok {
			t.Errorf("%q: should have no company, got %q at %q", none, r, c)
		}
	}
}

func TestBatchOnlyWhenTheHeadlineShowsAYear(t *testing.T) {
	for headline, want := range map[string]string{"SDE @ X | VIT '22": "2022", "SDE @ X | VIT 2021": "2021", "SDE @ X | VIT": "", "SDE @ X | 24x7 on-call": ""} {
		if got := BatchOf(headline); got != want {
			t.Errorf("%q: batch %q, want %q", headline, got, want)
		}
	}
}

func hit(headline string) PersonHit {
	return PersonHit{Name: "Riya Sen", Headline: headline, URL: "https://www.linkedin.com/in/riya-sen-1a2b3c/?miniProfileUrn=x", Location: "Bengaluru"}
}

func TestAlumniRulesKeepWorkingVITAlumniOnly(t *testing.T) {
	cases := []struct {
		name     string
		h        PersonHit
		skip     string
		club     string
		batch    string
		company  string
		roleWant string
	}{
		{"acm alum", hit("SDE-2 @ Razorpay | VIT '22 | ACM"), "", "ACM VIT", "2022", "Razorpay", "SDE-2"},
		{"no club unless the headline says ACM", hit("Software Engineer at Google | VIT Vellore"), "", "", "", "Google", "Software Engineer"},
		{"no VIT anywhere", hit("Software Engineer at Google"), "vit", "", "", "", ""},
		{"another VIT campus", hit("Software Engineer at Google | VIT Bhopal"), "vit", "", "", "", ""},
		{"no company", hit("VIT Vellore | Coder | ACM"), "company", "", "", "", ""},
		{"recruiter", hit("Technical Recruiter at Google | VIT Vellore"), "recruiter", "", "", "", ""},
		{"talent acquisition", hit("Talent Acquisition @ Razorpay | VIT"), "recruiter", "", "", "", ""},
		{"hr", hit("HR Manager at Infosys | VIT Vellore"), "recruiter", "", "", "", ""},
		{"student", hit("Student at VIT Vellore | Aspiring SDE"), "student", "", "", "", ""},
		{"intern", hit("SDE Intern @ Razorpay | VIT '25"), "student", "", "", "", ""},
		{"has not graduated", hit("Backend Engineer at Zoho | VIT '27"), "student", "", "", "", ""},
		{"open to work", hit("Software Engineer at Google | VIT | Open to work"), "student", "", "", "", ""},
		{"linkedin member", PersonHit{Name: "LinkedIn Member", Headline: "SDE at Google | VIT", URL: "https://www.linkedin.com/in/acoaabc/"}, "profile", "", "", "", ""},
		{"not a profile link", PersonHit{Name: "X Y", Headline: "SDE at Google | VIT", URL: "https://www.linkedin.com/company/google/"}, "profile", "", "", "", ""},
	}
	for _, c := range cases {
		p, skip := AlumniFromHit(c.h, searchDay)
		if skip != c.skip {
			t.Errorf("%s: skip %q, want %q", c.name, skip, c.skip)
			continue
		}
		if skip == "" && (p.Club != c.club || p.Batch != c.batch || p.Company != c.company || p.Role != c.roleWant || p.LinkedIn != "https://www.linkedin.com/in/riya-sen-1a2b3c/") {
			t.Errorf("%s: got %+v", c.name, p)
		}
	}
}

func TestVITInTheCardsOtherLinesCounts(t *testing.T) {
	h := hit("Backend Engineer at Swiggy")
	h.Snippet = "Past: Software Engineer Intern at Zoho\nVellore Institute of Technology"
	if p, skip := AlumniFromHit(h, searchDay); skip != "" || p.Company != "Swiggy" {
		t.Fatalf("skip=%q p=%+v", skip, p)
	}
}

func TestAlumniRowsMatchTheListShapeAndDedupe(t *testing.T) {
	hits := []PersonHit{
		hit("SDE-2 @ Razorpay | VIT '22 | ACM"),
		{Name: "Riya Sen", Headline: "SDE-2 @ Razorpay | VIT '22", URL: "https://in.linkedin.com/in/RIYA-SEN-1a2b3c"}, // same person, other spelling
		{Name: "Dev Rao", Headline: "Software Engineer at Google | VIT Vellore", URL: "https://www.linkedin.com/in/dev-rao/"},
		{Name: "Tia Roy", Headline: "Recruiter at Google | VIT", URL: "https://www.linkedin.com/in/tia-roy/"},
		{Name: "Kim Lee", Headline: "Software Engineer", URL: "https://www.linkedin.com/in/kim-lee/"},
	}
	table, st := AlumniRows(hits, searchDay, 40)
	lines := strings.Split(table, "\n")
	if len(lines) != 4 || lines[0]+"\n"+lines[1] != AlumniHeader {
		t.Fatalf("table:\n%s", table)
	}
	want := "| Riya Sen | 2022 | Razorpay | SDE-2 | ACM VIT | https://www.linkedin.com/in/riya-sen-1a2b3c/ |  | not contacted | 2026-11-22 |"
	if lines[2] != want {
		t.Fatalf("row:\n%s\nwant:\n%s", lines[2], want)
	}
	if !strings.Contains(lines[3], "| Dev Rao |  | Google | Software Engineer |  | https://www.linkedin.com/in/dev-rao/ |") {
		t.Fatalf("batch and club stay empty when the headline shows none: %s", lines[3])
	}
	if long, _ := AlumniRows([]PersonHit{{Name: "Long Handle", Headline: "SDE at Google | VIT", URL: "https://www.linkedin.com/in/" + strings.Repeat("a", 31) + "/"}}, searchDay, 5); !PlaybookLooksSafe(long) || long == "" {
		t.Fatalf("the longest handle kept must pass the list's credential check:\n%s", long)
	}
	if st.Seen != 5 || st.Kept != 2 || st.Duplicates != 1 || st.Recruiters != 1 || st.NoCompany != 1 {
		t.Fatalf("stats %+v", st)
	}
	if strings.Contains(st.Report(), "%!") {
		t.Fatal(st.Report())
	}
}

func TestAlumniRowsStopAtMaxAndSayNothingWhenNobodyPasses(t *testing.T) {
	var hits []PersonHit
	for _, n := range []string{"a", "b", "c"} {
		hits = append(hits, PersonHit{Name: "Person " + n, Headline: "SDE at Google | VIT", URL: "https://www.linkedin.com/in/" + n + "/"})
	}
	if table, st := AlumniRows(hits, searchDay, 2); st.Kept != 2 || strings.Count(table, "\n") != 3 {
		t.Fatalf("%+v\n%s", st, table)
	}
	if table, _ := AlumniRows(hits[:0], searchDay, 5); table != "" {
		t.Fatalf("no table without people: %q", table)
	}
}

func TestAlumniRowsMergeIntoTheListWithoutDuplicates(t *testing.T) {
	existing := AlumniHeader + "\n| Riya Sen | 2023 | Google | SDE | ACM | https://www.linkedin.com/in/riya-sen-1a2b3c/ | why | contacted 2026-11-20 | 2026-11-01 |"
	table, _ := AlumniRows([]PersonHit{
		hit("SDE-2 @ Razorpay | VIT '22 | ACM"),
		{Name: "Dev Rao", Headline: "Software Engineer at Google | VIT Vellore", URL: "https://www.linkedin.com/in/dev-rao/"},
	}, searchDay, 40)
	out, n := MergeAlumni(existing, table)
	if n != 1 || strings.Count(out, "Riya Sen") != 1 || !strings.Contains(out, "Dev Rao") || !strings.Contains(out, "contacted 2026-11-20") {
		t.Fatalf("n=%d\n%s", n, out)
	}
	// A second identical run adds nobody.
	if _, n := MergeAlumni(out, table); n != 0 {
		t.Fatalf("second run added %d", n)
	}
	// The new rows are what the DM batch can pick up.
	if ps := AlumniDMProposals(out, 8); len(ps) != 1 || !strings.Contains(ps[0].Title, "Dev Rao") {
		t.Fatalf("proposals: %+v", ps)
	}
}

func TestParseLinkedInPeople(t *testing.T) {
	if p, err := ParseLinkedInPeople(json.RawMessage(`{"linkedin_scan":{"minutes":5}}`)); p != nil || err != nil {
		t.Fatalf("not a people task: %v %v", p, err)
	}
	p, err := ParseLinkedInPeople(json.RawMessage(`{"linkedin_people":{}}`))
	if err != nil || p.Max != DefaultPeopleMax || len(p.Queries) != 0 {
		t.Fatalf("defaults: %+v %v", p, err)
	}
	for _, bad := range []string{
		`{"linkedin_people":{"queries":["a","b","c","d"]}}`,
		`{"linkedin_people":{"queries":["  "]}}`,
		`{"linkedin_people":{"max":101}}`,
		`{"linkedin_people":{"max":-1}}`,
		`{"linkedin_people":{"workspace":11}}`,
	} {
		if _, err := ParseLinkedInPeople(json.RawMessage(bad)); err == nil {
			t.Errorf("%s should be refused", bad)
		}
	}
}

func TestPeopleQueriesRotateDailyAndStayWithinTheLimit(t *testing.T) {
	p := &LinkedInPeople{}
	a, b := p.QueriesFor(searchDay), p.QueriesFor(searchDay.AddDate(0, 0, 1))
	if len(a) != MaxPeopleQueries || len(b) != MaxPeopleQueries || a[0] == b[0] {
		t.Fatalf("%v %v", a, b)
	}
	for _, q := range append(a, b...) {
		found := false
		for _, known := range AlumniQueries {
			found = found || q == known
		}
		if !found {
			t.Fatalf("unknown query %q", q)
		}
	}
	if own := (&LinkedInPeople{Queries: []string{"ACM VIT"}}).QueriesFor(searchDay); len(own) != 1 || own[0] != "ACM VIT" {
		t.Fatalf("own queries win: %v", own)
	}
}

func TestProfileURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://www.linkedin.com/in/riya-sen-1a2b3c/?miniProfileUrn=x":     "https://www.linkedin.com/in/riya-sen-1a2b3c/",
		"https://in.linkedin.com/in/dev-rao":                                "https://www.linkedin.com/in/dev-rao/",
		"https://www.linkedin.com/in/riya-sen-1a2b3c/overlay/contact-info/": "https://www.linkedin.com/in/riya-sen-1a2b3c/",
	} {
		if got, ok := ProfileURL(in); !ok || got != want {
			t.Errorf("%s: %q %v", in, got, ok)
		}
	}
	for _, bad := range []string{"", "https://www.linkedin.com/company/x/", "https://example.com/in/x/", "https://www.linkedin.com/in/", "/in/x", "https://www.linkedin.com/in/ACoAAB1cdEfGhIjKlMnOpQrStUvWxYz0123/", "https://www.linkedin.com/in/" + strings.Repeat("a", 32) + "/"} {
		if _, ok := ProfileURL(bad); ok {
			t.Errorf("%q is not a profile", bad)
		}
	}
}

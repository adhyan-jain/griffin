package domain

import "testing"

func TestEmployerNamesThatAreTheSameEmployerShareAKey(t *testing.T) {
	same := [][]string{
		{"Acme", "ACME", "acme", "  Acme  ", "Acme Pvt Ltd", "Acme Pvt. Ltd.", "Acme Private Limited", "Acme Technologies Pvt. Ltd.", "Acme Technologies", "Acme Inc", "Acme, Inc.", "Acme LLP", "The Acme Company"},
		{"Zoho Corporation", "Zoho Corp", "ZOHO"},
		{"Procter & Gamble", "Procter and Gamble", "Procter&Gamble Co."},
		{"Razorpay Software Private Limited", "Razorpay Software Pvt Ltd"},
	}
	for _, group := range same {
		want := NormalizeEmployer(group[0])
		if want == "" {
			t.Fatalf("%q normalizes to nothing", group[0])
		}
		for _, name := range group[1:] {
			if got := NormalizeEmployer(name); got != want {
				t.Errorf("%q -> %q, want %q (as %q)", name, got, want, group[0])
			}
		}
	}
}

func TestDifferentEmployersKeepDifferentKeys(t *testing.T) {
	pairs := [][2]string{{"Acme", "Acme Labs"}, {"Stripe", "Stripe Capital"}, {"Open AI", "OpenAI"}, {"Meta", "Metabase"}, {"Tata Consultancy Services", "Tata Motors"}}
	for _, p := range pairs {
		if NormalizeEmployer(p[0]) == NormalizeEmployer(p[1]) {
			t.Errorf("%q and %q are different employers", p[0], p[1])
		}
	}
	if NormalizeEmployer("Technologies Pvt Ltd") == "" || NormalizeEmployer("Inc") == "" || NormalizeEmployer("") != "" {
		t.Error("a name made only of suffix words keeps them; an empty name stays empty")
	}
}

func TestRoleTitlesThatAreTheSameRoleShareAKey(t *testing.T) {
	same := [][]string{
		{"Software Engineer Intern", "Software Engineering Intern", "Software Engineering Internship", "software  engineer   INTERN", "Software Engineer - Intern", "Intern, Software Engineer", "Software Engineer (Intern)", "Software Engineer Interns"},
		{"Backend Development Intern", "Backend Developer Intern", "Backend Development Internship"},
		{"Machine Learning Intern", "Machine-Learning Internship", "Intern - Machine Learning"},
		{"R&D Engineer Intern", "R and D Engineer Internship"},
	}
	for _, group := range same {
		want := NormalizeRole(group[0])
		for _, title := range group[1:] {
			if got := NormalizeRole(title); got != want {
				t.Errorf("%q -> %q, want %q (as %q)", title, got, want, group[0])
			}
		}
	}
}

func TestDifferentRolesKeepDifferentKeys(t *testing.T) {
	pairs := [][2]string{
		{"Software Engineer Intern", "Software Engineer"},                 // an internship is not the full-time role
		{"Backend Intern", "Frontend Intern"},                             //
		{"Software Developer Intern", "Software Engineer Intern"},         // reworded titles are not guessed to match
		{"Machine Learning Intern", "Machine Learning Research Intern"},   //
		{"Software Engineer Intern (Summer)", "Software Engineer Intern"}, // a qualifier is kept
	}
	for _, p := range pairs {
		if NormalizeRole(p[0]) == NormalizeRole(p[1]) {
			t.Errorf("%q and %q are different roles", p[0], p[1])
		}
	}
}

func TestPostingURLsCanonicalizeToOneForm(t *testing.T) {
	cases := map[string]string{
		"https://Boards.Greenhouse.io/Acme/jobs/123":                                   "https://boards.greenhouse.io/Acme/jobs/123",
		"https://boards.greenhouse.io/acme/jobs/123/":                                  "https://boards.greenhouse.io/acme/jobs/123",
		"https://boards.greenhouse.io/acme/jobs/123#app":                               "https://boards.greenhouse.io/acme/jobs/123",
		"https://boards.greenhouse.io/acme/jobs/123?gh_src=abc":                        "https://boards.greenhouse.io/acme/jobs/123",
		"https://boards.greenhouse.io/acme/jobs/123?gh_jid=123&gh_src=x&utm_source=li": "https://boards.greenhouse.io/acme/jobs/123?gh_jid=123",
		"https://jobs.lever.co/acme/abc-def?lever-source=LinkedIn&utm_campaign=z":      "https://jobs.lever.co/acme/abc-def",
		"https://jobs.lever.co/acme/abc-def?lever-origin=applied&ref=x&source=y":       "https://jobs.lever.co/acme/abc-def",
		"http://www.example.com/careers/42/?b=2&a=1":                                   "https://example.com/careers/42?a=1&b=2",
		"https://example.com:443/jobs":                                                 "https://example.com/jobs",
		"https://example.com:8443/jobs":                                                "https://example.com:8443/jobs",
		"https://example.com":                                                          "https://example.com",
		"https://example.com/":                                                         "https://example.com",
		"  https://example.com/jobs?utm_medium=email  ":                                "https://example.com/jobs",
		"https://example.com/jobs?UTM_Source=a":                                        "https://example.com/jobs",
		"https://example.com/search?q=go+intern&fbclid=1":                              "https://example.com/search?q=go+intern",
	}
	for in, want := range cases {
		if got := CanonicalURL(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
	for _, bad := range []string{"", "   ", "not a url", "ftp://example.com/x", "/relative/path", "mailto:a@b.co", "https://"} {
		if got := CanonicalURL(bad); got != "" {
			t.Errorf("%q must not canonicalize, got %q", bad, got)
		}
	}
	if CanonicalURL("https://example.com/Jobs/A") == CanonicalURL("https://example.com/jobs/a") {
		t.Error("paths are case-sensitive: only the host is lower-cased")
	}
}

func TestIdentityMatchesByEmployerAndRoleOrURLOrExternalID(t *testing.T) {
	lead := NewIdentity("internshala", "", "Acme Technologies Pvt. Ltd.", "Software Engineering Internship", "https://internshala.com/internship/detail/swe-at-acme-1?utm_source=a")
	ats := NewIdentity("greenhouse", "4001", "Acme", "Software Engineer Intern", "https://boards.greenhouse.io/acme/jobs/4001")
	if !lead.Same(ats) || !ats.Same(lead) {
		t.Error("the same employer and role from two sources is one opportunity")
	}
	other := NewIdentity("greenhouse", "4002", "Acme", "Data Engineer Intern", "https://boards.greenhouse.io/acme/jobs/4002")
	if ats.Same(other) {
		t.Error("another role at the same employer is another opportunity")
	}
	reworded := NewIdentity("lever", "x", "Acme", "Platform Apprentice", "https://boards.greenhouse.io/acme/jobs/4001/?gh_src=li")
	if !ats.Same(reworded) {
		t.Error("the same canonical URL is the same opportunity even when the title differs")
	}
	a := NewIdentity("greenhouse", "77", "A Co", "Role One", "")
	b := NewIdentity("greenhouse", "77", "B Co", "Role Two", "")
	c := NewIdentity("lever", "77", "B Co", "Role Two X", "")
	if !a.Same(b) {
		t.Error("the same external id from the same source is the same opportunity")
	}
	if a.Same(c) {
		t.Error("an external id means nothing across sources")
	}
	empty := NewIdentity("x", "", "", "", "")
	if empty.Same(NewIdentity("y", "", "", "", "")) {
		t.Error("two records with nothing to identify them are not the same")
	}
}

func TestADescriptionHashIgnoresSpacingAndStaysUnknownWhenEmpty(t *testing.T) {
	if DescriptionHash("") != "" || DescriptionHash(" \n\t ") != "" {
		t.Error("no description is no hash")
	}
	if DescriptionHash("Build  APIs\n in Go") != DescriptionHash("Build APIs in Go") {
		t.Error("spacing is not a change")
	}
	if DescriptionHash("Build APIs in Go") == DescriptionHash("Build APIs in Rust") {
		t.Error("different text hashes differently")
	}
}

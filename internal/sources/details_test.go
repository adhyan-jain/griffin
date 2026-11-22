package sources

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func mustContain(t *testing.T, got string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in:\n%s", w, got)
		}
	}
}

func TestInternshalaDetailOnsite(t *testing.T) {
	d, err := ParseInternshalaDetail(fixture(t, "internshala_detail_onsite.html"))
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, d.Description,
		"About the internship\nAre you a tech-savvy individual",
		"Key responsibilities:\n1. Collaborate with our development team",
		"Skill(s) required\n.NET, CSS, HTML, Java, JavaScript, MySQL, Node.js, PostgreSQL, React",
		"Who can apply\nOnly those candidates can apply who:\n1. are available for full time (in-office) internship",
		"4. are from Pune only",
		"Perks\nCertificate, Letter of recommendation, Flexible work hours, Informal dress code, 5 days a week",
		"Number of openings\n5",
		"Start date: Immediately",
		"Duration: 6 Months",
		"Stipend: ₹ 5,000 /month",
		"Apply by: 20 Dec' 26",
		"Posted: 2026-11-20",
		"Listed as: Internship",
		"Certificate: yes, listed under Perks (Certificate)",
	)
	if strings.Contains(d.Description, "Pre-placement") {
		t.Errorf("no PPO is mentioned on this page:\n%s", d.Description)
	}
	if strings.Contains(d.Description, "Learn CSS") || strings.Contains(d.Description, "<") {
		t.Errorf("training links and markup do not belong in the description:\n%s", d.Description)
	}
	if d.Deadline == nil || !d.Deadline.Equal(time.Date(2026, 12, 20, 23, 59, 59, 0, ist)) {
		t.Errorf("deadline %v", d.Deadline)
	}
}

func TestInternshalaDetailWorkFromHomeWithJobOffer(t *testing.T) {
	d, err := ParseInternshalaDetail(fixture(t, "internshala_detail_wfh_ppo.html"))
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, d.Description,
		"About the internship\nAs a software development intern at NayePankh Foundation",
		"Skill(s) required\n.NET, C++ Programming, CSS, Effective Communication",
		"Listed as: Job offer post internship, Part time",
		"Pre-placement offer (PPO): yes, the listing is labelled \"Job offer post internship\"",
		"Apply by: 19 Dec' 26",
	)
	if d.Deadline == nil || d.Deadline.Format("2006-01-02") != "2026-12-19" {
		t.Errorf("deadline %v", d.Deadline)
	}
}

func TestInternshalaDetailMissingSectionsKeepsTheRest(t *testing.T) {
	page := `<html><body><div class="internship_details">
	<h2 class="section_heading about_heading">About the internship</h2><div class="text-container">Build things.<br/>Ship them.</div>
	</div></body></html>`
	d, err := ParseInternshalaDetail([]byte(page))
	if err != nil {
		t.Fatal(err)
	}
	if d.Description != "About the internship\nBuild things.\nShip them." || d.Deadline != nil {
		t.Errorf("only what the page says: %q, deadline %v", d.Description, d.Deadline)
	}
}

func TestInternshalaDetailApplyByTextIsTheDeadlineWhenThereIsNoStructuredOne(t *testing.T) {
	page := `<html><body><div class="internship_details"><h2 class="about_heading">About</h2><div>Text</div></div>
	<div class="other_detail_item apply_by"><div class="item_heading"><span>APPLY BY</span></div><div class="item_body">5 Jan' 27</div></div></body></html>`
	d, err := ParseInternshalaDetail([]byte(page))
	if err != nil || d.Deadline == nil || !d.Deadline.Equal(time.Date(2027, 1, 5, 23, 59, 59, 0, ist)) {
		t.Fatalf("%v %v", d.Deadline, err)
	}
}

func TestInternshalaDetailMalformedPages(t *testing.T) {
	for name, page := range map[string]string{
		"empty":         "",
		"not html":      "{\"error\": true}",
		"no sections":   "<html><body><p>Page not found</p></body></html>",
		"broken markup": "<div class=\"internship_details\"><h2 class=\"about_heading\">About<div",
	} {
		d, err := ParseInternshalaDetail([]byte(page))
		if err == nil && strings.TrimSpace(d.Description) == "" {
			t.Errorf("%s: an empty description must be an error", name)
		}
		if err != nil && !errors.Is(err, ErrNoDetail) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestUnstopDetailUnpaid(t *testing.T) {
	d, err := ParseUnstopDetail(fixture(t, "unstop_detail_unpaid.json"))
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, d.Description,
		"Details\nTringflow Private Limited is hiring for Software Intern",
		"Responsibilities\n- Write, test, and maintain clear, reliable code",
		"This is an unpaid internship focused on practical learning and mentorship.",
		"Eligibility\nOpen to: fresher, students\nExperience: all\nFresher passout years: 2025, 2026\nStudent passout years: all",
		"Courses (engineering): allCourses, btech, mtech",
		"Registration closes: 24 Nov 2026",
		"Job details\nWork type: Work from home\nTiming: Full time\nDuration: 3 months\nOpenings: 1\nPay: Unpaid\nPerks: Certificate of Completion, Letter of Recommendation",
		"Skills\nDebugging, Software Testing Life Cycle (STLC), Teamwork and Collaboration, Python",
	)
	if strings.Contains(d.Description, "<p>") || strings.Contains(d.Description, "<li>") {
		t.Errorf("markup in the description:\n%s", d.Description)
	}
	if d.Deadline == nil || !d.Deadline.Equal(time.Date(2026, 11, 24, 0, 0, 0, 0, ist)) {
		t.Errorf("deadline %v", d.Deadline)
	}
}

func TestUnstopDetailPaidUsesTheOtherKeyNames(t *testing.T) {
	d, err := ParseUnstopDetail(fixture(t, "unstop_detail_paid.json"))
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, d.Description, "The Entrepreneurship Network is hiring for Software Internship", "Pay: ₹ 1,000 /month", "Work type: Work from home", "Timing: Part time", "Skills\nSoftware Development Life Cycle (SDLC)")
	if d.Deadline == nil || d.Deadline.Format("2006-01-02") != "2026-12-03" {
		t.Errorf("deadline %v", d.Deadline)
	}
}

func TestUnstopDetailMissingAndMalformed(t *testing.T) {
	d, err := ParseUnstopDetail([]byte(`{"data":{"competition":{"details":"<p>Only text.</p>"}}}`))
	if err != nil || d.Description != "Details\nOnly text." || d.Deadline != nil {
		t.Errorf("only what the page says: %q %v %v", d.Description, d.Deadline, err)
	}
	for name, body := range map[string]string{
		"empty":           "",
		"html 404":        "<!DOCTYPE html><title>Not Found</title>",
		"no data":         `{"data":{"status":false}}`,
		"no sections":     `{"data":{"competition":{"title":"x","details":""}}}`,
		"bad eligibility": `{"data":{"competition":{"details":"<p>a</p>","regnRequirements":{"eligibility":"{not json"}}}}`,
	} {
		d, err := ParseUnstopDetail([]byte(body))
		if name == "bad eligibility" {
			if err != nil || d.Description != "Details\na" {
				t.Errorf("%s: a bad eligibility is left out, not fatal: %q %v", name, d.Description, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s: want an error, got %q", name, d.Description)
		}
	}
}

func TestDetailURL(t *testing.T) {
	for _, c := range []struct{ source, link, want string }{
		{Internshala, "https://internshala.com/internship/detail/x-at-y123", "https://internshala.com/internship/detail/x-at-y123"},
		{Unstop, "https://unstop.com/internships/software-internship-tringflow-private-limited-1803844", "https://unstop.com/api/public/competition/1803844"},
		{Unstop, "https://unstop.com/internships/a-b-55?lb=x", "https://unstop.com/api/public/competition/55"},
		{Unstop, "https://unstop.com/o/sXVkzmW", ""},
		{Internshala, "https://internshala.com/internships/foo/", ""},
		{InternDock, "https://interndock.com/x-1", ""},
	} {
		got, ok := DetailURL(c.source, c.link)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("%s %s = %q %v", c.source, c.link, got, ok)
		}
	}
}

func TestFetchDetailAsksTheRightAddressAndReportsFailures(t *testing.T) {
	var asked string
	get := func(_ context.Context, u string) ([]byte, error) {
		asked = u
		return fixture(t, "unstop_detail_paid.json"), nil
	}
	d, err := FetchDetail(context.Background(), get, Unstop, "https://unstop.com/internships/a-b-1818776")
	if err != nil || d.Description == "" || asked != "https://unstop.com/api/public/competition/1818776" {
		t.Fatalf("%q %v %q", d.Description, err, asked)
	}
	if _, err := FetchDetail(context.Background(), func(context.Context, string) ([]byte, error) { return nil, errors.New("down") }, Internshala, "https://internshala.com/internship/detail/x1"); err == nil {
		t.Error("a failed read is an error")
	}
	if _, err := FetchDetail(context.Background(), get, Internshala, "https://example.com/x"); err == nil {
		t.Error("a link that is not a detail page is an error")
	}
}

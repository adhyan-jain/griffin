package linkedin

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/griffin/core/internal/domain"
)

func fixture(t *testing.T) json.RawMessage {
	t.Helper()
	b, err := os.ReadFile("testdata/people_page.json")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// peoplePageFake plays the laptop browser: it records the addresses opened and answers the reader script with
// whatever reply(address) says.
type peoplePageFake struct {
	opened []string
	reply  func(address string) json.RawMessage
}

func (f *peoplePageFake) Navigate(_ context.Context, address string) error {
	f.opened = append(f.opened, address)
	return nil
}

func (f *peoplePageFake) Eval(_ context.Context, script string) (json.RawMessage, error) {
	if script == peopleScript {
		return f.reply(f.opened[len(f.opened)-1]), nil
	}
	return json.RawMessage("null"), nil
}

func searchOf(f *peoplePageFake, reply func(string) json.RawMessage) PeopleSearch {
	f.reply = reply
	return PeopleSearch{
		Open:  func(context.Context, int) (Page, func(), error) { return f, func() {}, nil },
		Pause: func(context.Context) bool { return true },
		Now:   func() time.Time { return time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC) },
	}
}

func TestParsePeoplePageSortsEachCardsLines(t *testing.T) {
	address, _, hits, err := ParsePeoplePage(fixture(t))
	if err != nil || !strings.Contains(address, "/search/results/people/") || len(hits) != 6 {
		t.Fatalf("%s %d %v", address, len(hits), err)
	}
	want := []domain.PersonHit{
		{Name: "Riya Sen", Headline: "SDE-2 @ Razorpay | VIT '22 | ACM", Location: "Bengaluru, Karnataka, India", Snippet: "Current: SDE-2 at Razorpay"},
		{Name: "Dev Rao", Headline: "Software Engineer at Google", Location: "Hyderabad, Telangana, India", Snippet: "Past: Software Engineer Intern at Zoho\nVellore Institute of Technology"},
		{Name: "Tia Roy", Headline: "Technical Recruiter at Google | Hiring VIT grads", Location: "Bengaluru"},
		{Name: "Kim Lee", Headline: "Software Engineer", Location: "Chennai"},
	}
	for i, w := range want {
		g := hits[i]
		if g.Name != w.Name || g.Headline != w.Headline || g.Location != w.Location || g.Snippet != w.Snippet {
			t.Errorf("card %d: got %+v want %+v", i, g, w)
		}
	}
	if hits[0].URL == "" || hits[1].URL != "https://in.linkedin.com/in/dev-rao" {
		t.Errorf("urls: %q %q", hits[0].URL, hits[1].URL)
	}
	if _, _, _, err := ParsePeoplePage(json.RawMessage(`not json`)); err == nil {
		t.Error("garbage is an error")
	}
}

func TestPeopleSearchReturnsOnlyWorkingVITAlumniAsAnAlumniTable(t *testing.T) {
	f := &peoplePageFake{}
	answer, err := searchOf(f, func(string) json.RawMessage { return fixture(t) }).Run(context.Background(), &domain.LinkedInPeople{Max: 40})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.opened) != domain.MaxPeopleQueries*domain.MaxPeoplePages {
		t.Fatalf("a run is at most 3 queries x 2 pages, opened %d: %v", len(f.opened), f.opened)
	}
	for i, a := range f.opened {
		u, err := url.Parse(a)
		if err != nil || u.Path != "/search/results/people/" || u.Query().Get("origin") != "GLOBAL_SEARCH_HEADER" || u.Query().Get("keywords") == "" {
			t.Fatalf("address %q", a)
		}
		if wantPage := []string{"", "2"}[i%2]; u.Query().Get("page") != wantPage {
			t.Fatalf("address %d %q: page %q, want %q", i, a, u.Query().Get("page"), wantPage)
		}
	}
	table, ok := domain.ExtractTrackerTable(answer)
	if !ok {
		t.Fatalf("no ## Tracker table in:\n%s", answer)
	}
	lines := strings.Split(table, "\n")
	if lines[0]+"\n"+lines[1] != domain.AlumniHeader || len(lines) != 4 {
		t.Fatalf("table:\n%s", table)
	}
	if !strings.Contains(lines[2], "| Riya Sen | 2022 | Razorpay | SDE-2 | ACM VIT | https://www.linkedin.com/in/riya-sen-1a2b3c/ |  | not contacted | 2026-11-22 |") ||
		!strings.Contains(lines[3], "| Dev Rao |  | Google | Software Engineer |  | https://www.linkedin.com/in/dev-rao/ |") {
		t.Fatalf("rows:\n%s", table)
	}
	for _, not := range []string{"Tia Roy", "Kim Lee", "Stu Dent", "LinkedIn Member"} {
		if strings.Contains(table, not) {
			t.Errorf("%s must not be listed", not)
		}
	}
	// The same people on every page are listed once, and the list merge would drop them again.
	if _, n := domain.MergeAlumni(domain.AlumniHeader+"\n"+lines[2], table); n != 1 {
		t.Fatalf("merging into a list that has Riya adds only Dev: %d", n)
	}
}

func TestPeopleSearchUsesTheTasksOwnQueries(t *testing.T) {
	f := &peoplePageFake{}
	_, err := searchOf(f, func(string) json.RawMessage { return fixture(t) }).Run(context.Background(), &domain.LinkedInPeople{Max: 40, Queries: []string{"ACM VIT"}})
	if err != nil || len(f.opened) != 2 || !strings.Contains(f.opened[0], "keywords=ACM+VIT") {
		t.Fatalf("%v %v", f.opened, err)
	}
}

func TestPeopleSearchStopsAtTheFirstWall(t *testing.T) {
	for name, page := range map[string]string{
		"checkpoint":   `{"url":"https://www.linkedin.com/checkpoint/challenge/abc","text":"","cards":[]}`,
		"login wall":   `{"url":"https://www.linkedin.com/authwall?trk=x","text":"Sign in","cards":[]}`,
		"unusual":      `{"url":"https://www.linkedin.com/search/results/people/?keywords=x","text":"We noticed unusual activity from your account","cards":[]}`,
		"limit":        `{"url":"https://www.linkedin.com/search/results/people/?keywords=x","text":"You've reached the monthly limit for profile searches","cards":[]}`,
		"wall + cards": `{"url":"https://www.linkedin.com/checkpoint/lg/login","text":"","cards":[{"url":"https://www.linkedin.com/in/x","name":"X","lines":["X","SDE at Google | VIT"]}]}`,
	} {
		f := &peoplePageFake{}
		_, err := searchOf(f, func(string) json.RawMessage { return json.RawMessage(page) }).Run(context.Background(), &domain.LinkedInPeople{Max: 40})
		var n *NeedsYou
		if !errors.As(err, &n) || !strings.Contains(err.Error(), "needs you") {
			t.Errorf("%s: want a needs-you error, got %v", name, err)
		}
		if len(f.opened) != 1 {
			t.Errorf("%s: it must stop at once, opened %d pages", name, len(f.opened))
		}
	}
}

func TestPeopleSearchSaysSoWhenItReadsNobody(t *testing.T) {
	f := &peoplePageFake{}
	_, err := searchOf(f, func(string) json.RawMessage {
		return json.RawMessage(`{"url":"https://www.linkedin.com/search/results/people/?keywords=x","text":"No results found","cards":[]}`)
	}).Run(context.Background(), &domain.LinkedInPeople{Max: 40})
	var n *NeedsYou
	if err == nil || errors.As(err, &n) || !strings.Contains(err.Error(), "layout changed") {
		t.Fatalf("%v", err)
	}
	if len(f.opened) != domain.MaxPeopleQueries {
		t.Fatalf("an empty first page skips the second: opened %d", len(f.opened))
	}
}

func TestPeopleSearchStopsOnceItHasEnough(t *testing.T) {
	f := &peoplePageFake{}
	answer, err := searchOf(f, func(string) json.RawMessage { return fixture(t) }).Run(context.Background(), &domain.LinkedInPeople{Max: 2})
	if err != nil || len(f.opened) != 1 || strings.Count(answer, "not contacted") != 2 {
		t.Fatalf("opened %d: %v\n%s", len(f.opened), err, answer)
	}
}

func TestPeopleSearchWithNobodyQualifyingAddsNoTable(t *testing.T) {
	f := &peoplePageFake{}
	page := `{"url":"https://www.linkedin.com/search/results/people/?keywords=x","text":"","cards":[{"url":"https://www.linkedin.com/in/kim-lee-77","name":"Kim Lee","lines":["Kim Lee","Software Engineer","Chennai"]}]}`
	answer, err := searchOf(f, func(string) json.RawMessage { return json.RawMessage(page) }).Run(context.Background(), &domain.LinkedInPeople{Max: 40})
	if err != nil || strings.Contains(answer, domain.TrackerSection) || !strings.Contains(answer, "Nobody new") {
		t.Fatalf("%v\n%s", err, answer)
	}
}

func TestPeopleSearchPausesBetweenLoadsAndNeverClicks(t *testing.T) {
	f := &peoplePageFake{}
	pauses := 0
	s := searchOf(f, func(string) json.RawMessage { return fixture(t) })
	s.Pause = func(context.Context) bool { pauses++; return true }
	if _, err := s.Run(context.Background(), &domain.LinkedInPeople{Max: 40}); err != nil || pauses != 2*len(f.opened) {
		t.Fatalf("pauses=%d opened=%d err=%v", pauses, len(f.opened), err)
	}
	for _, banned := range []string{".click(", "submit(", "dispatchEvent", "fetch(", "XMLHttpRequest", "location.href =", "location.assign", "location.replace", "location ="} {
		if strings.Contains(peopleScript+scrollScript, banned) {
			t.Errorf("the reader must only read: found %q", banned)
		}
	}
}

func TestWallTextIsSpecific(t *testing.T) {
	if Wall("https://www.linkedin.com/search/results/people/?keywords=x", "") != "" {
		t.Fatal("a normal search page is not a wall")
	}
	if Wall("https://www.linkedin.com/uas/login", "") == "" {
		t.Fatal("a sign-in page is a wall")
	}
}

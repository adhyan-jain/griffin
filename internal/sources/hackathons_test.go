package sources

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

var hackNow = time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)

func TestDevpostHackathonsAreReadWithTheirRealDeadlines(t *testing.T) {
	hs, err := ParseDevpost(fixture(t, "devpost.json"), hackNow)
	if err != nil || len(hs) != 12 {
		t.Fatalf("twelve open hackathons in the saved page: %d %v", len(hs), err)
	}
	k := hs[0]
	if k.Name != "Kiroween 2026" || k.Organizer != "Kiro" || !k.Online || k.Prize != "$100,000" || k.Source != Devpost ||
		k.Link != "https://kiroween.devpost.com/" || k.Deadline.Format("2006-01-02") != "2026-12-04" || k.DaysLeft(hackNow) != 12 {
		t.Fatalf("first: %+v", k)
	}
	if !strings.Contains(strings.Join(k.Themes, ","), "Machine Learning/AI") {
		t.Fatalf("themes: %v", k.Themes)
	}
	// "Nov 11 - 30, 2026" takes its month from the start; "Nov 11, 2026 - Jan 11, 2027" ends next year; a $0 prize is no prize.
	if hs[1].Deadline.Format("2006-01-02") != "2026-11-30" || hs[6].Deadline.Format("2006-01-02") != "2027-01-11" {
		t.Fatalf("deadlines: %v %v", hs[1].Deadline, hs[6].Deadline)
	}
	if hs[9].Name != "Code for Humanity" || hs[9].Prize != "" {
		t.Fatalf("a $0 prize shows nothing: %+v", hs[9])
	}
}

func TestDevpostDeadlineFormats(t *testing.T) {
	for in, want := range map[string]string{
		"Oct 30 - Dec 04, 2026":       "2026-12-04",
		"Nov 11 - 30, 2026":           "2026-11-30",
		"Dec 31, 2026":                "2026-12-31",
		"Nov 18, 2026 - Feb 01, 2027": "2027-02-01",
	} {
		got, ok := devpostDeadline(in)
		if !ok || got.Format("2006-01-02") != want {
			t.Errorf("%q: %v %v want %s", in, got, ok, want)
		}
	}
	if _, ok := devpostDeadline("TBD"); ok {
		t.Error("an unreadable date gives no deadline")
	}
}

func TestDevpostSkipsInviteOnlyAndFinished(t *testing.T) {
	body := `{"hackathons":[
	 {"title":"Closed","url":"https://a.devpost.com/","open_state":"open","displayed_location":{"location":"Online"},"submission_period_dates":"Nov 01 - 20, 2026","themes":[]},
	 {"title":"Invite","url":"https://b.devpost.com/","open_state":"open","displayed_location":{"location":"Online"},"submission_period_dates":"Nov 01 - 30, 2026","themes":[],"invite_only":true},
	 {"title":"Hybrid","url":"https://c.devpost.com/","open_state":"upcoming","displayed_location":{"location":"Santa Clara Convention Center + Online"},"submission_period_dates":"Jan 25 - Feb 11, 2027","themes":[],"prize_amount":"$CAD <span data-currency-value>1,050</span>"}]}`
	hs, err := ParseDevpost([]byte(body), hackNow)
	if err != nil || len(hs) != 1 || hs[0].Name != "Hybrid" || !hs[0].Online || hs[0].Location == "" || hs[0].InIndia || hs[0].Prize != "$CAD 1,050" {
		t.Fatalf("%v %+v", err, hs)
	}
}

func TestUnstopHackathonsAreReadWithPlaceAndPrize(t *testing.T) {
	hs, err := ParseUnstopHackathons(fixture(t, "unstop_hackathons.json"), hackNow)
	if err != nil || len(hs) != 20 {
		t.Fatalf("%d %v", len(hs), err)
	}
	if hs[0].Name != "Claude Build Challenge 2026" || !hs[0].Online || hs[0].Source != UnstopH || hs[0].Organizer != "SAGE University Bhopal" ||
		!strings.HasPrefix(hs[0].Link, "https://unstop.com/hackathons/") || hs[0].Deadline.Format("2006-01-02") != "2026-12-25" || hs[0].Prize != "" {
		t.Fatalf("online: %+v", hs[0])
	}
	patna := hs[2]
	if patna.Name != "PromptWars x Quad Patna" || patna.Online || patna.Location != "Patna" || !patna.InIndia || patna.Prize != "₹20,000" || patna.Mode() != "In person, Patna" {
		t.Fatalf("in person: %+v", patna)
	}
	if hs[3].Mode() != "Online + Kolkata" {
		t.Fatalf("hybrid: %s", hs[3].Mode())
	}
}

func TestUnstopClosedAndPastAreSkipped(t *testing.T) {
	body := `{"data":{"data":[
	 {"title":"Shut","seo_url":"https://unstop.com/h/1","regn_open":0,"status":"LIVE","region":"online","end_date":"2026-12-25T23:59:00+05:30"},
	 {"title":"Past","seo_url":"https://unstop.com/h/2","regn_open":1,"status":"LIVE","region":"online","end_date":"2026-11-01T23:59:00+05:30"},
	 {"title":"Abroad","seo_url":"https://unstop.com/h/3","regn_open":1,"status":"LIVE","region":"offline","end_date":"2026-12-25T23:59:00+05:30","address_with_country_logo":{"city":"Austin","country":{"name":"United States"}}}]}}`
	hs, err := ParseUnstopHackathons([]byte(body), hackNow)
	if err != nil || len(hs) != 1 || hs[0].Name != "Abroad" || hs[0].InIndia {
		t.Fatalf("%v %+v", err, hs)
	}
}

func TestDevfolioHackathonsAreReadFromTheSearch(t *testing.T) {
	hs, err := ParseDevfolio(fixture(t, "devfolio.json"), hackNow)
	if err != nil || len(hs) != 8 {
		t.Fatalf("%d %v", len(hs), err)
	}
	var promptwaves, hft *Hackathon
	for i := range hs {
		switch hs[i].Name {
		case "promptwaves":
			promptwaves = &hs[i]
		case "Hack For Tomorrow 2026":
			hft = &hs[i]
		}
	}
	if promptwaves == nil || !promptwaves.Online || promptwaves.Link != "https://promptwaves.devfolio.co/" || promptwaves.Source != Devfolio ||
		promptwaves.Deadline.Format("2006-01-02") != "2026-12-08" {
		t.Fatalf("promptwaves: %+v", promptwaves)
	}
	if hft == nil || hft.Prize != "₹30,000" || hft.Online || hft.Location != "Ernakulam" || !hft.InIndia {
		t.Fatalf("hft: %+v", hft)
	}
	for _, h := range hs {
		if h.Name == "ETHChiangmai & Longevity Hackathon 2026" && h.InIndia {
			t.Errorf("a Thailand event is not in India: %+v", h)
		}
	}
}

func TestOnlyRelevantOpenHackathonsWithTimeLeftAreKept(t *testing.T) {
	day := 24 * time.Hour
	mk := func(name string, online, india bool, days int, themes ...string) Hackathon {
		return Hackathon{Name: name, Online: online, InIndia: india, Themes: themes, Deadline: hackNow.Add(time.Duration(days) * day), Source: Devpost, Link: "https://x/" + name}
	}
	got := FilterHackathons([]Hackathon{
		mk("Agent Build", true, false, 20),
		mk("LLM Sprint", true, false, 6),
		mk("Too Soon AI", true, false, 2, "Machine Learning/AI"),
		mk("Past AI", true, false, -1, "Machine Learning/AI"),
		mk("Berlin Backend Jam", false, false, 30, "DevOps"),
		mk("Pune Backend Jam", false, true, 30, "DevOps"),
		mk("Music Remix", true, false, 30, "Music/Art", "Beginner Friendly"),
		mk("AI Quiz Night", true, false, 30, "Machine Learning/AI"),
		{Name: "Agent Build", Online: true, Deadline: hackNow.Add(20 * day), Link: "https://x/Agent Build/"},
	}, hackNow)
	var names []string
	for _, h := range got {
		names = append(names, h.Name)
	}
	if strings.Join(names, ",") != "LLM Sprint,Agent Build,Pune Backend Jam" {
		t.Fatalf("soonest deadline first, only online or Indian, relevant, 3+ days, no quiz, no repeats: %v", names)
	}
}

func TestTheHackathonListIsCapped(t *testing.T) {
	var hs []Hackathon
	for i := 0; i < MaxHackathonRows+15; i++ {
		hs = append(hs, Hackathon{Name: "AI Jam " + strconv.Itoa(i), Online: true, Deadline: hackNow.Add(time.Duration(5+i) * 24 * time.Hour), Link: "https://x/" + strconv.Itoa(i)})
	}
	got := FilterHackathons(hs, hackNow)
	if len(got) != MaxHackathonRows || got[0].Name != "AI Jam 0" {
		t.Fatalf("%d rows, first %s", len(got), got[0].Name)
	}
}

func TestHackathonsListIsATableWithDaysLeft(t *testing.T) {
	hs, _ := ParseDevpost(fixture(t, "devpost.json"), hackNow)
	text := HackathonsListText(FilterHackathons(hs, hackNow), hackNow)
	if !strings.Contains(text, "| Name | Organizer | Mode & Location | Prize | Themes | Deadline | Days left | Source | Link |") ||
		!strings.Contains(text, "| Kiroween 2026 | Kiro | Online | $100,000 |") || !strings.Contains(text, "| 2026-12-04 | 12 | Devpost | https://kiroween.devpost.com/ |") {
		t.Fatalf("\n%s", text)
	}
}

func TestReadHackathonsNamesTheSiteThatFailedAndKeepsTheRest(t *testing.T) {
	get := func(_ context.Context, u string) ([]byte, error) {
		if strings.Contains(u, "devpost") {
			return nil, errors.New("offline")
		}
		return fixture(t, "unstop_hackathons.json"), nil
	}
	post := func(context.Context, string, []byte) ([]byte, error) { return fixture(t, "devfolio.json"), nil }
	hs, problems := ReadHackathons(context.Background(), get, post, hackNow)
	if len(hs) != 8+4*20 || len(problems) != devpostPages || problems[0] != "Devpost page 1" {
		t.Fatalf("%d rows, problems %v", len(hs), problems)
	}
}

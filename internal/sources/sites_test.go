package sources

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/griffin/core/internal/domain"
)

func TestInternshalaCardsAreReadFromTheRealPage(t *testing.T) {
	page, err := os.ReadFile("testdata/internshala.html")
	if err != nil {
		t.Fatal(err)
	}
	ps := ParseInternshala(string(page))
	if len(ps) != 4 {
		t.Fatalf("four cards in the saved page, got %d", len(ps))
	}
	first := ps[0]
	if first.Title != "Software Development Intern" || first.Company == "" || !strings.HasPrefix(first.Link, "https://internshala.com/internship/detail/") ||
		first.Pay != "₹ 5,000 /month" || !strings.Contains(first.Location, "Pune") || first.Source != Internshala {
		t.Fatalf("first card: %+v", first)
	}
	if ps[1].Location != "Remote (India)" || ps[1].Pay != "Unpaid" {
		t.Fatalf("work from home and unpaid are read as such: %+v", ps[1])
	}
	for _, p := range ps {
		if Fits(p) {
			t.Errorf("unpaid and ₹2-5k posts are not shown: %+v", p)
		}
	}
	rich := strings.Replace(string(page), "₹ 5,000 /month", "₹ 25,000 - 45,000 /month", 1)
	if p := ParseInternshala(rich)[0]; !Fits(p) || Tier(p.Pay) != "A" {
		t.Fatalf("a well-paid software internship in Pune fits and is tier A: %+v", p)
	}
}

func TestUnstopResultsAreReadWithTheirStipend(t *testing.T) {
	body, err := os.ReadFile("testdata/unstop.json")
	if err != nil {
		t.Fatal(err)
	}
	ps, err := ParseUnstop(body, time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	byCo := map[string]domain.Posting{}
	for _, p := range ps {
		byCo[p.Company] = p
	}
	coy, ok := byCo["Coy"]
	if !ok || coy.Pay != "₹ 45,000 /month" || !strings.Contains(coy.Location, "Gurgaon") || !strings.HasPrefix(coy.Link, "https://unstop.com/") {
		t.Fatalf("Coy: %+v", coy)
	}
	if !Fits(coy) || Tier(coy.Pay) != "A" {
		t.Fatalf("a ₹45k software internship in Gurgaon is a tier A fit: %+v", coy)
	}
	var fit []string
	for _, p := range ps {
		if Fits(p) {
			fit = append(fit, p.Company+": "+p.Title+" "+p.Pay)
		}
	}
	for _, f := range fit {
		if strings.Contains(f, "Analyst") || strings.Contains(f, "Testing") {
			t.Errorf("analyst and testing roles never fit: %s", f)
		}
	}
	if _, err := ParseUnstop(body, time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if later, _ := ParseUnstop(body, time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC)); len(later) != 0 {
		t.Fatalf("past their deadline, none are listed: %d", len(later))
	}
}

func TestStipendTiers(t *testing.T) {
	for pay, want := range map[string]int{"₹ 15,000 - 30,000 /month": 30000, "₹ 45,000 /month": 45000, "Unpaid": 0, "₹ 50,000 lump sum": 0, "": 0} {
		if got := MonthlyStipend(pay); got != want {
			t.Errorf("%q: %d want %d", pay, got, want)
		}
	}
	if Tier("₹ 40,000 /month") != "A" || Tier("₹ 15,000 - 40,000 /month") != "A" || Tier("₹ 4,000 - 40,000 /month") != "B" || Tier("₹ 20,000 /month") != "B" || Tier("₹ 12,000 /month") != "C" {
		t.Error("tiers")
	}
}

func TestRemoteRolesTiedToAnotherCountryDoNotCount(t *testing.T) {
	for loc, want := range map[string]bool{
		"Bengaluru, India": true, "Remote (India)": true, "Gurugram": true, "Remote (Anywhere)": true,
		"New York, New York (Remote)": false, "Belgrade (Remote)": false, "Remote - US": false, "": false,
	} {
		p := domain.Posting{Title: "Software Engineer Intern", Location: loc, Pay: "₹ 50,000 /month"}
		if got := Fits(p); got != want {
			t.Errorf("%q: got %v, want %v", loc, got, want)
		}
	}
}

func TestOneBrokenSiteDoesNotStopTheOthers(t *testing.T) {
	unstop, _ := os.ReadFile("testdata/unstop.json")
	posts, problems := Read(context.Background(), func(_ context.Context, u string) ([]byte, error) {
		if strings.Contains(u, "unstop.com") {
			return unstop, nil
		}
		return nil, errors.New("down")
	})
	if len(posts) == 0 || len(problems) != len(InternshalaPages)+len(domain.InternDockPages) {
		t.Fatalf("posts %d, problems %v", len(posts), problems)
	}
}

func TestRolesOutsideBackendAIAndSDEAreLeftOut(t *testing.T) {
	for _, title := range []string{"Generative AI Trainer Intern", "Subject Matter Expert (SME) - AI/ML Intern", "Angular Developer Intern", "Front End Development Intern", "Godot Game Developer Intern", "Software Testing Internship"} {
		if Fits(domain.Posting{Title: title, Location: "Remote (India)", Pay: "₹ 30,000 /month"}) {
			t.Errorf("%q is not a backend, AI or SDE role", title)
		}
	}
	for _, title := range []string{"Backend Development Intern", "Machine Learning Intern", "Software Engineer Intern", "Full Stack Development Intern", "AI Agent Development Intern"} {
		if !Fits(domain.Posting{Title: title, Location: "Remote (India)", Pay: "₹ 30,000 /month"}) {
			t.Errorf("%q fits", title)
		}
	}
}

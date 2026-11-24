package domain

import (
	"strings"
	"testing"
)

// A card as InternDock serves it today (2026-11-22): a logo placeholder with the company's initials comes first, so
// the title, company and place are the second, third and fourth text nodes. The parser used to read the placeholder
// as the title ("H"), the title as the company and the company as the place.
const logoCard = `<ul><li><a href="/internships/hp/software-product-security-engineer-intern-af30ac89-8643-4da7-8f74-d7cca27ac22e" class="card"><div class="logo"><span aria-hidden="true">H</span></div><div><p>Software Product Security Engineer Intern</p><p><span>HP</span><span><svg><path d="M0 0"></path></svg>Spring, Texas, United States</span><span>$35–$40 per hour</span></p></div></a></li>` +
	`<li><a href="/internships/lockheed-martin/ai-research-engineer-intern-11111111-2222-3333-4444-555555555555" class="card"><div class="logo"><span>LM</span></div><div><p>AI Research Engineer Intern</p><p><span>Lockheed Martin</span><span>Remote, United States</span></p></div></a></li></ul>`

func TestParseInternDockSkipsTheLogoPlaceholder(t *testing.T) {
	got := ParseInternDock(logoCard)
	if len(got) != 2 {
		t.Fatalf("want 2 postings, got %+v", got)
	}
	if p := got[0]; p.Title != "Software Product Security Engineer Intern" || p.Company != "HP" || p.Location != "Spring, Texas, United States" || p.Pay != "$35–$40 per hour" {
		t.Errorf("logo card read wrongly: %+v", p)
	}
	if p := got[1]; p.Title != "AI Research Engineer Intern" || p.Company != "Lockheed Martin" {
		t.Errorf("initials of two words: %+v", p)
	}
}

func TestParseInternDockCardWithoutLogoIsUnchanged(t *testing.T) {
	card := `<a href="/internships/acme/software-engineer-intern-abcdefghijklmnopqrstuvwx" class="c"><h3>AI</h3><span>Acme Corp</span><span>Bengaluru, India</span><span>Posted Nov 20, 2026</span></a>`
	got := ParseInternDock(card)
	if len(got) != 1 || got[0].Title != "AI" || got[0].Company != "Acme Corp" || got[0].Location != "Bengaluru, India" {
		t.Fatalf("%+v", got)
	}
}

func TestFitsOwnerRoleFilter(t *testing.T) {
	for title, want := range map[string]bool{
		"Frontend Engineering Intern":      true,
		"Full Stack Developer Intern":      true,
		"UI Engineer Intern (React)":       true,
		"Angular Developer Intern":         true,
		"Systems Design Engineer Intern":   true,
		"Contest Platform Engineer Intern": true,
		"QA Tester Intern":                 false,
		"Software Test Intern":             false,
		"Graphic Design Intern":            false,
		"UI/UX Designer Intern":            false,
		"Product Design Software Intern":   false,
	} {
		if got := FitsOwner(Posting{Title: title, Location: "Remote, Worldwide"}); got != want {
			t.Errorf("%q: FitsOwner = %v, want %v", title, got, want)
		}
	}
}

func TestInIndiaOrWorldwideNeedsEvidence(t *testing.T) {
	for loc, want := range map[string]bool{
		"Bengaluru, India": true, "Remote - Worldwide": true, "Remote (anywhere)": true,
		"Remote - APAC": false, "Remote, Asia": false, "Remote": false, "Remote - US": false,
	} {
		if got := InIndiaOrWorldwide(loc); got != want {
			t.Errorf("%q: %v, want %v", loc, got, want)
		}
	}
}

func TestListingRowTierFollowsPay(t *testing.T) {
	h := strings.Split(InternshipsHeader, "\n")[0]
	for pay, want := range map[string]string{"₹ 45,000 /month": "| Tier A |", "₹ 20,000 /month": "| Tier B |", "": "| Tier C |", "Unpaid": "| Tier C |"} {
		if row := ListingRow(h, Posting{Title: "Software Intern", Company: "X", Pay: pay}); !strings.HasPrefix(row, want) {
			t.Errorf("pay %q: %s, want prefix %s", pay, row, want)
		}
	}
}

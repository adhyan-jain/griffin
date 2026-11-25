package domain

import "testing"

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

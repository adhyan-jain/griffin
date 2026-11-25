package sources

import (
	"testing"

	"github.com/griffin/core/internal/domain"
)

// Real Internshala and Unstop titles (captured 2026-11-22 for the opportunity benchmark) that name the language and
// "Development", never "developer" or "software": the owner writes Python and Node, and these were dropped.
func TestInternshipsNamedByLanguageAreKept(t *testing.T) {
	for _, title := range []string{"Node.js Development Intern", "Python Development Intern", "JavaScript Development Intern", "Django Development Intern", "Java Development Intern", "Golang Development Intern"} {
		if !Fits(domain.Posting{Title: title, Location: "Remote (India)", Pay: "₹ 20,000 /month"}) {
			t.Errorf("%q is a backend internship in India and fits", title)
		}
	}
	for _, title := range []string{"Business Development Intern", "Sales Development Intern", "Learning Development Intern"} {
		if Fits(domain.Posting{Title: title, Location: "Remote (India)", Pay: "₹ 20,000 /month"}) {
			t.Errorf("%q is not an engineering role", title)
		}
	}
}

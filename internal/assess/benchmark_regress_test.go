package assess

import (
	"strings"
	"testing"

	"github.com/griffin/core/internal/domain"
)

// Regressions found by the opportunity benchmark (internal/eval) on real postings captured on 2026-11-22.

// A season or month before the later year is part of the list. Databricks, Scale AI, Stripe and Snowflake write their
// intern years this way, and the owner (2028) was told the posting "is for other graduating years".
func TestGraduationYearListsMayNameTheSeason(t *testing.T) {
	cases := []struct{ text, want string }{
		{"You will graduate between fall 2027 and summer 2028 with a degree in Computer Science, Engineering, or related field.", domain.GradFit},
		{"Requirements: A graduation date in Fall 2027 or Spring 2028 with a Bachelor's degree (or equivalent) in a relevant field.", domain.GradFit},
		{"Enrolled in a degree program with the expectation of graduating in December 2027 or spring/summer 2028.", domain.GradFit},
		{"You will graduate between Autumn 2027 and Summer 2028 with a degree in Computer Science.", domain.GradFit},
		{"Pursuing a degree graduating between Fall 2026 and Spring 2027.", domain.GradMismatch},
		{"A graduation date in Fall 2026 or Spring 2027 with a Bachelor's degree.", domain.GradMismatch},
	}
	for _, c := range cases {
		if got := AssessGrad(c.text).Fit; got != c.want {
			t.Errorf("%q: got %s, want %s", c.text, got, c.want)
		}
	}
}

// The lower-case verb "be" is not a bachelor's degree: it used to switch off the postgraduate-only check in nearly
// every posting (a PhD-only Figma intern was not blocked).
func TestPostgraduateOnlyIsBlockedEvenWhenTheTextSaysBe(t *testing.T) {
	phd := "This internship is open to students in a PhD program: enrollment as a 4th or 5th year senior PhD student pursuing a PhD in Computer Graphics. You will be working with the graphics team and be mentored."
	if r := AssessGrad(phd); len(r.Blockers) == 0 {
		t.Errorf("a PhD-only internship must block an undergraduate: %+v", r)
	}
	for _, ok := range []string{
		"Currently enrolled in a BS, MS or PhD program. You will be working with the team.",
		"Pursuing a master's or a B.E. degree. You will be paired with a mentor.",
		"Pursuing a PhD, or a bachelor's degree, and able to be on site.",
	} {
		if r := AssessGrad(ok); len(r.Blockers) != 0 {
			t.Errorf("%q welcomes undergraduates: %+v", ok, r.Blockers)
		}
	}
}

// Airbnb's US internship says "Authorization to work in the United States is required." (a noun phrase, not "must be
// authorized to work"), and the pattern list missed it: the posting was ranked first with eligibility unknown.
func TestAuthorisationToWorkIsRequiredIsABlocker(t *testing.T) {
	r := AssessLocation("San Francisco, California", "You will build things. - Authorization to work in the United States is required. Please note: This role is not eligible for visa or immigration sponsorship. Domestic relocation support will be provided.")
	if len(r.Blockers) == 0 {
		t.Fatalf("required US work authorisation must block: %+v", r)
	}
	if got := AssessLocation("San Francisco, California", "Authorization to work in the United States is a plus."); len(got.Blockers) != 0 {
		t.Errorf("a preference is not a blocker: %+v", got.Blockers)
	}
}

// A cap that lowers the class lowers the score, because the page ranks by score.
func TestCappedListingsDoNotOutrankListingsTheOwnerCanTake(t *testing.T) {
	india := run(Input{Title: "Backend Engineer Intern", Company: "Acme", Location: "Bengaluru", Description: strongPosting})
	abroad := run(Input{Title: "Backend Engineer Intern", Company: "Acme", Location: "New York, NY", Description: strongPosting})
	barred := run(Input{Title: "Backend Engineer Intern", Company: "Acme", Location: "Bengaluru", Description: strings.Replace(strongPosting, "graduating in 2027 or 2028", "graduating in 2026", 1)})
	if abroad.ValueClass != domain.ValueExploratory || abroad.ValueScore > 49 {
		t.Errorf("abroad without visa support: %d %s", abroad.ValueScore, abroad.ValueClass)
	}
	if barred.Eligibility != domain.EligibilityIneligible || barred.ValueClass != domain.ValueLow || barred.ValueScore > 34 {
		t.Errorf("ineligible: %d %s %s", barred.ValueScore, barred.ValueClass, barred.Eligibility)
	}
	if india.ValueScore <= abroad.ValueScore || india.ValueScore <= barred.ValueScore {
		t.Errorf("the role the owner can take (%d) must outrank the capped ones (%d, %d)", india.ValueScore, abroad.ValueScore, barred.ValueScore)
	}
}

package assess

import (
	"strings"
	"testing"

	"github.com/griffin/core/internal/domain"
)

// The place and eligibility reading of a role, one case per kind of location the owner meets. The description always
// carries enough engineering text and the owner's graduating year, so the place is the only thing in doubt.
func TestRemoteAndPlaceEligibilityTable(t *testing.T) {
	base := strings.Repeat("You will build backend services and APIs with Postgres and Go. ", 6) + "Open to students graduating in 2028. "
	cases := []struct {
		name, loc, extra string
		fit              string
		eligible         bool // confirmed or likely
		ineligible       bool // a quoted blocker
	}{
		{"1 remote worldwide", "Remote - Worldwide", "", domain.LocationRemoteOK, true, false},
		{"2 remote accepting India", "Remote", "This role is remote and open to candidates in India.", domain.LocationRemoteOK, true, false},
		{"3 remote US-only (label)", "Remote - United States", "", domain.LocationOutside, false, false},
		{"3b remote US-only (text)", "Remote", "This is a remote role. US only.", domain.LocationUnknown, false, true},
		{"4 remote EU-only (label)", "Remote - EU", "", domain.LocationOutside, false, false},
		{"4b remote EU-only (text)", "Remote", "EU only. Applicants must be authorized to work in the European Union.", domain.LocationUnknown, false, true},
		{"5 remote with ambiguous wording", "Remote", "Remote-first team. Applicants from selected regions are considered.", domain.LocationUnknown, false, false},
		{"6 remote with no eligibility stated", "Remote", "", domain.LocationUnknown, false, false},
		{"7 Indian remote", "Remote (India)", "", domain.LocationIndia, true, false},
		{"8 Indian on-site", "Pune, India", "", domain.LocationIndia, true, false},
		// Existing rule: a foreign on-site role whose posting offers relocation or visa support is "relocation"; the
		// posting does not say the owner may apply, so it is never eligible on that alone.
		{"9 foreign on-site with relocation", "Berlin", "We offer relocation assistance.", domain.LocationRelocation, false, false},
		{"10 foreign on-site without relocation", "Berlin", "", domain.LocationOutside, false, false},
		{"APAC is evidence-free", "APAC", "", domain.LocationUnknown, false, false},
		{"remote APAC", "Remote - APAC", "", domain.LocationUnknown, false, false},
		{"remote Asia", "Remote, Asia", "", domain.LocationUnknown, false, false},
		{"Hybrid names no place", "Hybrid", "", domain.LocationUnknown, false, false},
		{"Multiple locations names no place", "Multiple locations", "", domain.LocationUnknown, false, false},
		{"APAC with a named foreign country stays outside", "Singapore, APAC", "", domain.LocationOutside, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := run(Input{Title: "Backend Engineer Intern", Company: "Acme Cloud", Location: c.loc, Description: base + c.extra})
			if a.LocationFit != c.fit {
				t.Errorf("location fit %q, want %q", a.LocationFit, c.fit)
			}
			eligible := a.Eligibility == domain.EligibilityConfirmed || a.Eligibility == domain.EligibilityLikely
			if eligible != c.eligible {
				t.Errorf("eligibility %q (eligible=%v), want eligible=%v", a.Eligibility, eligible, c.eligible)
			}
			if (a.Eligibility == domain.EligibilityIneligible) != c.ineligible {
				t.Errorf("eligibility %q, want ineligible=%v", a.Eligibility, c.ineligible)
			}
		})
	}
}

func TestCompensationTable(t *testing.T) {
	cases := []struct {
		name, pay, desc, loc string
		want                 float64 // monthly rupees; -1 unknown
		annual               bool
	}{
		{"rupees per month", "₹15,000/month", "", "", 15000, false},
		{"dollars per month", "$5,000/month", "", "", 5000 * UsdToInr, false},
		{"annual dollar salary is flagged, not a stipend", "$120,000 per year", "", "", 120000 * UsdToInr / 12, true},
		{"annual salary in the description", "", "The salary is $120,000 per year.", "", 120000 * UsdToInr / 12, true},
		{"euros per month", "€2,000/month", "", "", 2000 * EurToInr, false},
		{"pounds per month", "£1,500 per month", "", "", 1500 * GbpToInr, false},
		{"EUR code", "EUR 2,000 per month", "", "", 2000 * EurToInr, false},
		{"CA$ is not US dollars", "CA$4,000/month", "", "", -1, false},
		{"C$ is not US dollars", "C$4,000/month", "", "", -1, false},
		{"bare dollar in Toronto text", "$4,000/month, Toronto", "", "", -1, false},
		{"bare dollar with a Canadian location", "$4,000/month", "", "Toronto, Canada", -1, false},
		{"bare dollar with an Australian location", "$4,000/month", "", "Sydney, Australia", -1, false},
		{"explicit US$ in Canada stays dollars", "US$4,000/month", "", "Toronto, Canada", 4000 * UsdToInr, false},
		{"no period", "₹15,000", "", "", -1, false},
		{"no period in text", "", "Stipend of ₹10,000.", "", -1, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseCompensationAt(c.pay, "", c.desc, c.loc)
			if c.want < 0 {
				if got != nil {
					t.Fatalf("want unknown, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("want %v, got unknown", c.want)
			}
			if d := got.MonthlyINR - c.want; d > 1 || d < -1 {
				t.Errorf("monthly %v, want %v", got.MonthlyINR, c.want)
			}
			if got.Annual != c.annual {
				t.Errorf("annual %v, want %v", got.Annual, c.annual)
			}
		})
	}
}

// An annual salary never becomes a stored monthly stipend, and scores neutral with the conversion named.
func TestAnnualSalaryIsNotStoredAsAStipend(t *testing.T) {
	a := run(Input{Title: "Backend Engineer Intern", Company: "Acme", Location: "Pune, India", Pay: "$120,000 per year"})
	if a.CompensationMonthlyINR != nil {
		t.Fatalf("an annual figure is not a monthly stipend: %v", *a.CompensationMonthlyINR)
	}
	c := comp(a, CompCompensation)
	if c.Points != 7.5 || !strings.Contains(c.Reason, "divided by 12") {
		t.Fatalf("want a neutral, labelled component, got %+v", c)
	}
}

package domain

import (
	"strings"
	"testing"
	"time"
)

const personList = AlumniHeader + "\n" +
	"| Sara Iyer | 2021 | Google | ML Engineer | ACM VIT | https://www.linkedin.com/in/sara | x | not contacted | 2026-11-20 |\n" +
	"| Sara Iyer | | Google | Operations Manager | | https://www.linkedin.com/in/sara-iyer-2 | x | not contacted | 2026-11-20 |\n" +
	"| Dev Rao | 2023 | Razorpay | Backend Engineer | | https://www.linkedin.com/in/dev-rao | x | opted out 2026-11-21 | 2026-11-20 |\n" +
	"| No Link | 2020 | Acme | SDE | | | x | not contacted | 2026-11-20 |"

func TestAlumniImportRowsKeepTwoPeopleWithOneNameApart(t *testing.T) {
	rows := AlumniImportRows(personList)
	if len(rows) != 3 {
		t.Fatalf("a row with no profile link cannot be imported: %d rows", len(rows))
	}
	if rows[0].Name != rows[1].Name || rows[0].Company != rows[1].Company || rows[0].ProfileURL == rows[1].ProfileURL {
		t.Fatalf("same name and company, different profiles stay two rows: %+v %+v", rows[0], rows[1])
	}
	if rows[0].GraduationYear != 2021 || rows[1].GraduationYear != 0 {
		t.Fatalf("a year only when the row has one: %d %d", rows[0].GraduationYear, rows[1].GraduationYear)
	}
	if !rows[2].DoNotContact || rows[2].ContactState != ContactOptedOut {
		t.Fatalf("an opted-out status is do-not-contact: %+v", rows[2])
	}
	if rows[0].DoNotContact {
		t.Fatal("a normal row is not do-not-contact")
	}
}

func TestAlumniStatusState(t *testing.T) {
	for in, want := range map[string]struct {
		s   ContactState
		dnc bool
	}{
		"not contacted": {ContactNotContacted, false}, "": {ContactNotContacted, false},
		"sent 2026-11-20": {ContactSentManually, false}, "requested 2026-11-20": {ContactSentManually, false},
		"replied": {ContactReplied, false}, "Do not contact": {ContactOptedOut, true},
		"opted out 2026-11-21": {ContactOptedOut, true}, "DNC": {ContactOptedOut, true},
	} {
		s, dnc := AlumniStatusState(in)
		if s != want.s || dnc != want.dnc {
			t.Errorf("%q: got %s %v", in, s, dnc)
		}
	}
}

func TestGraduationEligibilityTable(t *testing.T) {
	y := func(v int) *int { return &v }
	d := func(v string) *time.Time {
		x, err := time.Parse("2006-01-02", v)
		if err != nil {
			t.Fatal(err)
		}
		return &x
	}
	for name, tc := range map[string]struct {
		year *int
		date *time.Time
		want string
	}{
		"exact on the cutoff day":   {nil, d("2022-11-26"), CohortEligible},
		"exact the day after":       {nil, d("2022-11-27"), CohortIneligible},
		"year only 2022":            {y(2022), nil, CohortBorderline2022},
		"year 2021":                 {y(2021), nil, CohortEligible},
		"year 2023":                 {y(2023), nil, CohortIneligible},
		"nil":                       {nil, nil, CohortUnknown},
		"zero":                      {y(0), nil, CohortUnknown},
		"exact date beats the year": {y(2023), d("2022-05-01"), CohortEligible},
	} {
		if got := EvaluateGraduationEligibility(tc.year, tc.date, DefaultCutoffDate); got != tc.want {
			t.Errorf("%s: got %q want %q", name, got, tc.want)
		}
	}
	if !GraduationCutoffDate(2022).Equal(DefaultCutoffDate) || !GraduationCutoffDate(0).Equal(DefaultCutoffDate) {
		t.Error("the 2022 cutoff is the default cutoff date")
	}
}

func TestClassifyVITCampus(t *testing.T) {
	for in, want := range map[string][2]string{
		"Vellore Institute of Technology, Chennai": {CampusChennai, AffiliationOtherCampus},
		"VIT Chennai":       {CampusChennai, AffiliationOtherCampus},
		"VIT-AP University": {CampusAP, AffiliationOtherCampus},
		"VIT AP":            {CampusAP, AffiliationOtherCampus},
		"VIT Amaravati":     {CampusAP, AffiliationOtherCampus},
		"VIT Bhopal":        {CampusBhopal, AffiliationOtherCampus},
		"VIT Vellore":       {CampusVellore, AffiliationConfirmedVellore},
		"Vellore Institute of Technology, Vellore": {CampusVellore, AffiliationConfirmedVellore},
		"Vellore Institute of Technology":          {CampusUnknown, AffiliationUnconfirmed},
		"VIT":                                      {CampusUnknown, AffiliationUnconfirmed},
		"ACM VIT":                                  {CampusUnknown, AffiliationUnconfirmed},
		"Japan":                                    {CampusUnknown, AffiliationUnconfirmed},
		"Vellore, Tamil Nadu":                      {CampusUnknown, AffiliationUnconfirmed},
		"":                                         {CampusUnknown, AffiliationUnconfirmed},
	} {
		if c, a := ClassifyVITCampus(in); c != want[0] || a != want[1] {
			t.Errorf("%q: got %s/%s want %s/%s", in, c, a, want[0], want[1])
		}
	}
}

func TestResolveACMAffiliation(t *testing.T) {
	for name, tc := range map[string]struct{ claimed, source, excerpt, want string }{
		"confirmed with source and excerpt": {"confirmed", "https://acmvit.in/team", "Core member 2020", "confirmed"},
		"confirmed without a source":        {"confirmed", "", "Core member", "likely"},
		"confirmed without an excerpt":      {"confirmed", "https://acmvit.in/team", " ", "likely"},
		"confirmed with a non-URL source":   {"confirmed", "tracker", "Core member", "likely"},
		"confirmed with an ftp source":      {"confirmed", "ftp://x/y", "Core member", "likely"},
		"club text mentioning ACM":          {"ACM VIT", "", "", "likely"},
		"likely stays likely":               {"likely", "", "", "likely"},
		"nothing":                           {"", "https://x.io", "e", "unknown"},
		"unknown":                           {"unknown", "https://x.io", "e", "unknown"},
	} {
		if got := ResolveACMAffiliation(tc.claimed, tc.source, tc.excerpt); got != tc.want {
			t.Errorf("%s: got %q want %q", name, got, tc.want)
		}
	}
}

func TestCanonicalLinkedInURL(t *testing.T) {
	const want = "https://linkedin.com/in/sara-iyer"
	for _, in := range []string{
		"https://www.linkedin.com/in/sara-iyer/", "http://linkedin.com/in/sara-iyer", "HTTPS://WWW.LinkedIn.com/in/Sara-Iyer",
		"https://www.linkedin.com/in/sara-iyer?utm_source=x#top", "linkedin.com/in/sara-iyer/", " https://www.linkedin.com/in/sara-iyer/details/experience/ ",
	} {
		if got := CanonicalLinkedInURL(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
	for _, in := range []string{"", "https://example.com/in/sara", "https://www.linkedin.com/company/acme", "https://www.linkedin.com/in/", "not a url"} {
		if got := CanonicalLinkedInURL(in); got != "" {
			t.Errorf("%q: got %q want empty", in, got)
		}
	}
}

func TestRoleFamilyIsOnlyAGuessFromTheTitle(t *testing.T) {
	for in, want := range map[string]string{
		"Staff Software Engineer": RoleFamilyTech, "ML Engineer": RoleFamilyTech,
		"Director of Operations & Supply Chain": RoleFamilyOperations, "Strategy Lead": RoleFamilyOperations,
		"Marketing Lead": "", "": "",
	} {
		if got := RoleFamilyOf(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestDMProposalsNeverIncludeADoNotContactPerson(t *testing.T) {
	list := AlumniHeader + "\n" +
		"| Sara Iyer | 2021 | Google | ML Engineer | ACM VIT | https://www.linkedin.com/in/sara | x | not contacted | 2026-11-20 |\n" +
		"| Meera Das | 2022 | Razorpay | Software Engineer | | https://www.linkedin.com/in/meera/ | x | not contacted | 2026-11-20 |\n" +
		"| Dev Rao | 2023 | Razorpay | Backend Engineer | | https://www.linkedin.com/in/dev-rao | x | do not contact | 2026-11-20 |"
	skip := func(link string) bool {
		p, _ := ProfileURL(link)
		return strings.EqualFold(p, "https://www.linkedin.com/in/sara/")
	}
	ps := AlumniDMProposalsExcluding(list, 8, skip)
	if len(ps) != 1 || ps[0].Contact.Name != "Meera Das" {
		t.Fatalf("Sara is skipped by the person record, Dev by his row's own status: %+v", ps)
	}
	if got := AlumniDMProposals(list, 8); len(got) != 2 {
		t.Fatalf("the plain call still skips a row that says do not contact: %d", len(got))
	}
}

func TestEvaluateGraduationEligibility(t *testing.T) {
	cutoff := DefaultCutoffDate // 2022-11-26
	y20, y21, y22, y23 := 2020, 2021, 2022, 2023

	if got := EvaluateGraduationEligibility(nil, nil, cutoff); got != CohortUnknown {
		t.Errorf("nil date/year: got %q, want %q", got, CohortUnknown)
	}
	if got := EvaluateGraduationEligibility(&y20, nil, cutoff); got != CohortEligible {
		t.Errorf("2020: got %q, want %q", got, CohortEligible)
	}
	if got := EvaluateGraduationEligibility(&y21, nil, cutoff); got != CohortEligible {
		t.Errorf("2021: got %q, want %q", got, CohortEligible)
	}
	if got := EvaluateGraduationEligibility(&y22, nil, cutoff); got != CohortBorderline2022 {
		t.Errorf("2022 without exact date: got %q, want %q", got, CohortBorderline2022)
	}
	if got := EvaluateGraduationEligibility(&y23, nil, cutoff); got != CohortIneligible {
		t.Errorf("2023: got %q, want %q", got, CohortIneligible)
	}

	exactBefore := time.Date(2022, 6, 15, 0, 0, 0, 0, time.UTC)
	exactAfter := time.Date(2022, 12, 1, 0, 0, 0, 0, time.UTC)
	if got := EvaluateGraduationEligibility(&y22, &exactBefore, cutoff); got != CohortEligible {
		t.Errorf("exact June 2022: got %q, want %q", got, CohortEligible)
	}
	if got := EvaluateGraduationEligibility(&y22, &exactAfter, cutoff); got != CohortIneligible {
		t.Errorf("exact Dec 2022: got %q, want %q", got, CohortIneligible)
	}
}

func TestClassifyRoleAndSeniority(t *testing.T) {
	title, fn, fam, sen, conf := ClassifyRole("Co-founder & CTO")
	if title != "Co-founder & CTO" || fn != "technology" || fam != RoleFamilyTech || sen != "founder" || conf < 90 {
		t.Errorf("founder CTO: got %s, %s, %s, %s, %d", title, fn, fam, sen, conf)
	}

	_, fn2, fam2, sen2, _ := ClassifyRole("VP of Operations and Supply Chain")
	if fn2 != "operations" || fam2 != RoleFamilyOperations || sen2 != "executive" {
		t.Errorf("VP ops: got %s, %s, %s", fn2, fam2, sen2)
	}

	_, fn3, fam3, sen3, _ := ClassifyRole("Lead Frontend Engineer")
	if fn3 != "technology" || fam3 != RoleFamilyTech || sen3 != "lead" {
		t.Errorf("Lead frontend: got %s, %s, %s", fn3, fam3, sen3)
	}
}

func TestEvaluatePriorityTier(t *testing.T) {
	// P1: Confirmed ACM + High-Potential Startup Employer
	tier, score, _ := EvaluatePriorityTier("confirmed", "HIGH", "Zetwerk", true, true, false)
	if tier != PriorityP1 || score < 90 {
		t.Errorf("expected P1, got %s (score %d)", tier, score)
	}

	// P2: Confirmed ACM + Other Relevant Startup Employer
	tier, score, _ = EvaluatePriorityTier("confirmed", "LOW/UNKNOWN", "Procol", true, true, false)
	if tier != PriorityP2 {
		t.Errorf("expected P2, got %s (score %d)", tier, score)
	}

	// P3: Other VIT + High-Potential Startup Employer
	tier, score, _ = EvaluatePriorityTier("unknown", "HIGH", "Infra.Market", true, true, false)
	if tier != PriorityP3 {
		t.Errorf("expected P3, got %s (score %d)", tier, score)
	}

	// P4: Other VIT + Relevant Startup Employer
	tier, score, _ = EvaluatePriorityTier("unknown", "LOW/UNKNOWN", "Winuall", true, true, false)
	if tier != PriorityP4 {
		t.Errorf("expected P4, got %s (score %d)", tier, score)
	}

	// Excluded Employers: IT service body shops and Big Tech mega-corps route strictly to REVIEW with score 10
	tier, score, rat := EvaluatePriorityTier("confirmed", "HIGH", "TCS", true, true, false)
	if tier != PriorityReview || score != 10 || !strings.Contains(rat, "IT Services") {
		t.Errorf("expected TCS to route to REVIEW with score 10, got %s (score %d, rat %s)", tier, score, rat)
	}

	tier, score, rat = EvaluatePriorityTier("confirmed", "HIGH", "Amazon", true, true, false)
	if tier != PriorityReview || score != 10 || !strings.Contains(rat, "Mega-Corp") {
		t.Errorf("expected Amazon to route to REVIEW with score 10, got %s (score %d, rat %s)", tier, score, rat)
	}

	tier, score, _ = EvaluatePriorityTier("confirmed", "HIGH", "Microsoft", true, true, false)
	if tier != PriorityReview || score != 10 {
		t.Errorf("expected Microsoft to route to REVIEW with score 10, got %s (score %d)", tier, score)
	}

	tier, score, _ = EvaluatePriorityTier("confirmed", "HIGH", "Infosys", true, true, false)
	if tier != PriorityReview || score != 10 {
		t.Errorf("expected Infosys to route to REVIEW with score 10, got %s (score %d)", tier, score)
	}

	// REVIEW: Borderline or unverified campus
	tier, _, _ = EvaluatePriorityTier("confirmed", "HIGH", "Zetwerk", false, true, false)
	if tier != PriorityReview {
		t.Errorf("unverified campus should be REVIEW, got %s", tier)
	}
	tier, _, _ = EvaluatePriorityTier("confirmed", "HIGH", "Zetwerk", true, false, true)
	if tier != PriorityReview {
		t.Errorf("borderline 2022 should be REVIEW, got %s", tier)
	}
}

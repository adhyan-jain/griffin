package service

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
)

var forbiddenPhrases = []string{
	"adhyan", "palni", "computehive", "senior core", "like you were", "following in the footsteps",
	"admire", "impressive", "inspiring", "genuinely", "referral", "opening", "opportunit", "job",
	"verified from primary sources",
}

func examplePerson(name string) *domain.Person {
	url := "https://www.linkedin.com/in/example-" + uuid.NewString()[:8]
	return &domain.Person{
		ID: uuid.New(), Name: name, ProfileURL: &url,
		Company: "Example Labs", Title: "Software Engineer",
		VITAffiliation: domain.AffiliationConfirmedVellore, GraduationEligibility: domain.CohortEligible,
		ContactState: domain.ContactNotContacted, PreferredChannel: "linkedin",
	}
}

var fullProfile = domain.OwnerProfile{
	FullName: "Sample Sender", School: "Example University", Degree: "B.Tech",
	Facts: []domain.ProfileFact{
		{ID: "f1", Project: "Example Runner", Text: "A task runner.", Source: "resume", Tags: []string{"project"}},
		{ID: "f2", Project: "ACM-VIT", Text: "Member of the club.", Source: "resume"},
	},
}

func TestVITDraftHasNoForbiddenPhrases(t *testing.T) {
	acmNoSource := examplePerson("Alex Example")
	acmNoSource.ACMAffiliation = "confirmed"
	acmSource := examplePerson("Blake Example")
	acmSource.ACMAffiliation = "confirmed"
	acmSource.ACMSource = "https://example.org/acm-roster"
	acmSource.ACMRole = "Lead"
	plain := examplePerson("Casey Example")
	noRole := examplePerson("Drew Example")
	noRole.Title = ""

	for _, p := range []*domain.Person{acmNoSource, acmSource, plain, noRole} {
		for pname, prof := range map[string]domain.OwnerProfile{"empty": {}, "full": fullProfile} {
			d, err := GenerateVITAlumniDraft(p, prof)
			if err != nil {
				t.Fatal(err)
			}
			text := strings.ToLower(d.ShortNote + "\n" + d.WarmDraft + "\n" + strings.Join(d.VerifiedFacts, "\n"))
			for _, bad := range forbiddenPhrases {
				if strings.Contains(text, bad) {
					t.Errorf("%s/%s: draft contains %q:\n%s", p.Name, pname, bad, text)
				}
			}
		}
	}
}

func TestVITDraftPlaceholdersAndReviewFlag(t *testing.T) {
	d, err := GenerateVITAlumniDraft(examplePerson("Alex Example"), domain.OwnerProfile{})
	if err != nil {
		t.Fatal(err)
	}
	if !d.NeedsUserReview {
		t.Error("empty profile must set NeedsUserReview")
	}
	for _, ph := range []string{"[your name]", "[your college]"} {
		if !strings.Contains(d.WarmDraft, ph) {
			t.Errorf("warm draft missing %s", ph)
		}
	}
	if !strings.Contains(d.WarmDraft, "[one line about what you're building]") {
		t.Error("warm draft missing project placeholder")
	}

	d, err = GenerateVITAlumniDraft(examplePerson("Alex Example"), fullProfile)
	if err != nil {
		t.Fatal(err)
	}
	if d.NeedsUserReview || strings.Contains(d.WarmDraft, "[") {
		t.Errorf("full profile must need no review: %s", d.WarmDraft)
	}
	if !strings.Contains(d.WarmDraft, "Sample Sender") || !strings.Contains(d.WarmDraft, "Example Runner") {
		t.Errorf("sender facts missing: %s", d.WarmDraft)
	}
}

func TestVITDraftACMLineNeedsSource(t *testing.T) {
	p := examplePerson("Alex Example")
	p.ACMAffiliation = "confirmed"
	d, _ := GenerateVITAlumniDraft(p, fullProfile)
	if strings.Contains(d.ShortNote, "ACM") || strings.Contains(d.WarmDraft, "ACM") {
		t.Error("ACM mentioned without a source URL")
	}
	p.ACMSource = "https://example.org/roster"
	d, _ = GenerateVITAlumniDraft(p, domain.OwnerProfile{})
	if !strings.Contains(d.ShortNote, "I saw you were part of ACM-VIT.") || strings.Contains(d.ShortNote, "where I'm involved") {
		t.Errorf("unexpected ACM line without a sender ACM fact: %s", d.ShortNote)
	}
	d, _ = GenerateVITAlumniDraft(p, fullProfile)
	if !strings.Contains(d.ShortNote, "where I'm involved now") {
		t.Errorf("sender ACM fact should be used: %s", d.ShortNote)
	}
	if len(d.VerifiedFacts) < 2 || !strings.Contains(strings.Join(d.VerifiedFacts, " "), p.ACMSource) {
		t.Errorf("ACM source missing from verified facts: %v", d.VerifiedFacts)
	}
	p.ACMAffiliation = "likely"
	d, _ = GenerateVITAlumniDraft(p, fullProfile)
	if strings.Contains(d.ShortNote, "ACM") {
		t.Error("only confirmed ACM may be mentioned")
	}
}

func TestVITDraftShortNoteFits(t *testing.T) {
	p := examplePerson(strings.Repeat("Longname", 40) + " Example")
	p.Title = strings.Repeat("Principal Staff Engineer ", 10)
	p.Company = strings.Repeat("Example Systems ", 10)
	p.ACMAffiliation, p.ACMSource = "confirmed", "https://example.org/roster"
	d, err := GenerateVITAlumniDraft(p, fullProfile)
	if err != nil {
		t.Fatal(err)
	}
	if n := utf8.RuneCountInString(d.ShortNote); n > 300 {
		t.Errorf("short note is %d runes", n)
	}
	if strings.Contains(d.ShortNote, "...") {
		t.Errorf("note must not be truncated with an ellipsis: %s", d.ShortNote)
	}
}

func TestVITDraftSelfReportedRoleAndFacts(t *testing.T) {
	p := examplePerson("Alex Example")
	d, _ := GenerateVITAlumniDraft(p, fullProfile)
	joined := strings.Join(d.VerifiedFacts, "\n")
	if !strings.Contains(joined, "self-reported current role: Software Engineer at Example Labs") {
		t.Errorf("role not labelled self-reported: %s", joined)
	}
	if !strings.Contains(joined, "VIT Vellore campus confirmed") {
		t.Errorf("campus fact missing: %s", joined)
	}
	p.VITAffiliation = domain.AffiliationUnconfirmed
	d, _ = GenerateVITAlumniDraft(p, fullProfile)
	if strings.Contains(strings.Join(d.VerifiedFacts, "\n"), "VIT Vellore") {
		t.Error("campus fact stated without confirmation")
	}
}

func TestVITDraftRefusesDNCAndExcluded(t *testing.T) {
	dnc := examplePerson("Alex Example")
	dnc.DoNotContact = true
	out := examplePerson("Blake Example")
	out.ContactState = domain.ContactOptedOut
	big := examplePerson("Casey Example")
	big.Company = "Microsoft"
	for _, p := range []*domain.Person{dnc, out, big} {
		if _, err := GenerateVITAlumniDraft(p, fullProfile); err == nil {
			t.Errorf("%s must be refused", p.Name)
		}
	}
	if _, err := GenerateVITAlumniDraft(nil, fullProfile); err == nil {
		t.Error("nil person must be refused")
	}
}

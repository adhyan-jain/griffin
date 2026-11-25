package prep

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/assess"
	"github.com/griffin/core/internal/domain"
)

// fakeProfile is the Library document form of an owner profile (what cmd/seed_profile writes): invented for the test.
const fakeProfile = `# Owner profile

- Full name: Asha Verma
- Email: asha@example.com
- GitHub: https://github.com/asha-v
- LinkedIn: https://linkedin.com/in/asha-v
- Education: B.Tech Computer Science, Northfield Institute of Technology
- Expected graduation: 2028
- CGPA: 8.9

## Verified facts

- [f1] ComputeHive: built a distributed task runner in Go with gRPC and Postgres
- [f2] ComputeHive: wrote the scheduler and the worker lease logic in Go
- [f3] LabTutor: built a tutoring app with React and Node.js
- [f4] Palni: interned as an AI Engineer building LLM apps with RAG
`

const fakePosting = `Backend Engineering Intern. You will build and operate Go services backed by Postgres, work on distributed systems problems
and ship features with a small team. Experience with gRPC or Docker is a plus. Why do you want to join Acme? Tell us in a few lines.
Please include your GitHub profile, a transcript of your grades and your graduation year. What is your notice period?
Location: Bengaluru. Stipend: 50,000 per month.`

func fakeListing() *domain.Listing {
	h := domain.DescriptionHash(fakePosting)
	return &domain.Listing{ID: uuid.New(), UserID: uuid.New(), Company: "Acme Technologies", Role: "Backend Engineering Intern", Location: "Bengaluru", URL: "https://acme.example/jobs/1", DescriptionHash: &h}
}

const goodLetter = "Dear Hiring Team,\n\nI am applying for the Backend Engineering Intern role at Acme Technologies. I am a B.Tech Computer Science student at Northfield Institute of Technology, expected to graduate in 2028. " +
	"I built ComputeHive, a distributed task runner in Go with gRPC and Postgres, and wrote its scheduler and worker lease logic. That is the kind of service work your posting describes, and I would enjoy doing it on your team.\n\n" +
	"Thank you for considering my application.\n\nSincerely,\nAsha Verma"

const goodWhy = "Your posting describes building and operating Go services backed by Postgres on a small team. That matches what I built in ComputeHive, and I want to do it with real users."

// scripted returns the replies in order and counts the calls.
type scripted struct {
	replies []string
	calls   int
	prompts []string
}

func (s *scripted) model() assess.ModelFunc {
	return func(_ context.Context, _, user string) (string, error) {
		s.prompts = append(s.prompts, user)
		i := s.calls
		s.calls++
		if i >= len(s.replies) {
			return "", errors.New("no more scripted replies")
		}
		return s.replies[i], nil
	}
}

func reply(letter, why string) string {
	b := strings.NewReplacer("\n", `\n`, `"`, `\"`)
	return `{"cover_letter": "` + b.Replace(letter) + `", "why_join": "` + b.Replace(why) + `"}`
}

func build(t *testing.T, profile string, model assess.ModelFunc) Result {
	t.Helper()
	l := fakeListing()
	a := &domain.Assessment{RoleClass: domain.RoleBackend, Eligibility: domain.EligibilityConfirmed}
	return Build(context.Background(), Input{Listing: l, Description: fakePosting, Assessment: a, Profile: ParseProfile(profile), Model: model, ModelName: "fake-flash", Now: time.Now()})
}

func TestParseProfileReadsBothFormsOnceAndNeverThePhone(t *testing.T) {
	both := fakeProfile + "\n- Phone: +91 99999 99999\n\nCandidate Background (verified owner profile):\n- Full Name: Asha Verma\n- [f1] ComputeHive: built a distributed task runner in Go with gRPC and Postgres\n"
	p := ParseProfile(both)
	if p.Name != "Asha Verma" || p.GradYear != "2028" || p.GitHub != "https://github.com/asha-v" {
		t.Fatalf("identity not read: %+v", p)
	}
	if len(p.Facts) != 4 {
		t.Fatalf("a fact present in both forms must count once: %d facts", len(p.Facts))
	}
	if strings.Contains(strings.Join(p.Lines, " "), "99999") {
		t.Fatalf("the phone number was read: %v", p.Lines)
	}
}

func TestSelectProjectsByDeterministicSkillOverlapWithThePostingQuote(t *testing.T) {
	p := ParseProfile(fakeProfile)
	got := SelectProjects(p, "Backend Engineering Intern", fakePosting)
	if len(got) == 0 || got[0].Project != "ComputeHive" {
		t.Fatalf("ComputeHive shares Go, gRPC and Postgres with the posting and must come first: %+v", got)
	}
	for _, pr := range got {
		if pr.Project == "LabTutor" || pr.Project == "Palni" {
			t.Fatalf("%s shares no skill with this posting and must not be chosen", pr.Project)
		}
		if !assess.QuoteIn(fakePosting, pr.Quote) {
			t.Fatalf("the quote %q is not in the posting", pr.Quote)
		}
		for _, f := range pr.Facts {
			if !strings.Contains(fakeProfile, f.Text) {
				t.Fatalf("fact %q is not verbatim from the profile", f.Text)
			}
		}
	}
	again := SelectProjects(p, "Backend Engineering Intern", fakePosting)
	if len(again) != len(got) || again[0].Project != got[0].Project || strings.Join(again[0].Skills, ",") != strings.Join(got[0].Skills, ",") {
		t.Fatal("selection is not deterministic")
	}
	// An AI posting picks the AI project instead.
	ai := SelectProjects(p, "AI Engineering Intern", "Build LLM applications with RAG and AI agents. Python required for the whole internship role.")
	if len(ai) == 0 || ai[0].Project != "Palni" {
		t.Fatalf("an LLM posting should pick Palni: %+v", ai)
	}
}

func TestRequiredDocumentsAndQuestionsAreReadFromThePosting(t *testing.T) {
	p := ParseProfile(fakeProfile)
	docs := RequiredDocuments(fakePosting, p, true, "resume.pdf")
	status := map[string]string{}
	for _, d := range docs {
		status[d.Name] = d.Status
	}
	if status["Resume"] != "ready" || status["Cover letter"] != "written" || status["GitHub"] != "ready" || status["Transcript"] != "missing" {
		t.Fatalf("documents: %v", status)
	}
	if _, ok := status["Portfolio"]; ok {
		t.Fatal("a portfolio the posting never names must not be listed")
	}
	keys := map[string]string{}
	for _, q := range QuestionsIn(fakePosting) {
		keys[q.Key] = q.Quote
		if !assess.QuoteIn(fakePosting, q.Quote) {
			t.Fatalf("question quote %q is not in the posting", q.Quote)
		}
	}
	for _, want := range []string{QWhyJoin, QNoticePeriod, QGraduation} {
		if _, ok := keys[want]; !ok {
			t.Fatalf("question %s not detected: %v", want, keys)
		}
	}
}

func TestBuildWritesAGroundedPackage(t *testing.T) {
	s := &scripted{replies: []string{reply(goodLetter, goodWhy)}}
	r := build(t, fakeProfile, s.model())
	if r.ModelErr != nil || s.calls != 1 || r.ModelCalls != 1 {
		t.Fatalf("calls=%d err=%v", s.calls, r.ModelErr)
	}
	p := r.Package
	if p.CoverLetter != goodLetter {
		t.Fatalf("the checked letter was not kept: %q", p.CoverLetter)
	}
	if p.ResumeFile != domain.ResumeFiles["backend"] || p.RoleClass != domain.RoleBackend {
		t.Fatalf("a backend role gets the backend resume: %s", p.ResumeFile)
	}
	var grad, why bool
	for _, a := range p.Answers {
		grad = grad || (a.Question == "What is your graduation year?" && a.Answer == "2028")
		why = why || (a.Question == "Why do you want to join us?" && a.Answer == goodWhy)
	}
	if !grad || !why {
		t.Fatalf("answers: %+v", p.Answers)
	}
	if p.ModelUsed == nil || *p.ModelUsed != "fake-flash" {
		t.Fatal("the model used was not recorded")
	}
	if len(p.Evidence) == 0 {
		t.Fatal("no evidence")
	}
}

func TestBuildListsWhatTheProfileDoesNotSayAsMissing(t *testing.T) {
	// The posting asks for the notice period and a transcript; the profile has neither.
	s := &scripted{replies: []string{reply(goodLetter, goodWhy)}}
	r := build(t, fakeProfile, s.model())
	missing := strings.Join(r.Package.MissingInformation, "\n")
	for _, want := range []string{"notice period", "Transcript"} {
		if !strings.Contains(strings.ToLower(missing), strings.ToLower(want)) {
			t.Fatalf("%q must be listed as missing: %s", want, missing)
		}
	}
	if r.Package.Status != domain.PackageNeedsOwner {
		t.Fatalf("a package with missing information needs the owner: %s", r.Package.Status)
	}
	for _, a := range r.Package.Answers {
		if strings.Contains(strings.ToLower(a.Question), "notice") {
			t.Fatalf("a notice-period answer was invented: %+v", a)
		}
	}
	// And a profile without a graduation year cannot answer for it.
	noGrad := strings.ReplaceAll(fakeProfile, "- Expected graduation: 2028\n", "")
	s2 := &scripted{replies: []string{reply(strings.ReplaceAll(goodLetter, ", expected to graduate in 2028", ""), goodWhy)}}
	r2 := build(t, noGrad, s2.model())
	if !strings.Contains(strings.Join(r2.Package.MissingInformation, "\n"), "graduation year") {
		t.Fatalf("the graduation year is missing from the profile: %v", r2.Package.MissingInformation)
	}
}

func TestBuildCompleteWhenNothingIsMissing(t *testing.T) {
	posting := "Backend Engineering Intern. You will build Go services backed by Postgres and work on distributed systems with a small team in Bengaluru. Experience with gRPC is a plus. Please share your GitHub profile."
	l := fakeListing()
	h := domain.DescriptionHash(posting)
	l.DescriptionHash = &h
	s := &scripted{replies: []string{reply(goodLetter, "")}}
	r := Build(context.Background(), Input{Listing: l, Description: posting, Assessment: &domain.Assessment{RoleClass: domain.RoleBackend, Eligibility: domain.EligibilityConfirmed},
		Profile: ParseProfile(fakeProfile), Model: s.model(), ModelName: "fake-flash", Now: time.Now()})
	if len(r.Package.MissingInformation) != 0 || r.Package.Status != domain.PackageDraft {
		t.Fatalf("nothing is missing here: %v (%s)", r.Package.MissingInformation, r.Package.Status)
	}
}

func TestBuildDropsAnInventedClaimAndRetriesOnce(t *testing.T) {
	invented := strings.Replace(goodLetter, "I built ComputeHive", "At Google I led 12 engineers, and I built ComputeHive", 1)
	s := &scripted{replies: []string{reply(invented, goodWhy), reply(goodLetter, goodWhy)}}
	r := build(t, fakeProfile, s.model())
	if s.calls != 2 || r.Package.CoverLetter != goodLetter {
		t.Fatalf("the second draft should replace the failed one: calls=%d letter=%q", s.calls, r.Package.CoverLetter)
	}
	if !strings.Contains(s.prompts[1], "Google") || !strings.Contains(s.prompts[1], "REJECTED") {
		t.Fatal("the retry must say what was wrong")
	}
}

func TestBuildStoresNoLetterThatKeepsFailingTheFactCheck(t *testing.T) {
	invented := strings.Replace(goodLetter, "I built ComputeHive", "At Google I led 12 engineers, and I built ComputeHive", 1)
	s := &scripted{replies: []string{reply(invented, goodWhy), reply(invented, goodWhy), reply(goodLetter, goodWhy)}}
	r := build(t, fakeProfile, s.model())
	if s.calls != 2 {
		t.Fatalf("at most two model calls per listing, made %d", s.calls)
	}
	p := r.Package
	if p.CoverLetter != "" {
		t.Fatalf("a letter with an invented claim was stored: %q", p.CoverLetter)
	}
	if !strings.Contains(strings.Join(p.MissingInformation, "\n"), "cover letter could not be written") {
		t.Fatalf("the gap must be listed: %v", p.MissingInformation)
	}
	if p.Status != domain.PackageNeedsOwner {
		t.Fatal("a package without its letter needs the owner")
	}
}

func TestBuildRejectsATechnologyTheProfileDoesNotHave(t *testing.T) {
	rust := strings.Replace(goodLetter, "I built ComputeHive", "I am fluent in Rust and I built ComputeHive", 1)
	s := &scripted{replies: []string{reply(rust, goodWhy), reply(rust, goodWhy)}}
	r := build(t, fakeProfile, s.model())
	if r.Package.CoverLetter != "" {
		t.Fatal("a letter naming a skill that is not in the profile was stored")
	}
}

func TestBuildEnforcesTheWordLimit(t *testing.T) {
	long := goodLetter + " " + strings.Repeat("I would be a good fit for the team. ", 30)
	s := &scripted{replies: []string{reply(long, goodWhy), reply(long, goodWhy)}}
	r := build(t, fakeProfile, s.model())
	if r.Package.CoverLetter != "" || Words(goodLetter) > MaxLetterWords {
		t.Fatal("a letter over 180 words was stored")
	}
	if got := Words(goodLetter); got < MinLetterWords {
		t.Fatalf("the fixture letter itself is too short: %d", got)
	}
}

func TestBuildAsksNoModelWithoutAPostingOrAProfile(t *testing.T) {
	s := &scripted{replies: []string{reply(goodLetter, goodWhy)}}
	l := fakeListing()
	r := Build(context.Background(), Input{Listing: l, Description: "", Profile: ParseProfile(fakeProfile), Model: s.model(), Now: time.Now()})
	if s.calls != 0 || !strings.Contains(strings.Join(r.Package.MissingInformation, "\n"), "open the posting") {
		t.Fatalf("a lead with no posting text gets no model call: calls=%d missing=%v", s.calls, r.Package.MissingInformation)
	}
	r2 := build(t, "", s.model())
	if s.calls != 0 || !strings.Contains(strings.Join(r2.Package.MissingInformation, "\n"), "profile is empty") {
		t.Fatalf("no profile, no call: calls=%d", s.calls)
	}
}

func TestBuildReportsAModelFailureWithoutAPackage(t *testing.T) {
	failing := func(context.Context, string, string) (string, error) { return "", errors.New("503 from the model") }
	r := build(t, fakeProfile, failing)
	if r.ModelErr == nil {
		t.Fatal("a model that failed must not yield a stored package")
	}
}

func TestBuildNotesEligibilityAndDeadlineProblems(t *testing.T) {
	l := fakeListing()
	past := time.Now().Add(-48 * time.Hour)
	l.Deadline = &past
	a := &domain.Assessment{RoleClass: domain.RoleBackend, Eligibility: domain.EligibilityIneligible,
		EligibilityReasons: []domain.Reason{{Reason: "Only students graduating in 2026 may apply", Quote: "graduating in 2026"}}}
	r := Build(context.Background(), Input{Listing: l, Description: fakePosting, Assessment: a, Profile: ParseProfile(fakeProfile), Now: time.Now()})
	missing := strings.Join(r.Package.MissingInformation, "\n")
	if !strings.Contains(missing, "rule you out") || !strings.Contains(missing, "deadline has passed") {
		t.Fatalf("missing: %s", missing)
	}
}

func TestResumeChoiceFollowsTheRoleClass(t *testing.T) {
	for class, key := range map[string]string{
		domain.RoleBackend: "backend", domain.RoleInfra: "backend", domain.RoleAIML: "AI/ML", domain.RoleResearch: "AI/ML",
		domain.RoleFrontend: "software engineering", domain.RoleUnknown: "software engineering",
	} {
		if domain.ResumeFor(class) != domain.ResumeFiles[key] || domain.ResumeFor(class) == "" {
			t.Fatalf("%s: got %q want the %s resume", class, domain.ResumeFor(class), key)
		}
	}
}

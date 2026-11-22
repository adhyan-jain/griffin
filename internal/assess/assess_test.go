package assess

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/jobs"
	"github.com/griffin/core/internal/sources/ats"
)

func run(in Input) *domain.Assessment {
	if in.Key == "" {
		in.Key = "key:" + in.Title
	}
	return Assess(context.Background(), in, nil, Options{}).Assessment
}

func comp(a *domain.Assessment, name string) domain.ValueComponent {
	for _, c := range a.ValueBreakdown {
		if c.Name == name {
			return c
		}
	}
	return domain.ValueComponent{}
}

// A posting in the style of a real Greenhouse engineering internship: detailed engineering work, mentorship and a
// return offer, open to the owner's graduating year, in India.
const strongPosting = `About the role. You will design and build backend services in Go and Python that power our payments platform at scale.
You will own a feature end to end: write the API, design the Postgres schema and migrations, add Redis caching, ship it behind a flag and watch the latency and throughput dashboards after release.
You will work with distributed systems problems such as retries, idempotency and consistency across microservices, deployed on AWS with Docker and Kubernetes, with CI/CD and observability built in.
You will pair program with senior engineers, get code reviews on every change, and be assigned a mentor for the duration of the internship.
This is a 6 month internship. Open to students graduating in 2027 or 2028. Strong performers may receive a pre-placement offer (PPO).
We offer a stipend of ₹60,000 per month.`

const shallowHighPayPosting = `We are looking for a Software Development Intern to help our customer success team. You will update spreadsheets, answer customer emails, do data entry for new accounts and prepare weekly sales reports for the team lead.
You will also help with social media posts and cold calling for lead generation. No prior experience is needed and training is provided on the job.
The role is based in Bengaluru and runs for 3 months. Candidates graduating in 2028 are welcome to apply.`

// ---- employment and role -----------------------------------------------------------------------------------------

func TestClassifyEmployment(t *testing.T) {
	cases := []struct{ title, desc, want string }{
		{"Software Development Intern", "", domain.EmploymentInternship},
		{"Backend Engineering Internship", "", domain.EmploymentInternship},
		{"Senior Backend Engineer", "", domain.EmploymentFullTime},
		{"Software Engineer (New Grad)", "", domain.EmploymentFullTime},
		{"Platform Engineer", "This is a full-time position based in Pune.", domain.EmploymentFullTime},
		{"Platform Engineer", "5+ years of professional experience building services.", domain.EmploymentFullTime},
		{"Research Engineer", "This is a summer internship program for students.", domain.EmploymentInternship},
		{"Backend Engineer, Internationalisation", "You will design and implement the platform.", domain.EmploymentUnknown},
	}
	for _, c := range cases {
		if got, _ := ClassifyEmployment(c.title, c.desc); got != c.want {
			t.Errorf("%q: got %s, want %s", c.title, got, c.want)
		}
	}
}

func TestClassifyRoleKeepsFrontendResearchAndDevtools(t *testing.T) {
	cases := []struct{ title, desc, want string }{
		{"Frontend Developer Intern", "", domain.RoleFrontend},
		{"React Developer Intern", "", domain.RoleFrontend},
		{"Research Intern, Language Models", "", domain.RoleResearch},
		{"Machine Learning Engineer Intern", "", domain.RoleAIML},
		{"LLM Application Intern", "", domain.RoleAIML},
		{"Developer Tools Intern", "", domain.RoleDevtools},
		{"DevOps Intern", "", domain.RoleInfra},
		{"Android Developer Intern", "", domain.RoleMobile},
		{"Full Stack Intern", "", domain.RoleFullstack},
		{"Backend Engineer Intern", "", domain.RoleBackend},
		{"Security Engineer Intern", "", domain.RoleSecurity},
		{"Data Engineer Intern", "", domain.RoleData},
		{"Software Development Intern", "", domain.RoleOtherTech},
		{"Marketing Intern", "", domain.RoleNonTech},
		{"Sales Engineer", "", domain.RoleNonTech},
		{"Data Entry Intern", "", domain.RoleNonTech},
		{"Business Development Intern", "", domain.RoleNonTech},
		{"Intern", "", domain.RoleUnknown},
		{"Software Intern", "You will build React and Next.js components with CSS and HTML, and write Postgres queries and Redis caching behind a REST API in Node.js with Django services.", domain.RoleFullstack},
		{"Software Intern", "You will work on PyTorch training, LLM fine-tuning, embeddings and RAG evaluation.", domain.RoleAIML},
	}
	for _, c := range cases {
		if got, _ := ClassifyRole(c.title, c.desc); got != c.want {
			t.Errorf("%q: got %s, want %s", c.title, got, c.want)
		}
	}
}

// ---- location, work authorisation, graduation year ----------------------------------------------------------------

func TestUSOnlyRemoteIsIneligibleWithTheQuote(t *testing.T) {
	desc := "This is a remote role. Candidates must be authorized to work in the United States without sponsorship. " + strings.Repeat("You will build backend services and APIs with Postgres and Go. ", 6)
	a := run(Input{Title: "Backend Engineer Intern", Company: "Acme Cloud", Location: "Remote", Description: desc, SourceKind: "employer_ats", Verification: "employer_verified"})
	if a.Eligibility != domain.EligibilityIneligible {
		t.Fatalf("US-only remote should be ineligible, got %s", a.Eligibility)
	}
	if len(a.EligibilityReasons) == 0 || !QuoteIn(desc, a.EligibilityReasons[0].Quote) || !strings.Contains(strings.ToLower(a.EligibilityReasons[0].Quote), "authorized to work in the united states") {
		t.Fatalf("the reason must carry the posting's words, got %+v", a.EligibilityReasons)
	}
	if a.ValueClass != domain.ValueLow {
		t.Fatalf("an ineligible listing is low, got %s", a.ValueClass)
	}
}

func TestWorkAuthorisationPatterns(t *testing.T) {
	ineligible := []string{
		"This role is US only.",
		"Open to UK-only applicants.",
		"You must be legally authorized to work in the U.S. at the time of hire.",
		"Applicants must have the right to work in the UK.",
		"US citizenship is required for this position.",
		"This role requires an active Secret clearance.",
		"You must be located in the United States to be considered.",
		"Only available to candidates based in Canada.",
	}
	for _, text := range ineligible {
		if r := AssessLocation("Remote", text); len(r.Blockers) == 0 {
			t.Errorf("should block: %q", text)
		}
	}
	fine := []string{
		"You must be authorized to work in India.",
		"Join us and help build things for users in the US.",
		"We are a US based company with a global team.",
		"Our team is distributed across the US, Europe and India.",
	}
	for _, text := range fine {
		if r := AssessLocation("Bengaluru, India", text); len(r.Blockers) != 0 {
			t.Errorf("should not block: %q (%+v)", text, r.Blockers)
		}
	}
}

func TestLocationFit(t *testing.T) {
	cases := []struct{ loc, desc, want string }{
		{"Bengaluru, India", "", domain.LocationIndia},
		{"Remote (India)", "", domain.LocationIndia},
		{"Remote - Worldwide", "", domain.LocationRemoteOK},
		{"Remote", "This role is fully remote and you can work from anywhere in the world.", domain.LocationRemoteOK},
		{"Remote", "A remote team.", domain.LocationUnknown},
		{"Remote, US", "", domain.LocationOutside},
		{"Remote - United States", "", domain.LocationOutside},
		{"Barcelona", "", domain.LocationOutside},
		{"London", "We offer relocation assistance for the right candidate.", domain.LocationRelocation},
		{"London", "We offer relocation assistance. We do not sponsor visas.", domain.LocationOutside},
		{"", "", domain.LocationUnknown},
	}
	for _, c := range cases {
		if got := AssessLocation(c.loc, c.desc).Fit; got != c.want {
			t.Errorf("%q: got %s, want %s", c.loc, got, c.want)
		}
	}
	// Onsite abroad and the posting says it does not sponsor: ineligible with the quote.
	r := AssessLocation("Berlin", "This is an onsite role. We do not sponsor work visas.")
	if len(r.Blockers) == 0 {
		t.Fatal("outside India with no sponsorship should be a blocker")
	}
}

func TestGraduationYear(t *testing.T) {
	cases := []struct {
		text string
		want string
	}{
		{"We are hiring students graduating in 2027.", domain.GradMismatch},
		{"Open to the 2026/2027 batch.", domain.GradMismatch},
		{"Candidates from the 2026 batch only.", domain.GradMismatch},
		{"Eligible: 2025, 2026 and 2027 graduates.", domain.GradMismatch},
		{"Open to students graduating in 2027 or 2028.", domain.GradFit},
		{"2027 and 2028 batch can apply.", domain.GradFit},
		{"Students graduating between 2026 and 2028 are welcome.", domain.GradFit},
		{"Graduating 2026 to 2028.", domain.GradFit},
		{"Class of 2028 undergraduates.", domain.GradFit},
		{"Graduating in 2027 or later.", domain.GradFit},
		{"Pre-final year students are encouraged to apply.", domain.GradFit},
		{"Open to any graduation year.", domain.GradFit},
		{"You will build things. No year is mentioned here.", domain.GradUnknown},
		{"We started in 2019 and raised funding in 2024.", domain.GradUnknown},
		{"Currently pursuing an M.Tech or MBA.", domain.GradMismatch},
	}
	for _, c := range cases {
		if got := AssessGrad(c.text).Fit; got != c.want {
			t.Errorf("%q: got %s, want %s", c.text, got, c.want)
		}
	}
	if r := AssessGrad("Currently pursuing a B.Tech or an M.Tech."); len(r.Blockers) != 0 {
		t.Errorf("an undergraduate is welcome: %+v", r.Blockers)
	}
	// "Final-year" alone is a hint for the reviewer, not a verdict.
	if r := AssessGrad("This internship is for final year students."); r.Fit != domain.GradUnknown || r.Soft == nil {
		t.Errorf("final-year should be a soft note: %+v", r)
	}
}

func TestGraduatingIn2027IsIneligibleForA2028Student(t *testing.T) {
	desc := "We are hiring a backend intern. Students graduating in 2027 can apply for this six month internship. " + strings.Repeat("You will build APIs with Go and Postgres. ", 6)
	a := run(Input{Title: "Backend Intern", Company: "Acme", Location: "Bengaluru", Description: desc})
	if a.Eligibility != domain.EligibilityIneligible || a.GradYearFit != domain.GradMismatch {
		t.Fatalf("got %s / %s", a.Eligibility, a.GradYearFit)
	}
	if !QuoteIn(desc, a.EligibilityReasons[0].Quote) {
		t.Fatalf("the quote must be in the posting: %+v", a.EligibilityReasons)
	}
}

// ---- compensation and duration ------------------------------------------------------------------------------------

func TestParseCompensation(t *testing.T) {
	cases := []struct {
		pay, title, desc string
		want             float64 // -1: unknown
		unpaid           bool
	}{
		{"₹ 30,000 /month", "", "", 30000, false},
		{"₹ 15,000 - 20,000 /month", "", "", 15000, false},
		{"₹ 6,00,000 /year", "", "", 50000, false},
		{"$45/hr", "", "", 45 * UsdToInr * HoursPerMonth, false},
		{"$4,000 per month", "", "", 4000 * UsdToInr, false},
		{"₹ 6 LPA", "", "", 50000, false},
		{"Unpaid", "", "", 0, true},
		{"", "Software Development Intern at Pledge India Foundation (Unpaid)", "", 0, true},
		{"", "", "This is an unpaid internship for students.", 0, true},
		{"", "", "The stipend is ₹25,000 per month for six months.", 25000, false},
		{"", "", "The pay range for this role is $60,000 - $80,000 per year.", 60000 * UsdToInr / 12, false},
		{"", "", "We raised $50 million in funding and grew 10x.", -1, false},
		{"", "", "This is not an unpaid internship; interns are paid ₹40,000 per month.", 40000, false},
		{"", "", "A competitive stipend will be provided.", -1, false},
		{"", "", "Stipend of ₹10,000.", -1, false}, // no period: not read
		{"", "", "", -1, false},
	}
	for _, c := range cases {
		got := ParseCompensation(c.pay, c.title, c.desc)
		if c.want < 0 {
			if got != nil {
				t.Errorf("%q %q %q: want unknown, got %+v", c.pay, c.title, c.desc, got)
			}
			continue
		}
		if got == nil {
			t.Errorf("%q %q %q: want %v, got unknown", c.pay, c.title, c.desc, c.want)
			continue
		}
		if diff := got.MonthlyINR - c.want; diff > 1 || diff < -1 || got.Unpaid != c.unpaid {
			t.Errorf("%q %q %q: got %+v, want %v unpaid=%v", c.pay, c.title, c.desc, got, c.want, c.unpaid)
		}
		if got.Quote == "" {
			t.Errorf("%q: pay must come with its quote", c.pay)
		}
	}
	if c := ParseCompensation("₹ 15,000 - 20,000 /month", "", ""); c.High != 20000 {
		t.Errorf("range high end: %+v", c)
	}
}

func TestParseDuration(t *testing.T) {
	cases := []struct{ title, desc, want string }{
		{"", "This is a 6 month internship.", "6 months"},
		{"", "Internship duration: 8-12 weeks.", "8-12 weeks"},
		{"", "The internship lasts for 3 months.", "3 months"},
		{"", "You need 3 years of experience.", ""},
		{"", "We launched 6 months ago.", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		if got, _ := ParseDuration(c.title, c.desc); got != c.want {
			t.Errorf("%q: got %q, want %q", c.desc, got, c.want)
		}
	}
}

// ---- skills -------------------------------------------------------------------------------------------------------

func TestSkillsAreMatchedAgainstTheOwnersSkills(t *testing.T) {
	owner := OwnerSkills("")
	got := MatchSkills("Backend Intern", "You will write Go and Python services with Postgres, Redis and gRPC on AWS, plus some Rust. Go beyond what is asked.", owner)
	names := map[string]bool{}
	for _, s := range got {
		names[s.Skill] = true
		if s.Quote == "" {
			t.Errorf("%s has no quote", s.Skill)
		}
	}
	for _, want := range []string{"Go", "Python", "PostgreSQL", "Redis", "gRPC", "AWS"} {
		if !names[want] {
			t.Errorf("missing %s in %v", want, names)
		}
	}
	if names["Rust"] {
		t.Error("Rust is not one of the owner's skills")
	}
	if got := MatchSkills("Intern", "Go beyond what is asked and go to market early.", owner); len(got) != 0 {
		t.Errorf("the verb is not the language: %+v", got)
	}
	// A skill in the owner's profile counts.
	if got := MatchSkills("Intern", "You will write Rust.", OwnerSkills("Projects: a Rust compiler front end.")); len(got) != 1 || got[0].Skill != "Rust" {
		t.Errorf("profile skills should count: %+v", got)
	}
}

// ---- the model stage ----------------------------------------------------------------------------------------------

// ambiguousPosting leaves eligibility unknown for code: a plain "Remote" with no country named and no year.
const ambiguousPosting = `Join our small research team as an intern. The work is on evaluating retrieval-augmented generation pipelines and writing the evaluation harness in Python.
You will read papers, run experiments on our inference cluster and report results to the team each week. The team is fully remote across several time zones.
Applicants should be enrolled in an undergraduate or graduate programme in computer science or a related field.`

func reply(v any) ModelFunc {
	b, _ := json.Marshal(v)
	return func(context.Context, string, string) (string, error) { return "```json\n" + string(b) + "\n```", nil }
}

func counting(f ModelFunc, n *int) ModelFunc {
	return func(ctx context.Context, s, u string) (string, error) { *n++; return f(ctx, s, u) }
}

func TestModelOutputWithAQuoteNotInThePostingIsRejected(t *testing.T) {
	in := Input{Title: "Research Intern", Company: "Acme Labs", Location: "Remote", Description: ambiguousPosting, Key: "k1"}
	liar := ModelReply{
		Eligibility:        "ineligible",
		EligibilityReasons: []domain.Reason{{Reason: "Only for US citizens", Quote: "Only US citizens may apply to this internship"}},
		PPO:                QuotedLevel{"yes", "Every intern receives a full-time offer"},
		Depth:              QuotedLevel{"deep", "world-class distributed training at planetary scale"},
		Skills:             []domain.SkillMatch{{Skill: "Go", Quote: "You will write Go services"}},
	}
	n := 0
	r := Assess(context.Background(), in, nil, Options{Model: counting(reply(liar), &n), ModelName: "m", AllowModel: true})
	if n != 1 || !r.ModelCalled {
		t.Fatalf("the model should have been asked once, calls=%d", n)
	}
	a := r.Assessment
	if a.Eligibility == domain.EligibilityIneligible {
		t.Fatal("a conclusion with an invented quote must not make a listing ineligible")
	}
	if a.Eligibility != domain.EligibilityUnknown {
		t.Fatalf("eligibility must stay unknown, got %s", a.Eligibility)
	}
	if string(a.ModelOutput) == "" || strings.Contains(string(a.ModelOutput), "Only US citizens") || strings.Contains(string(a.ModelOutput), "full-time offer") {
		t.Fatalf("nothing unverified may be stored: %s", a.ModelOutput)
	}
	if comp(a, CompPPO).Unknown != true || comp(a, CompDepth).Points == 25 {
		t.Fatalf("an unverified claim must not change the score: %+v", a.ValueBreakdown)
	}
	if a.ModelKey == nil || *a.ModelKey != "k1" {
		t.Fatal("the attempt is recorded so the model is not asked again for this description")
	}
}

func TestModelOutputWithVerifiedQuotesIsMerged(t *testing.T) {
	in := Input{Title: "Research Intern", Company: "Acme Labs", Location: "Remote", Description: ambiguousPosting, Key: "k2"}
	good := ModelReply{
		Eligibility:        "likely",
		EligibilityReasons: []domain.Reason{{Reason: "Open to undergraduates", Quote: "enrolled in an undergraduate or graduate programme in computer science"}},
		RoleClass:          "research", RoleQuote: "Join our small research team as an intern",
		Depth: QuotedLevel{"deep", "read papers, run experiments on our inference cluster"},
		Skills: []domain.SkillMatch{{Skill: "python", Quote: "writing the evaluation harness in Python"},
			{Skill: "Rust", Quote: "run experiments on our inference cluster"}, // not the owner's skill
		},
	}
	a := Assess(context.Background(), in, nil, Options{Model: reply(good), ModelName: "gemini-flash", AllowModel: true}).Assessment
	if a.Eligibility != domain.EligibilityLikely || len(a.EligibilityReasons) != 1 {
		t.Fatalf("got %s %+v", a.Eligibility, a.EligibilityReasons)
	}
	if a.ModelUsed == nil || *a.ModelUsed != "gemini-flash" {
		t.Fatal("model_used not set")
	}
	got := map[string]bool{}
	for _, s := range a.SkillsMatched {
		got[s.Skill] = true
	}
	if !got["Python"] || got["Rust"] || got["Go"] {
		t.Fatalf("only the owner's verified skills are kept (Python yes; Rust is not the owner's; the invented Go quote is not in the posting): %+v", a.SkillsMatched)
	}
	if d := comp(a, CompDepth); d.Unknown || d.Points != 25 || !QuoteIn(ambiguousPosting, d.Quote) {
		t.Fatalf("verified depth should be scored with its quote: %+v", d)
	}
	for _, c := range a.ValueBreakdown {
		if c.Quote != "" && !QuoteIn(ambiguousPosting+" "+in.Title+" "+in.Company, c.Quote) {
			t.Fatalf("every quoted component must quote the posting: %+v", c)
		}
	}
}

func TestModelIsAskedOncePerKeyAndWithinBudget(t *testing.T) {
	in := Input{Title: "Research Intern", Company: "Acme Labs", Location: "Remote", Description: ambiguousPosting, Key: "k3"}
	n := 0
	m := counting(reply(ModelReply{Eligibility: "likely", EligibilityReasons: []domain.Reason{{Reason: "ok", Quote: "enrolled in an undergraduate or graduate programme"}}}), &n)
	first := Assess(context.Background(), in, nil, Options{Model: m, ModelName: "m", AllowModel: true}).Assessment
	// Same key: the stored answer is reused, no second call, even when the rules re-score.
	again := Assess(context.Background(), in, first, Options{Model: m, ModelName: "m", AllowModel: true})
	if n != 1 || again.ModelCalled {
		t.Fatalf("the same description must not be asked twice, calls=%d", n)
	}
	if again.Assessment.Eligibility != domain.EligibilityLikely {
		t.Fatalf("the cached conclusion should still apply, got %s", again.Assessment.Eligibility)
	}
	// A changed description is a new key: asked again.
	in.Key, in.Description = "k4", ambiguousPosting+" We also use Go."
	Assess(context.Background(), in, first, Options{Model: m, ModelName: "m", AllowModel: true})
	if n != 2 {
		t.Fatalf("a changed description should be asked once more, calls=%d", n)
	}
	// Budget spent: no call, and the listing still gets a (deterministic) assessment.
	in.Key = "k5"
	r := Assess(context.Background(), in, nil, Options{Model: m, ModelName: "m", AllowModel: false})
	if n != 2 || r.ModelCalled || r.Assessment == nil {
		t.Fatalf("no call when the budget is spent, calls=%d", n)
	}
}

func TestModelIsNotAskedWhenCodeSettledIt(t *testing.T) {
	n := 0
	m := counting(reply(ModelReply{}), &n)
	// Eligibility settled by a quoted blocker.
	Assess(context.Background(), Input{Title: "Backend Intern", Location: "Remote", Key: "a", Description: "US only. " + strings.Repeat("You will build backend APIs with Postgres. ", 8)}, nil, Options{Model: m, AllowModel: true})
	// Eligibility settled as likely, role settled by the title.
	Assess(context.Background(), Input{Title: "Backend Intern", Location: "Pune, India", Key: "b", Description: strings.Repeat("You will build backend APIs with Postgres. ", 8)}, nil, Options{Model: m, AllowModel: true})
	// No text to quote.
	Assess(context.Background(), Input{Title: "Software Development Intern", Location: "Remote (India)", Key: "c"}, nil, Options{Model: m, AllowModel: true})
	if n != 0 {
		t.Fatalf("the model must not be asked, calls=%d", n)
	}
}

func TestBadModelReplyIsDiscardedNotStored(t *testing.T) {
	in := Input{Title: "Research Intern", Company: "Acme Labs", Location: "Remote", Description: ambiguousPosting, Key: "k6"}
	for name, m := range map[string]ModelFunc{
		"prose": func(context.Context, string, string) (string, error) { return "I think it is eligible.", nil },
		"wrong type": func(context.Context, string, string) (string, error) {
			return `{"eligibility": 5, "skills": "go"}`, nil
		},
	} {
		r := Assess(context.Background(), in, nil, Options{Model: m, ModelName: "m", AllowModel: true})
		if r.ModelErr == nil || r.Assessment.Eligibility != domain.EligibilityUnknown || len(r.Assessment.ModelOutput) != 0 {
			t.Errorf("%s: err=%v elig=%s out=%s", name, r.ModelErr, r.Assessment.Eligibility, r.Assessment.ModelOutput)
		}
	}
	// A failed call is not recorded as made, so a later run tries again.
	r := Assess(context.Background(), in, nil, Options{Model: func(context.Context, string, string) (string, error) { return "", errors.New("rate limited") }, AllowModel: true})
	if r.Assessment.ModelKey != nil || !r.ModelCalled {
		t.Errorf("a failed call must not be recorded as made: %+v", r.Assessment.ModelKey)
	}
}

func TestQuoteVerification(t *testing.T) {
	text := "You will build\n  backend   services in Go."
	for quote, want := range map[string]bool{
		"build backend services in Go": true, "BUILD BACKEND services": true, "build backend services in Rust": false,
		"Go": false /* too short to prove anything */, "": false, strings.Repeat("x", 500): false,
	} {
		if got := QuoteIn(text, quote); got != want {
			t.Errorf("%q: got %v, want %v", quote, got, want)
		}
	}
}

// ---- the career-value score ---------------------------------------------------------------------------------------

func TestScoreComponentsSumToOneHundredAndUnknownsAreFlagged(t *testing.T) {
	a := run(Input{Title: "Software Development Intern", Company: "Acme", Location: "Remote (India)"})
	max := 0.0
	for _, c := range a.ValueBreakdown {
		max += c.Max
		if c.Unknown && c.Points != c.Max/2 && c.Name != CompEmployer && c.Name != CompRelevance && c.Name != CompFeasibility {
			t.Errorf("an unknown component sits at the midpoint: %+v", c)
		}
		if c.Reason == "" {
			t.Errorf("every component explains itself: %+v", c)
		}
	}
	if max != 100 {
		t.Fatalf("maxima must sum to 100, got %v", max)
	}
	for _, name := range []string{CompDepth, CompCompensation, CompMentorship, CompPPO} {
		if c := comp(a, name); !c.Unknown {
			t.Errorf("%s was not in the listing and must be flagged unknown", name)
		}
	}
	if a.CompensationMonthlyINR != nil {
		t.Fatal("pay the listing does not state stays NULL")
	}
}

func TestStrongEngineeringBeatsHighPayShallowRole(t *testing.T) {
	strong := run(Input{Title: "Backend Engineer Intern", Company: "Razorpay", Location: "Bengaluru, India", Pay: "₹ 30,000 /month", Description: strongPosting, SourceKind: "employer_ats", Verification: "employer_verified"})
	shallow := run(Input{Title: "Software Development Intern", Company: "Acme Services Pvt Ltd", Location: "Bengaluru, India", Pay: "₹ 1,20,000 /month", Description: shallowHighPayPosting, SourceKind: "aggregator", Verification: "lead"})
	if comp(shallow, CompCompensation).Points != 15 {
		t.Fatalf("setup: the shallow role should score full pay, got %+v", comp(shallow, CompCompensation))
	}
	if strong.ValueScore <= shallow.ValueScore+25 {
		t.Fatalf("strong engineering (%d %s) must clearly outrank a high-pay shallow role (%d %s)", strong.ValueScore, strong.ValueClass, shallow.ValueScore, shallow.ValueClass)
	}
	if strong.ValueClass != domain.ValueExceptional && strong.ValueClass != domain.ValueStrong {
		t.Fatalf("the strong role should be strong or better, got %d %s", strong.ValueScore, strong.ValueClass)
	}
	if shallow.ValueClass == domain.ValueExceptional || shallow.ValueClass == domain.ValueStrong {
		t.Fatalf("the shallow role must not be strong, got %d %s", shallow.ValueScore, shallow.ValueClass)
	}
	if strong.Eligibility != domain.EligibilityConfirmed {
		t.Fatalf("the strong posting names 2028 in India: confirmed, got %s", strong.Eligibility)
	}
	if strong.CompensationMonthlyINR == nil || *strong.CompensationMonthlyINR != 30000 {
		t.Fatalf("pay comes from the listing's pay field: %v", strong.CompensationMonthlyINR)
	}
}

func TestCompensationCannotDominate(t *testing.T) {
	base := Input{Title: "Backend Engineer Intern", Company: "Acme", Location: "Pune, India", Description: strongPosting}
	low, high := base, base
	low.Pay, high.Pay = "₹ 10,000 /month", "₹ 5,00,000 /month"
	a, b := run(low), run(high)
	if gap := b.ValueScore - a.ValueScore; gap > 15 || gap <= 0 {
		t.Fatalf("a fifty-fold pay difference may move the score by at most 15 points, moved %d", gap)
	}
}

func TestUnpaidResearchRoleIsKeptAndNotAutoLow(t *testing.T) {
	desc := `Research Intern, Efficient Inference. You will work with our research scientists on speeding up transformer inference: implement quantization and batching experiments in PyTorch, benchmark latency and throughput on GPUs, and write up results for a paper.
You will be mentored by a senior researcher and present at the weekly reading group. This is an unpaid research internship for undergraduate students graduating in 2028 or 2029, 3 months long, remote from India.`
	a := run(Input{Title: "Research Intern, Efficient Inference", Company: "Open Models Lab", Location: "Remote (India)", Description: desc, SourceKind: "employer_site", Verification: "employer_verified", CompanyKind: "lab"})
	if a.RoleClass != domain.RoleResearch {
		t.Fatalf("role %s", a.RoleClass)
	}
	if a.CompensationMonthlyINR == nil || *a.CompensationMonthlyINR != 0 {
		t.Fatalf("stated unpaid is 0, not unknown: %v", a.CompensationMonthlyINR)
	}
	if c := comp(a, CompCompensation); c.Points != 7.5 || c.Unknown {
		t.Fatalf("unpaid research is scored neutral, not low: %+v", c)
	}
	if a.ValueClass == domain.ValueLow || a.ValueClass == domain.ValueExploratory {
		t.Fatalf("an unpaid research role with real depth and mentorship must not be low: %d %s", a.ValueScore, a.ValueClass)
	}
	if a.Eligibility != domain.EligibilityConfirmed {
		t.Fatalf("2028 and a remote India role are confirmed, got %s", a.Eligibility)
	}
	// The same unpaid pay on a non-research role does cost points.
	other := run(Input{Title: "Backend Intern", Company: "Open Models Lab", Location: "Remote (India)", Pay: "Unpaid", Description: strongPosting})
	if c := comp(other, CompCompensation); c.Points != 0 {
		t.Fatalf("unpaid non-research pay scores zero: %+v", c)
	}
}

func TestFrontendRoleIsNotDropped(t *testing.T) {
	desc := `You will build the web app our customers use every day with React, Next.js and TypeScript. You will design reusable UI components, optimise rendering performance, write unit tests, and work with our designers and backend engineers on the API contract.
You will be mentored by a staff engineer and review code with the team. Open to students graduating in 2028. The role is in Bengaluru, India.`
	a := run(Input{Title: "Frontend Engineer Intern", Company: "Acme", Location: "Bengaluru, India", Pay: "₹ 35,000 /month", Description: desc, SourceKind: "employer_ats", Verification: "employer_verified"})
	if a.RoleClass != domain.RoleFrontend {
		t.Fatalf("role %s", a.RoleClass)
	}
	if a.ValueClass == domain.ValueLow || a.ValueScore < 50 {
		t.Fatalf("a good frontend internship must not be dropped to the bottom: %d %s", a.ValueScore, a.ValueClass)
	}
	if len(a.SkillsMatched) < 3 {
		t.Fatalf("React, Next.js and TypeScript should match: %+v", a.SkillsMatched)
	}
}

func TestNonTechRoleIsLow(t *testing.T) {
	a := run(Input{Title: "Marketing Intern", Company: "Acme", Location: "Bengaluru, India", Pay: "₹ 1,00,000 /month", Description: shallowHighPayPosting})
	if a.RoleClass != domain.RoleNonTech || a.ValueClass != domain.ValueLow || a.ValueScore > nonTechCap {
		t.Fatalf("got %s %d %s", a.RoleClass, a.ValueScore, a.ValueClass)
	}
}

// has is the plain containment check for quotes code itself extracted (which may be short, like "Sales").
func has(text, quote string) bool {
	return strings.Contains(strings.ToLower(strings.Join(strings.Fields(text), " ")), strings.ToLower(strings.Join(strings.Fields(quote), " ")))
}

// ---- real fixtures ------------------------------------------------------------------------------------------------

func TestNGOUnpaidGenericInternRanksLow(t *testing.T) {
	var pledge, others []jobs.RawJob
	for _, j := range internshalaJobs(t) {
		if j.EmployerName == "Pledge India Foundation" {
			pledge = append(pledge, j)
		} else {
			others = append(others, j)
		}
	}
	if len(pledge) != 1 {
		t.Fatalf("the Internshala fixture should hold the Pledge India Foundation posting once, got %d", len(pledge))
	}
	if pledge[0].Compensation != "Unpaid" || pledge[0].Title != "Software Development Intern" {
		t.Fatalf("fixture drifted: %+v", pledge[0])
	}
	a := run(inputOf(pledge[0]))
	if a.ValueClass != domain.ValueLow && a.ValueClass != domain.ValueExploratory {
		t.Fatalf("an unpaid NGO generic dev intern must be low or exploratory, got %d %s", a.ValueScore, a.ValueClass)
	}
	if a.ValueScore >= 45 {
		t.Fatalf("score %d is too high for an unpaid NGO posting with no engineering detail", a.ValueScore)
	}
	if a.CompensationMonthlyINR == nil || *a.CompensationMonthlyINR != 0 || a.CompensationQuote != "Unpaid" {
		t.Fatalf("unpaid is read from the pay field with its quote: %v %q", a.CompensationMonthlyINR, a.CompensationQuote)
	}
	if a.Eligibility != domain.EligibilityUnknown || a.LocationFit != domain.LocationIndia {
		t.Fatalf("an aggregator lead with no description: eligibility unknown (review), place India; got %s %s", a.Eligibility, a.LocationFit)
	}
	if c := comp(a, CompDepth); !c.Unknown {
		t.Fatalf("depth cannot be read without text: %+v", c)
	}
	// And it is capped for thin evidence, whatever the midpoints add up to.
	var capped bool
	for _, c := range a.ValueBreakdown {
		capped = capped || c.Name == CompCap
	}
	if !capped {
		t.Fatalf("the cap for unread points should be explained: %+v", a.ValueBreakdown)
	}
	// Every Internshala lead in the fixture lands below a verified strong engineering posting.
	strong := run(Input{Title: "Backend Engineer Intern", Company: "Razorpay", Location: "Bengaluru, India", Pay: "₹ 30,000 /month", Description: strongPosting, SourceKind: "employer_ats", Verification: "employer_verified"})
	for _, j := range append(others, pledge...) {
		if got := run(inputOf(j)); got.ValueScore >= strong.ValueScore {
			t.Errorf("%s at %s (%d) must rank below the strong engineering posting (%d)", j.Title, j.EmployerName, got.ValueScore, strong.ValueScore)
		}
	}
}

func TestRealATSPostingsAreAssessedWithQuotes(t *testing.T) {
	var all []jobs.RawJob
	all = append(all, atsJobs(t, ats.Greenhouse{F: atsFetcher(t, "greenhouse.json")}, "stripe")...)
	all = append(all, atsJobs(t, ats.Lever{F: atsFetcher(t, "lever.json")}, "x")...)
	all = append(all, atsJobs(t, ats.Ashby{F: atsFetcher(t, "ashby.json")}, "x")...)
	if len(all) == 0 {
		t.Fatal("no fixtures")
	}
	sawBackend := false
	for _, j := range all {
		in := inputOf(j)
		a := run(in)
		text := in.Title + " " + in.Company + " " + in.Location + " " + in.Pay + " " + in.Description
		for _, r := range a.EligibilityReasons {
			if r.Quote != "" && !has(text, r.Quote) {
				t.Errorf("%s: eligibility quote not in the posting: %q", j.Title, r.Quote)
			}
		}
		for _, c := range a.ValueBreakdown {
			if c.Quote != "" && !has(text, c.Quote) {
				t.Errorf("%s: %s quote not in the posting: %q", j.Title, c.Name, c.Quote)
			}
		}
		if j.Title == "Backend Engineer, Internationalisation" {
			sawBackend = true
			if a.RoleClass != domain.RoleBackend || a.LocationFit != domain.LocationOutside {
				t.Errorf("the Stripe backend role in Barcelona: %s %s", a.RoleClass, a.LocationFit)
			}
			if a.ValueClass == domain.ValueExceptional || a.ValueClass == domain.ValueStrong {
				t.Errorf("onsite in Barcelona with no relocation offer is capped, got %s", a.ValueClass)
			}
			if comp(a, CompEmployer).Points != 20 {
				t.Errorf("Stripe is on the curated list: %+v", comp(a, CompEmployer))
			}
		}
	}
	if !sawBackend {
		t.Fatal("the Greenhouse fixture should hold the Stripe backend role")
	}
}

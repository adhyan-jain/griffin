package service

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
)

// memAssess is an in-memory AssessStore.
type memAssess struct {
	rows        []*domain.Listing
	description map[uuid.UUID]string
	assessments map[uuid.UUID]*domain.Assessment
	kind        map[uuid.UUID]string
}

func newMemAssess() *memAssess {
	return &memAssess{description: map[uuid.UUID]string{}, assessments: map[uuid.UUID]*domain.Assessment{}, kind: map[uuid.UUID]string{}}
}

func (m *memAssess) add(company, role, location, pay, description, sourceKind string) *domain.Listing {
	l := &domain.Listing{ID: uuid.New(), Company: company, Role: role, Location: location, Pay: pay, URL: "https://example.com/" + uuid.NewString(), Stage: domain.StageVerified,
		SourceKind: sourceKind, Verification: domain.VerificationLead, CreatedAt: time.Now()}
	if sourceKind != domain.SourceKindAggregator {
		l.Verification = domain.VerificationEmployer
	}
	m.setDescription(l, description)
	m.rows = append(m.rows, l)
	return l
}

func (m *memAssess) setDescription(l *domain.Listing, d string) {
	m.description[l.ID] = d
	if h := domain.DescriptionHash(d); h != "" {
		l.DescriptionHash = &h
	} else {
		l.DescriptionHash = nil
	}
}

func (m *memAssess) AssessmentSubjects(_ context.Context, _ uuid.UUID) ([]domain.AssessmentSubject, error) {
	var out []domain.AssessmentSubject
	for _, l := range m.rows {
		if l.Stage == domain.StageClosed {
			continue
		}
		s := domain.AssessmentSubject{Listing: l, CompanyKind: "unknown"}
		if a := m.assessments[l.ID]; a != nil {
			s.Assessed, s.Key, s.InputHash, s.Version = true, a.AssessmentKey, a.InputHash, a.Version
		}
		out = append(out, s)
	}
	return out, nil
}

func (m *memAssess) ListingDescription(_ context.Context, _, id uuid.UUID) (string, error) {
	return m.description[id], nil
}

func (m *memAssess) GetAssessment(_ context.Context, _, id uuid.UUID) (*domain.Assessment, error) {
	return m.assessments[id], nil
}

func (m *memAssess) SaveAssessment(_ context.Context, a *domain.Assessment, _ string) error {
	cp := *a
	m.assessments[a.ListingID] = &cp
	return nil
}

func (m *memAssess) ListAssessed(_ context.Context, _ uuid.UUID, f domain.AssessedFilter) ([]*domain.Listing, error) {
	var out []*domain.Listing
	for _, l := range m.rows {
		cp := *l
		cp.Assessment = m.assessments[l.ID]
		out = append(out, &cp)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Assessment, out[j].Assessment
		if (a == nil) != (b == nil) {
			return a != nil
		}
		return a != nil && a.ValueScore > b.ValueScore
	})
	return out, nil
}

const strongDesc = `You will design and build backend services in Go and Python for our payments platform at scale. You will own features end to end: the API, the Postgres schema, Redis caching and the rollout, and watch latency and throughput after release.
You will work on distributed systems problems such as retries and idempotency across microservices on AWS with Docker, CI/CD and observability. You will pair program with senior engineers and have a mentor. Open to students graduating in 2028. This is a 6 month internship in Bengaluru, India.`

// vagueRemote leaves eligibility unknown for code (a remote role that names no country and no graduation year).
const vagueRemote = `Join our small research team as an intern. The work is on evaluating retrieval pipelines and writing the evaluation harness in Python. You will read papers and run experiments each week with the team.
The team is fully remote across several time zones. Applicants should be enrolled in a degree programme in computer science or a related field. Strong candidates will be considered.`

func TestOpportunityAssessAssessesNewAndChangedListingsOnce(t *testing.T) {
	store := newMemAssess()
	strong := store.add("Razorpay", "Backend Engineer Intern", "Bengaluru, India", "₹ 40,000 /month", strongDesc, domain.SourceKindEmployerATS)
	pledge := store.add("Pledge India Foundation", "Software Development Intern", "Remote (India)", "Unpaid", "", domain.SourceKindAggregator)
	o := &OpportunityAssess{Store: store}
	owner := uuid.New()

	res, err := o.Run(context.Background(), owner, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Assessed != 2 || len(store.assessments) != 2 {
		t.Fatalf("both listings are assessed: %+v", res)
	}
	a := store.assessments[strong.ID]
	if a.ValueClass != domain.ValueExceptional && a.ValueClass != domain.ValueStrong {
		t.Fatalf("the strong role: %d %s", a.ValueScore, a.ValueClass)
	}
	if a.UserID != owner || a.ListingID != strong.ID || a.InputHash == "" || a.AssessmentKey != domain.AssessmentKey(strong) {
		t.Fatalf("keys not filled in: %+v", a)
	}
	p := store.assessments[pledge.ID]
	if p.Eligibility != domain.EligibilityUnknown || p.CompensationMonthlyINR == nil || *p.CompensationMonthlyINR != 0 {
		t.Fatalf("pledge: unknown eligibility (review), unpaid: %+v", p)
	}

	// Nothing changed: nothing is assessed again.
	again, _ := o.Run(context.Background(), owner, "")
	if again.Assessed != 0 {
		t.Fatalf("an unchanged listing is not assessed again: %+v", again)
	}
	// A changed description is.
	store.setDescription(strong, strongDesc+" We also use Kafka.")
	changed, _ := o.Run(context.Background(), owner, "")
	if changed.Assessed != 1 {
		t.Fatalf("a changed description is assessed again: %+v", changed)
	}
	// A closed listing is not.
	pledge.Stage = domain.StageClosed
	delete(store.assessments, pledge.ID)
	closed, _ := o.Run(context.Background(), owner, "")
	if closed.Assessed != 0 {
		t.Fatalf("closed listings are left alone: %+v", closed)
	}
}

func TestOpportunityAssessSummaryListsClassesReviewAndTopFive(t *testing.T) {
	store := newMemAssess()
	for i, co := range []string{"Razorpay", "Postman", "Juspay", "Groww", "Zerodha", "Swiggy", "Acme"} {
		store.add(co, "Backend Engineer Intern "+strings.Repeat("I", i+1), "Bengaluru, India", "₹ 30,000 /month", strongDesc, domain.SourceKindEmployerATS)
	}
	store.add("Pledge India Foundation", "Software Development Intern", "Remote (India)", "Unpaid", "", domain.SourceKindAggregator)
	store.add("Acme Cloud", "Backend Intern", "Remote", "", "This is a US only role. "+strongDesc, domain.SourceKindEmployerATS)
	res, err := (&OpportunityAssess{Store: store}).Run(context.Background(), uuid.New(), "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Assessed != 9 || res.Review != 1 || res.Ineligible != 1 || len(res.Top) != 5 {
		t.Fatalf("%+v", res)
	}
	total := 0
	for _, n := range res.Classes {
		total += n
	}
	if total != 9 {
		t.Fatalf("every assessed listing is in one class: %v", res.Classes)
	}
	for _, top := range res.Top {
		if top.Eligibility == domain.EligibilityIneligible || top.Why == "" {
			t.Fatalf("an ineligible listing is never a top pick, and every pick says why: %+v", top)
		}
	}
	s := res.Summary()
	for _, want := range []string{"Assessed 9 listings", "Model calls: 0", "exceptional", "1 need your review", "1 are not open to you", "Top listings:", "1. Backend Engineer Intern"} {
		if !strings.Contains(s, want) {
			t.Errorf("the summary lacks %q:\n%s", want, s)
		}
	}
}

func TestOpportunityAssessModelBudgetAndCache(t *testing.T) {
	store := newMemAssess()
	var calls int32
	reply := func(context.Context, string, string) (string, error) {
		atomic.AddInt32(&calls, 1)
		b, _ := json.Marshal(map[string]any{"eligibility": "likely", "eligibility_reasons": []map[string]string{{"reason": "open to degree students", "quote": "enrolled in a degree programme in computer science"}}})
		return string(b), nil
	}
	var listings []*domain.Listing
	for i := 0; i < 5; i++ {
		l := store.add("Lab "+strings.Repeat("x", i+1), "Research Intern", "Remote", "", vagueRemote+" Lab number "+strings.Repeat("z", i+1)+".", domain.SourceKindEmployerATS)
		listings = append(listings, l)
	}
	o := &OpportunityAssess{Store: store, Model: reply, ModelName: "gemini-flash", MaxModelCalls: 3}
	res, _ := o.Run(context.Background(), uuid.New(), "")
	if res.ModelCalls != 3 || atomic.LoadInt32(&calls) != 3 || res.Assessed != 5 {
		t.Fatalf("the model is asked at most 3 times a run, and every listing is still assessed: %+v calls=%d", res, calls)
	}
	asked := 0
	for _, l := range listings {
		if a := store.assessments[l.ID]; a.ModelKey != nil {
			asked++
			if a.Eligibility != domain.EligibilityLikely || a.ModelUsed == nil {
				t.Fatalf("a verified answer is merged: %+v", a)
			}
		} else if a.Eligibility != domain.EligibilityUnknown {
			t.Fatalf("a listing the model was not asked about stays unknown: %+v", a)
		}
	}
	if asked != 3 {
		t.Fatalf("asked=%d", asked)
	}

	// A changed rule version re-assesses everything but never asks about a description twice; the two that were
	// not asked are asked now, under the remaining budget.
	for _, a := range store.assessments {
		a.Version = 0
	}
	res2, _ := o.Run(context.Background(), uuid.New(), "")
	if res2.Assessed != 5 || res2.ModelCalls != 2 || atomic.LoadInt32(&calls) != 5 {
		t.Fatalf("only the two unasked descriptions cost a call: %+v calls=%d", res2, calls)
	}
}

func TestOpportunityAssessStopsAskingWhenTheModelIsDown(t *testing.T) {
	store := newMemAssess()
	for i := 0; i < 4; i++ {
		store.add("Lab "+strings.Repeat("x", i+1), "Research Intern", "Remote", "", vagueRemote+" Lab "+strings.Repeat("z", i+1)+".", domain.SourceKindEmployerATS)
	}
	var calls int32
	down := func(context.Context, string, string) (string, error) {
		atomic.AddInt32(&calls, 1)
		return "", context.DeadlineExceeded
	}
	res, _ := (&OpportunityAssess{Store: store, Model: down}).Run(context.Background(), uuid.New(), "")
	if calls != 1 || res.Assessed != 4 || res.ModelErrors != 1 || len(res.Problems) == 0 {
		t.Fatalf("one failed call ends the asking, every listing is still assessed: %+v calls=%d", res, calls)
	}
}

func TestOpportunityAssessStopsAtItsTimeBudget(t *testing.T) {
	store := newMemAssess()
	for i := 0; i < 6; i++ {
		store.add("Co"+strings.Repeat("x", i+1), "Backend Intern", "Pune, India", "", strongDesc+strings.Repeat(" more", i), domain.SourceKindEmployerATS)
	}
	clock := time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC)
	o := &OpportunityAssess{Store: store, Budget: time.Minute, Now: func() time.Time { clock = clock.Add(25 * time.Second); return clock }}
	res, _ := o.Run(context.Background(), uuid.New(), "")
	if res.Assessed == 0 || res.Assessed >= 6 || res.Remaining != 6-res.Assessed {
		t.Fatalf("a run inside its budget assesses some and leaves the rest for the next run: %+v", res)
	}
	if !strings.Contains(res.Summary(), "more wait for the next run") {
		t.Fatalf("the summary says some are left: %s", res.Summary())
	}
}

func TestOpportunityAssessRoutineRunsInCode(t *testing.T) {
	store := newMemAssess()
	store.add("Razorpay", "Backend Engineer Intern", "Bengaluru, India", "₹ 40,000 /month", strongDesc, domain.SourceKindEmployerATS)
	w := &TrackerLists{Assess: &OpportunityAssess{Store: store}}
	answer, how := w.RunInCode(context.Background(), &domain.Task{UserID: uuid.New(), Title: OpportunityAssessTitle}, nil, nil)
	if how != Answered || !strings.Contains(answer, "Assessed 1 listings") {
		t.Fatalf("%v %s", how, answer)
	}
	if _, how := (&TrackerLists{}).RunInCode(context.Background(), &domain.Task{UserID: uuid.New(), Title: OpportunityAssessTitle}, nil, nil); how != NotInCode {
		t.Fatal("with no routine wired, the task is left to the normal path")
	}
	if !domain.IsRoutine(&domain.Task{Title: OpportunityAssessTitle}) {
		t.Fatal("the assess task is a routine and is never archived by age")
	}
}

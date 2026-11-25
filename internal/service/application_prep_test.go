package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/assess"
	"github.com/griffin/core/internal/domain"
)

const prepProfile = `# Owner profile

- Full name: Asha Verma
- GitHub: https://github.com/asha-v
- Education: B.Tech Computer Science, Northfield Institute of Technology
- Expected graduation: 2028

## Verified facts

- [f1] ComputeHive: built a distributed task runner in Go with gRPC and Postgres
- [f2] Palni: interned as an AI Engineer building LLM apps with RAG
`

// A posting that asks nothing the profile cannot answer, so its package is complete.
const completePosting = "Backend Engineering Intern. You will build Go services backed by Postgres and work on distributed systems with a small team in Bengaluru. Experience with gRPC is a plus. Please share your GitHub profile."

const prepLetter = "Dear Hiring Team,\n\nI am applying for the Backend Engineering Intern role at Acme Technologies. I am a B.Tech Computer Science student at Northfield Institute of Technology, expected to graduate in 2028. " +
	"I built ComputeHive, a distributed task runner in Go with gRPC and Postgres. That is the kind of service work your posting describes, and I would enjoy doing it on your team.\n\nThank you for considering my application.\n\nSincerely,\nAsha Verma"

// memPrep is an in-memory PrepStore.
type memPrep struct {
	mu       sync.Mutex
	rows     map[uuid.UUID]*domain.Listing
	desc     map[uuid.UUID]string
	pkgs     map[uuid.UUID]*domain.ApplicationPackage // by listing
	moves    []string
	saveFail bool
}

func newMemPrep() *memPrep {
	return &memPrep{rows: map[uuid.UUID]*domain.Listing{}, desc: map[uuid.UUID]string{}, pkgs: map[uuid.UUID]*domain.ApplicationPackage{}}
}

func (m *memPrep) add(company, class, eligibility string, score int, posting string) *domain.Listing {
	h := domain.DescriptionHash(posting)
	l := &domain.Listing{ID: uuid.New(), UserID: uuid.New(), Company: company, Role: "Backend Engineering Intern", Location: "Bengaluru", URL: "https://" + company + ".example/job",
		Stage: domain.StageVerified, DescriptionHash: &h, CreatedAt: time.Now()}
	l.Assessment = &domain.Assessment{ListingID: l.ID, RoleClass: domain.RoleBackend, Eligibility: eligibility, ValueClass: class, ValueScore: score}
	m.rows[l.ID] = l
	m.desc[l.ID] = posting
	return l
}

func (m *memPrep) ListAssessed(_ context.Context, _ uuid.UUID, f domain.AssessedFilter) ([]*domain.Listing, error) {
	in := func(list []string, v string) bool {
		if len(list) == 0 {
			return true
		}
		for _, x := range list {
			if x == v {
				return true
			}
		}
		return false
	}
	var out []*domain.Listing
	for _, l := range m.rows {
		if in(f.Stages, l.Stage) && in(f.Classes, l.Assessment.ValueClass) && in(f.Eligibility, l.Assessment.Eligibility) {
			out = append(out, l)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Assessment.ValueScore > out[j].Assessment.ValueScore })
	return out, nil
}

func (m *memPrep) Get(_ context.Context, _, id uuid.UUID) (*domain.Listing, error) {
	if l := m.rows[id]; l != nil {
		cp := *l
		return &cp, nil
	}
	return nil, domain.ErrNotFound
}

func (m *memPrep) Move(_ context.Context, _, id uuid.UUID, from, to, note string, _ *uuid.UUID) error {
	l := m.rows[id]
	if l == nil || l.Stage != from || !domain.CanMove(from, to) {
		return errors.New("illegal move " + from + " to " + to)
	}
	l.Stage, l.StageNote = to, note
	m.moves = append(m.moves, from+">"+to)
	return nil
}

func (m *memPrep) ListingDescription(_ context.Context, _, id uuid.UUID) (string, error) {
	return m.desc[id], nil
}

func (m *memPrep) GetAssessment(_ context.Context, _, id uuid.UUID) (*domain.Assessment, error) {
	return m.rows[id].Assessment, nil
}

func (m *memPrep) PackageFor(_ context.Context, _, listingID uuid.UUID) (*domain.ApplicationPackage, error) {
	if p := m.pkgs[listingID]; p != nil {
		cp := *p
		return &cp, nil
	}
	return nil, nil
}

func (m *memPrep) GetPackage(_ context.Context, _, id uuid.UUID) (*domain.ApplicationPackage, error) {
	for _, p := range m.pkgs {
		if p.ID == id {
			cp := *p
			return &cp, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (m *memPrep) SavePackage(_ context.Context, p *domain.ApplicationPackage) error {
	if m.saveFail {
		return errors.New("disk full")
	}
	if old := m.pkgs[p.ListingID]; old != nil {
		p.ID = old.ID
	} else {
		p.ID = uuid.New()
	}
	p.ApprovalID, p.TaskID = nil, nil
	cp := *p
	m.pkgs[p.ListingID] = &cp
	return nil
}

func (m *memPrep) HoldPackage(_ context.Context, _, id, approvalID, taskID uuid.UUID) error {
	for _, p := range m.pkgs {
		if p.ID == id && (p.Status == domain.PackageDraft || p.Status == domain.PackageNeedsOwner) {
			p.Status, p.ApprovalID, p.TaskID = domain.PackageReadyForApproval, &approvalID, &taskID
			return nil
		}
	}
	return domain.ErrConflict
}

func (m *memPrep) SetPackageStatus(_ context.Context, _, id uuid.UUID, from []string, to string) error {
	for _, p := range m.pkgs {
		if p.ID != id {
			continue
		}
		for _, f := range from {
			if p.Status == f {
				p.Status = to
				return nil
			}
		}
	}
	return domain.ErrConflict
}

func (m *memPrep) DeletePackage(_ context.Context, _, id uuid.UUID) error {
	for k, p := range m.pkgs {
		if p.ID == id {
			delete(m.pkgs, k)
		}
	}
	return nil
}

func (m *memPrep) PackageBriefs(_ context.Context, _ uuid.UUID) (map[uuid.UUID]*domain.PackageBrief, error) {
	out := map[uuid.UUID]*domain.PackageBrief{}
	for k, p := range m.pkgs {
		out[k] = &domain.PackageBrief{ID: p.ID, Status: p.Status, ApprovalID: p.ApprovalID, Missing: len(p.MissingInformation), ListingID: k, DescriptionHash: p.DescriptionHash, ProfileHash: p.ProfileHash}
	}
	return out, nil
}

// fakeHolder stands in for the task engine: it records what would be put on the approvals page.
type fakeHolder struct {
	held []*domain.ApplicationPackage
	fail bool
}

func (h *fakeHolder) HoldApplicationPackage(_ context.Context, _ *domain.Listing, p *domain.ApplicationPackage) (uuid.UUID, uuid.UUID, error) {
	if h.fail {
		return uuid.Nil, uuid.Nil, errors.New("approvals are down")
	}
	h.held = append(h.held, p)
	return uuid.New(), uuid.New(), nil
}

// countingModel always writes the grounded letter and counts the calls; each call advances the clock by cost.
type countingModel struct {
	calls int
	cost  time.Duration
	clock *time.Time
	err   error
}

func (c *countingModel) fn() assess.ModelFunc {
	return func(_ context.Context, _, user string) (string, error) {
		c.calls++
		if c.clock != nil {
			*c.clock = c.clock.Add(c.cost)
		}
		if c.err != nil {
			return "", c.err
		}
		company := "Acme Technologies"
		if i := strings.Index(user, "COMPANY: "); i >= 0 {
			company = strings.SplitN(user[i+len("COMPANY: "):], "\n", 2)[0]
		}
		letter := strings.ReplaceAll(prepLetter, "Acme Technologies", company)
		return `{"cover_letter": "` + strings.NewReplacer("\n", `\n`, `"`, `\"`).Replace(letter) + `", "why_join": ""}`, nil
	}
}

func newPrep(store *memPrep, model *countingModel, holder *fakeHolder) *ApplicationPrep {
	o := &ApplicationPrep{Store: store, Holder: holder, ModelName: "fake-flash", Facts: func(context.Context, uuid.UUID) string { return prepProfile }}
	if model != nil {
		o.Model = model.fn()
	}
	return o
}

func TestPrepareMovesTheListingToDraftedAndAsksForApproval(t *testing.T) {
	store, holder, model := newMemPrep(), &fakeHolder{}, &countingModel{}
	l := store.add("Acme Technologies", domain.ValueStrong, domain.EligibilityConfirmed, 80, completePosting)
	o := newPrep(store, model, holder)

	pkg, err := o.Prepare(context.Background(), l.UserID, l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Status != domain.PackageReadyForApproval || pkg.ApprovalID == nil || len(holder.held) != 1 {
		t.Fatalf("a complete package goes to the approvals page: %s held=%d", pkg.Status, len(holder.held))
	}
	if store.rows[l.ID].Stage != domain.StageDrafted {
		t.Fatalf("creating a package moves verified to drafted, got %s", store.rows[l.ID].Stage)
	}
	if model.calls != 1 {
		t.Fatalf("one model call for a letter that passes, got %d", model.calls)
	}
	// Asking again changes nothing and costs nothing.
	again, err := o.Prepare(context.Background(), l.UserID, l.ID)
	if err != nil || again.ID != pkg.ID || model.calls != 1 || len(holder.held) != 1 {
		t.Fatalf("a package that waits for the owner is left alone: err=%v calls=%d held=%d", err, model.calls, len(holder.held))
	}
}

func TestPrepareKeepsAnIncompletePackageOffTheApprovalsPage(t *testing.T) {
	store, holder, model := newMemPrep(), &fakeHolder{}, &countingModel{}
	posting := completePosting + " What is your notice period? Please attach your transcript."
	l := store.add("Acme Technologies", domain.ValueStrong, domain.EligibilityConfirmed, 80, posting)
	pkg, err := newPrep(store, model, holder).Prepare(context.Background(), l.UserID, l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Status != domain.PackageNeedsOwner || len(holder.held) != 0 || pkg.ApprovalID != nil {
		t.Fatalf("missing information must stop the package before approval: %s", pkg.Status)
	}
	if store.rows[l.ID].Stage != domain.StageDrafted || len(pkg.MissingInformation) < 2 {
		t.Fatalf("stage=%s missing=%v", store.rows[l.ID].Stage, pkg.MissingInformation)
	}
}

func TestPackageStageMovesAreLegalOrRefused(t *testing.T) {
	// The pipeline the package drives.
	for _, m := range [][2]string{{domain.StageVerified, domain.StageDrafted}, {domain.StageDrafted, domain.StageApproved}, {domain.StageDrafted, domain.StageVerified}, {domain.StageNeedsYou, domain.StageDrafted}} {
		if !domain.CanMove(m[0], m[1]) {
			t.Fatalf("%s to %s must be legal", m[0], m[1])
		}
	}
	for _, m := range [][2]string{{domain.StageVerified, domain.StageApproved}, {domain.StageVerified, domain.StageSubmitting}, {domain.StageApproved, domain.StageDrafted}, {domain.StageConfirmed, domain.StageDrafted}, {domain.StageClosed, domain.StageDrafted}} {
		if domain.CanMove(m[0], m[1]) {
			t.Fatalf("%s to %s must be illegal", m[0], m[1])
		}
	}
	// A listing already past drafted (or closed) cannot get a package, and no model is asked.
	for _, stage := range []string{domain.StageApproved, domain.StageSubmitting, domain.StageConfirmed, domain.StageClosed} {
		store, model := newMemPrep(), &countingModel{}
		l := store.add("Acme Technologies", domain.ValueStrong, domain.EligibilityConfirmed, 80, completePosting)
		l.Stage = stage
		_, err := newPrep(store, model, &fakeHolder{}).Prepare(context.Background(), l.UserID, l.ID)
		if !errors.Is(err, domain.ErrConflict) || model.calls != 0 || len(store.pkgs) != 0 {
			t.Fatalf("%s: err=%v calls=%d pkgs=%d", stage, err, model.calls, len(store.pkgs))
		}
	}
}

func TestPrepareStoresNothingWhenTheModelFails(t *testing.T) {
	store, holder := newMemPrep(), &fakeHolder{}
	model := &countingModel{err: errors.New("503")}
	l := store.add("Acme Technologies", domain.ValueStrong, domain.EligibilityConfirmed, 80, completePosting)
	_, err := newPrep(store, model, holder).Prepare(context.Background(), l.UserID, l.ID)
	if !errors.Is(err, ErrModelUnavailable) || len(store.pkgs) != 0 || store.rows[l.ID].Stage != domain.StageVerified {
		t.Fatalf("a failed model leaves everything as it was: err=%v pkgs=%d stage=%s", err, len(store.pkgs), store.rows[l.ID].Stage)
	}
}

func TestDecidingOnAPackageMovesTheListingAndNeverSendsByItself(t *testing.T) {
	ctx := context.Background()
	setup := func() (*memPrep, *ApplicationPrep, *domain.Listing, *domain.ApplicationPackage, *domain.Approval) {
		store, holder := newMemPrep(), &fakeHolder{}
		l := store.add("Acme Technologies", domain.ValueStrong, domain.EligibilityConfirmed, 80, completePosting)
		o := newPrep(store, &countingModel{}, holder)
		pkg, err := o.Prepare(ctx, l.UserID, l.ID)
		if err != nil || pkg.Status != domain.PackageReadyForApproval {
			t.Fatalf("setup: %v %s", err, pkg.Status)
		}
		return store, o, l, pkg, &domain.Approval{UserID: l.UserID, TaskID: uuid.New(), ActionType: domain.PackageApprovalAction, ActionPayload: domain.PackageApprovalPayload(l, pkg)}
	}

	t.Run("approve", func(t *testing.T) {
		store, o, l, pkg, a := setup()
		var submits []*domain.Approval
		o.Decided(ctx, a, true, func(_ context.Context, s *domain.Approval) { submits = append(submits, s) })
		if store.rows[l.ID].Stage != domain.StageApproved || store.pkgs[l.ID].Status != domain.PackageApproved {
			t.Fatalf("approving moves drafted to approved: %s / %s", store.rows[l.ID].Stage, store.pkgs[l.ID].Status)
		}
		if len(submits) != 1 || !strings.Contains(submits[0].ActionPayload.String(), "Dear Hiring Team") || !strings.Contains(submits[0].ActionPayload.String(), pkg.ResumeFile) {
			t.Fatalf("the submit path gets the approved text and the resume: %v", submits)
		}
		// A decision is applied once.
		o.Decided(ctx, a, true, func(context.Context, *domain.Approval) { t.Fatal("decided twice") })
		o.Decided(ctx, a, false, func(context.Context, *domain.Approval) {})
		if store.rows[l.ID].Stage != domain.StageApproved {
			t.Fatal("a second decision must not undo the first")
		}
	})
	t.Run("reject", func(t *testing.T) {
		store, o, l, _, a := setup()
		o.Decided(ctx, a, false, func(context.Context, *domain.Approval) { t.Fatal("a rejected package must not start a submission") })
		if store.rows[l.ID].Stage != domain.StageVerified || store.pkgs[l.ID].Status != domain.PackageRejected {
			t.Fatalf("rejecting moves the listing back to verified: %s / %s", store.rows[l.ID].Stage, store.pkgs[l.ID].Status)
		}
	})
	t.Run("approve for a listing that closed meanwhile", func(t *testing.T) {
		store, o, l, _, a := setup()
		store.rows[l.ID].Stage = domain.StageClosed
		o.Decided(ctx, a, true, func(context.Context, *domain.Approval) { t.Fatal("nothing may be submitted for a closed listing") })
		if store.pkgs[l.ID].Status != domain.PackageRejected {
			t.Fatalf("status %s", store.pkgs[l.ID].Status)
		}
	})
}

func TestRunPreparesOnlyTheTopEligibleListingsUpToFive(t *testing.T) {
	store, holder, model := newMemPrep(), &fakeHolder{}, &countingModel{}
	for i := 0; i < 8; i++ {
		store.add("Strong"+string(rune('A'+i)), domain.ValueStrong, domain.EligibilityLikely, 90-i, completePosting)
	}
	store.add("Exceptional Co", domain.ValueExceptional, domain.EligibilityConfirmed, 99, completePosting)
	good := store.add("Good Co", domain.ValueGood, domain.EligibilityConfirmed, 60, completePosting)
	unknown := store.add("Unknown Elig Co", domain.ValueExceptional, domain.EligibilityUnknown, 98, completePosting)
	inelig := store.add("Ineligible Co", domain.ValueExceptional, domain.EligibilityIneligible, 97, completePosting)

	res, err := newPrep(store, model, holder).Run(context.Background(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Prepared) != 5 || model.calls != 5 || res.Remaining < 4 {
		t.Fatalf("at most five a run, one call each: prepared=%d calls=%d remaining=%d", len(res.Prepared), model.calls, res.Remaining)
	}
	if res.Prepared[0].Company != "Exceptional Co" {
		t.Fatalf("best first: %v", res.Prepared)
	}
	for _, l := range []*domain.Listing{good, unknown, inelig} {
		if store.pkgs[l.ID] != nil {
			t.Fatalf("%s is not exceptional/strong and confirmed/likely: no package", l.Company)
		}
	}
	// The next run carries on with the rest and never repeats a package.
	res2, err := newPrep(store, model, holder).Run(context.Background(), uuid.New())
	if err != nil || len(res2.Prepared) != 4 || model.calls != 9 {
		t.Fatalf("second run: %v prepared=%d calls=%d", err, len(res2.Prepared), model.calls)
	}
	res3, _ := newPrep(store, model, holder).Run(context.Background(), uuid.New())
	if len(res3.Prepared) != 0 || model.calls != 9 || !strings.Contains(res3.Summary(), "Nothing was sent") {
		t.Fatalf("third run has nothing to do: %+v", res3)
	}
}

func TestRunStopsStartingPackagesWhenLessThanSixSecondsRemain(t *testing.T) {
	clock := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	store, holder := newMemPrep(), &fakeHolder{}
	model := &countingModel{cost: 10 * time.Second, clock: &clock} // each package costs 10s of a 24s budget
	for i := 0; i < 5; i++ {
		store.add("Strong"+string(rune('A'+i)), domain.ValueStrong, domain.EligibilityConfirmed, 90-i, completePosting)
	}
	o := newPrep(store, model, holder)
	o.Now = func() time.Time { return clock }

	res, err := o.Run(context.Background(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	// start at 0s (24s left) and 10s (14s left); at 20s only 4s remain, so nothing new starts.
	if len(res.Prepared) != 2 || res.Remaining != 3 || model.calls != 2 {
		t.Fatalf("prepared=%d remaining=%d calls=%d", len(res.Prepared), res.Remaining, model.calls)
	}
	if !strings.Contains(res.Summary(), "3 more wait for the next run") {
		t.Fatalf("summary: %s", res.Summary())
	}
	// Resume: a fresh run does the rest, and does not redo the two.
	clock = clock.Add(24 * time.Hour)
	res2, err := o.Run(context.Background(), uuid.New())
	if err != nil || len(res2.Prepared) != 2 || res2.Remaining != 1 || model.calls != 4 {
		t.Fatalf("resume: err=%v prepared=%d remaining=%d calls=%d", err, len(res2.Prepared), res2.Remaining, model.calls)
	}
}

func TestRunStartsNothingWhenTheRequestDeadlineIsNear(t *testing.T) {
	store, holder, model := newMemPrep(), &fakeHolder{}, &countingModel{}
	store.add("Strong A", domain.ValueStrong, domain.EligibilityConfirmed, 90, completePosting)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second) // under the 6s reserve
	defer cancel()
	res, err := newPrep(store, model, holder).Run(ctx, uuid.New())
	if err != nil || len(res.Prepared) != 0 || res.Remaining != 1 || model.calls != 0 {
		t.Fatalf("err=%v prepared=%d remaining=%d calls=%d", err, len(res.Prepared), res.Remaining, model.calls)
	}
}

func TestRunAsksForApprovalAgainWithoutAModelAndRefreshesStalePackages(t *testing.T) {
	store, holder, model := newMemPrep(), &fakeHolder{fail: true}, &countingModel{}
	l := store.add("Strong A", domain.ValueStrong, domain.EligibilityConfirmed, 90, completePosting)
	o := newPrep(store, model, holder)

	res, err := o.Run(context.Background(), uuid.New())
	if err != nil || store.pkgs[l.ID].Status != domain.PackageDraft || len(res.Problems) == 0 {
		t.Fatalf("approvals were down: the package stays a draft and the problem is reported: %v %+v", err, res)
	}
	holder.fail = false
	calls := model.calls
	res, err = o.Run(context.Background(), uuid.New())
	if err != nil || res.Held != 1 || model.calls != calls || store.pkgs[l.ID].Status != domain.PackageReadyForApproval {
		t.Fatalf("the next run asks again without the model: %v held=%d calls=%d->%d", err, res.Held, calls, model.calls)
	}

	// A package that needs the owner is made again once the profile changes (and only then).
	store2, holder2, model2 := newMemPrep(), &fakeHolder{}, &countingModel{}
	l2 := store2.add("Strong B", domain.ValueStrong, domain.EligibilityConfirmed, 90, completePosting+" What is your notice period?")
	profile := prepProfile
	o2 := newPrep(store2, model2, holder2)
	o2.Facts = func(context.Context, uuid.UUID) string { return profile }
	if _, err := o2.Run(context.Background(), uuid.New()); err != nil || store2.pkgs[l2.ID].Status != domain.PackageNeedsOwner {
		t.Fatalf("setup: %v", err)
	}
	if _, err := o2.Run(context.Background(), uuid.New()); err != nil || model2.calls != 1 {
		t.Fatalf("an unchanged package that needs the owner is left alone: calls=%d", model2.calls)
	}
	profile += "- Notice period: none, I am a student\n"
	if _, err := o2.Run(context.Background(), uuid.New()); err != nil || model2.calls != 2 {
		t.Fatalf("a changed profile prepares it again: calls=%d", model2.calls)
	}
	if got := store2.pkgs[l2.ID]; got.Status != domain.PackageReadyForApproval {
		t.Fatalf("the notice period is answered from the profile now: %s %v", got.Status, got.MissingInformation)
	}
}

func TestRunStopsAskingTheModelOnceItIsDown(t *testing.T) {
	store, holder := newMemPrep(), &fakeHolder{}
	model := &countingModel{err: errors.New("503")}
	for i := 0; i < 3; i++ {
		store.add("Strong"+string(rune('A'+i)), domain.ValueStrong, domain.EligibilityConfirmed, 90-i, completePosting)
	}
	res, err := newPrep(store, model, holder).Run(context.Background(), uuid.New())
	if err != nil || model.calls != 1 || len(store.pkgs) != 0 || len(res.Problems) != 1 || res.Remaining != 2 {
		t.Fatalf("err=%v calls=%d pkgs=%d problems=%v remaining=%d", err, model.calls, len(store.pkgs), res.Problems, res.Remaining)
	}
}

func TestTheRoutineAndItsPackageTaskRunInCode(t *testing.T) {
	store, holder, model := newMemPrep(), &fakeHolder{}, &countingModel{}
	l := store.add("Strong A", domain.ValueStrong, domain.EligibilityConfirmed, 90, completePosting)
	lists := &TrackerLists{Prep: newPrep(store, model, holder)}
	task := &domain.Task{UserID: l.UserID, Title: "APPLICATION PREP - daily"}
	answer, outcome := lists.RunInCode(context.Background(), task, nil, nil)
	if outcome != Answered || !strings.Contains(answer, "Prepared 1 application packages") || !strings.Contains(answer, "Nothing was sent") {
		t.Fatalf("%v %q", outcome, answer)
	}
	if _, outcome := lists.RunInCode(context.Background(), &domain.Task{Title: domain.ApplicationPackageTaskPrefix + "Acme - Intern"}, nil, nil); outcome != Answered {
		t.Fatal("a held package's task must be answered by code, never given to a model")
	}
	if !domain.IsRoutine(task) {
		t.Fatal("APPLICATION PREP is a routine: it is never archived by age")
	}
}

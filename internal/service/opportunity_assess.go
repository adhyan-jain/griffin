package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/assess"
	budgetpkg "github.com/griffin/core/internal/budget"
	"github.com/griffin/core/internal/domain"
)

// OpportunityAssessTitle is the routine that assesses new and changed open listings, in code.
const OpportunityAssessTitle = domain.OpportunityAssessTitle

// IsOpportunityAssess reports whether a task is that routine.
func IsOpportunityAssess(title string) bool {
	return strings.Contains(strings.ToUpper(title), OpportunityAssessTitle)
}

// AssessStore is what the assess routine needs from the database.
type AssessStore interface {
	AssessmentSubjects(ctx context.Context, userID uuid.UUID) ([]domain.AssessmentSubject, error)
	ListingDescription(ctx context.Context, userID, id uuid.UUID) (string, error)
	GetAssessment(ctx context.Context, userID, listingID uuid.UUID) (*domain.Assessment, error)
	SaveAssessment(ctx context.Context, a *domain.Assessment, sourceURL string) error
	ListAssessed(ctx context.Context, userID uuid.UUID, f domain.AssessedFilter) ([]*domain.Listing, error)
}

// Defaults for one run of the routine.
const (
	// DefaultMaxModelCalls bounds the model calls in one run. A listing is asked about at most once per description.
	DefaultMaxModelCalls = 40
	// DefaultAssessBudget bounds how long a run works when its context has no deadline (the cloud worker). A run
	// inside a host claim works to that request's deadline instead: see internal/budget.
	DefaultAssessBudget = 4 * time.Minute
)

// modelCallTime is the least time left for a run to start a model call, which takes a few seconds (a variable so a
// test can shrink it).
var modelCallTime = 8 * time.Second

// OpportunityAssess assesses the owner's open listings: those with no assessment, whose description or facts have
// changed, or that predate the current rules. Code decides what it can; a model is asked (at most MaxModelCalls
// times a run, once per listing and description) only for what code cannot, and only its quote-verified conclusions
// are kept.
type OpportunityAssess struct {
	Store AssessStore
	// Model is the model to ask (Gemini Flash); nil assesses by code alone.
	Model         assess.ModelFunc
	ModelName     string
	MaxModelCalls int
	// Budget bounds a run whose context has no deadline; with a deadline, the run works to it.
	Budget time.Duration
	Now    func() time.Time
}

// AssessResult counts what one run did.
type AssessResult struct {
	Assessed    int // listings assessed this run
	Remaining   int // stale listings left for the next run (time budget)
	ModelCalls  int
	ModelErrors int
	// Classes counts the open listings by value class; Review counts those whose eligibility is unknown (including
	// listings not assessed yet); Ineligible those the owner may not apply to.
	Classes    map[string]int
	Review     int
	Ineligible int
	Top        []AssessedTop
	Problems   []string
}

// AssessedTop is one of the best open listings, with its one-line reason.
type AssessedTop struct {
	Company, Role, Class, Eligibility, Why string
	Score                                  int
}

func openStages() []string {
	return []string{domain.StageVerified, domain.StageDrafted, domain.StageApproved, domain.StageSubmitting, domain.StageNeedsYou}
}

func listingPay(l *domain.Listing) string {
	if l.Pay != "" {
		return l.Pay
	}
	if l.CompensationText != nil {
		return *l.CompensationText
	}
	return ""
}

// Run assesses one owner's stale open listings.
func (o *OpportunityAssess) Run(ctx context.Context, userID uuid.UUID, ownerText string) (AssessResult, error) {
	res := AssessResult{Classes: map[string]int{}}
	if o == nil || o.Store == nil {
		return res, fmt.Errorf("opportunity assess: no store")
	}
	now := time.Now
	if o.Now != nil {
		now = o.Now
	}
	budget, maxCalls := o.Budget, o.MaxModelCalls
	if budget <= 0 {
		budget = DefaultAssessBudget
	}
	if maxCalls <= 0 {
		maxCalls = DefaultMaxModelCalls
	}
	// The run works to its context's deadline when it has one (a claim request, which the host platform kills at a
	// fixed time): it stops starting listings before it, and cuts model calls off before it so the writes still fit.
	work, cancel := budgetpkg.Work(ctx)
	defer cancel()
	_, timed := ctx.Deadline()
	deadline := now().Add(budget)
	timeUp := func(needed time.Duration) bool {
		if timed {
			return budgetpkg.Left(work) < needed
		}
		return ctx.Err() != nil || now().After(deadline)
	}

	subjects, err := o.Store.AssessmentSubjects(ctx, userID)
	if err != nil {
		return res, err
	}
	modelUp := o.Model != nil
	for i, s := range subjects {
		if !s.Stale() {
			continue
		}
		needed := budgetpkg.Reserve
		if modelUp && res.ModelCalls < maxCalls {
			needed = modelCallTime
		}
		if timeUp(needed) {
			for _, rest := range subjects[i:] {
				if rest.Stale() {
					res.Remaining++
				}
			}
			break
		}
		l := s.Listing
		desc, err := o.Store.ListingDescription(ctx, userID, l.ID)
		if err != nil {
			res.Problems = append(res.Problems, fmt.Sprintf("%s at %s: %v", l.Role, l.Company, err))
			continue
		}
		var prev *domain.Assessment
		if s.Assessed {
			if prev, err = o.Store.GetAssessment(ctx, userID, l.ID); err != nil {
				res.Problems = append(res.Problems, fmt.Sprintf("%s at %s: %v", l.Role, l.Company, err))
				continue
			}
		}
		in := assess.Input{
			Title: l.Role, Company: l.Company, Location: l.Location, Pay: listingPay(l), Description: desc,
			SourceKind: l.SourceKind, Verification: l.Verification, CompanyKind: s.CompanyKind,
			Key: domain.AssessmentKey(l), OwnerText: ownerText,
		}
		r := assess.Assess(work, in, prev, assess.Options{Model: o.Model, ModelName: o.ModelName, AllowModel: modelUp && res.ModelCalls < maxCalls, Now: o.Now})
		if r.CallFailed && timed && work.Err() != nil {
			// The model was cut off by the deadline, not unreachable: leave the listing for the next run, which asks
			// again, rather than save an assessment made without the answer.
			for _, rest := range subjects[i:] {
				if rest.Stale() {
					res.Remaining++
				}
			}
			res.ModelCalls++
			break
		}
		if r.ModelCalled {
			res.ModelCalls++
		}
		if r.ModelErr != nil {
			res.ModelErrors++
			if r.CallFailed {
				// The model could not be reached: stop asking for the rest of the run.
				modelUp = false
				res.Problems = append(res.Problems, "the model could not be reached: "+r.ModelErr.Error())
			}
		}
		a := r.Assessment
		a.ListingID, a.UserID, a.DescriptionHash = l.ID, userID, l.DescriptionHash
		a.InputHash = domain.AssessmentInputHash(l, s.CompanyKind)
		if err := o.Store.SaveAssessment(ctx, a, l.URL); err != nil {
			res.Problems = append(res.Problems, fmt.Sprintf("%s at %s: %v", l.Role, l.Company, err))
			continue
		}
		res.Assessed++
	}

	open, err := o.Store.ListAssessed(ctx, userID, domain.AssessedFilter{Stages: openStages(), Limit: 500})
	if err != nil {
		return res, err
	}
	for _, l := range open {
		a := l.Assessment
		switch {
		case a == nil || a.Eligibility == domain.EligibilityUnknown:
			res.Review++
			if a == nil {
				continue
			}
		case a.Eligibility == domain.EligibilityIneligible:
			res.Ineligible++
		}
		res.Classes[a.ValueClass]++
		if a.Eligibility != domain.EligibilityIneligible && len(res.Top) < 5 {
			res.Top = append(res.Top, AssessedTop{Company: l.Company, Role: l.Role, Class: a.ValueClass, Eligibility: a.Eligibility, Score: a.ValueScore, Why: assess.Explain(a)})
		}
	}
	return res, nil
}

// Summary is the owner-facing text of one run: counts per class, the review queue, and the top five with a line each.
func (r AssessResult) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Assessed %d listings", r.Assessed)
	if r.Remaining > 0 {
		fmt.Fprintf(&b, " (%d more wait for the next run)", r.Remaining)
	}
	fmt.Fprintf(&b, ". Model calls: %d.", r.ModelCalls)
	var parts []string
	for _, c := range []string{domain.ValueExceptional, domain.ValueStrong, domain.ValueGood, domain.ValueExploratory, domain.ValueLow} {
		parts = append(parts, fmt.Sprintf("%d %s", r.Classes[c], c))
	}
	fmt.Fprintf(&b, "\n\nOpen listings by value: %s. %d need your review (eligibility unknown), %d are not open to you.", strings.Join(parts, ", "), r.Review, r.Ineligible)
	if len(r.Top) > 0 {
		b.WriteString("\n\nTop listings:")
		for i, t := range r.Top {
			fmt.Fprintf(&b, "\n%d. %s at %s, %d (%s, eligibility %s): %s.", i+1, t.Role, t.Company, t.Score, t.Class, t.Eligibility, t.Why)
		}
	}
	if len(r.Problems) > 0 {
		b.WriteString("\n\nProblems: " + strings.Join(r.Problems, "; ") + ".")
	}
	return b.String()
}

// AssessOpportunities runs the assess routine for its task, in code. handled is false for any other task.
func (w *TrackerLists) AssessOpportunities(ctx context.Context, task *domain.Task) (string, bool) {
	if w == nil || w.Assess == nil || task == nil || !IsOpportunityAssess(task.Title) {
		return "", false
	}
	res, err := w.Assess.Run(ctx, task.UserID, w.ProfileText(ctx, task.UserID))
	if err != nil {
		return "", false // the normal path reports and retries it
	}
	return res.Summary(), true
}

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/assess"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/prep"
	"github.com/griffin/core/internal/repository"
)

// ApplicationPrepTitle is the routine that prepares application packages for the best eligible listings, in code.
const ApplicationPrepTitle = domain.ApplicationPrepTitle

// Defaults for one run of the routine. A code-run routine executes inside a host claim on Lambda behind a ~29s
// gateway: model calls end by the budget and nothing new starts in the last prepReserve.
const (
	DefaultPrepBudget      = 24 * time.Second
	prepReserve            = 6 * time.Second
	DefaultMaxPackagesRun  = 5
	prepCandidateLimit     = 100
	packageDecisionNoteFmt = "application package %s"
)

// PrepStore is what preparing packages needs from the database.
type PrepStore interface {
	ListAssessed(ctx context.Context, userID uuid.UUID, f domain.AssessedFilter) ([]*domain.Listing, error)
	Get(ctx context.Context, userID, id uuid.UUID) (*domain.Listing, error)
	Move(ctx context.Context, userID, id uuid.UUID, from, to, note string, taskID *uuid.UUID) error
	ListingDescription(ctx context.Context, userID, id uuid.UUID) (string, error)
	GetAssessment(ctx context.Context, userID, listingID uuid.UUID) (*domain.Assessment, error)

	PackageFor(ctx context.Context, userID, listingID uuid.UUID) (*domain.ApplicationPackage, error)
	GetPackage(ctx context.Context, userID, id uuid.UUID) (*domain.ApplicationPackage, error)
	SavePackage(ctx context.Context, p *domain.ApplicationPackage) error
	HoldPackage(ctx context.Context, userID, id, approvalID, taskID uuid.UUID) error
	SetPackageStatus(ctx context.Context, userID, id uuid.UUID, from []string, to string) error
	DeletePackage(ctx context.Context, userID, id uuid.UUID) error
	PackageBriefs(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]*domain.PackageBrief, error)
}

// PackageHolder puts a finished package on the owner's approvals page: it makes the task that carries the approval
// and the approval itself. The task engine is the real one.
type PackageHolder interface {
	HoldApplicationPackage(ctx context.Context, l *domain.Listing, p *domain.ApplicationPackage) (approvalID, taskID uuid.UUID, err error)
}

// ApplicationPrep prepares application packages. Nothing it makes is ever sent: a package waits for the owner's
// approval, and an approved one stops there unless the existing submit path (which asks again at send time) is
// started from it.
type ApplicationPrep struct {
	Store  PrepStore
	Holder PackageHolder
	// Model writes the cover letter (Gemini Flash); nil prepares what code can and lists the letter as missing.
	Model     assess.ModelFunc
	ModelName string
	// Facts returns the owner's profile text; packages are grounded in it and nothing else.
	Facts func(ctx context.Context, userID uuid.UUID) string
	// Budget bounds one routine run (DefaultPrepBudget when zero); MaxPackages the packages one run prepares.
	Budget      time.Duration
	MaxPackages int
	Now         func() time.Time
	Logger      *slog.Logger
}

func (o *ApplicationPrep) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

func (o *ApplicationPrep) log() *slog.Logger {
	if o.Logger != nil {
		return o.Logger
	}
	return slog.Default()
}

func (o *ApplicationPrep) profile(ctx context.Context, userID uuid.UUID) prep.Profile {
	if o.Facts == nil {
		return prep.Profile{}
	}
	return prep.ParseProfile(o.Facts(ctx, userID))
}

// ErrNoPrep is returned when package preparation is not configured.
var ErrNoPrep = errors.New("application preparation is not available")

// ErrModelUnavailable is returned when the model could not be reached or failed: nothing was stored.
var ErrModelUnavailable = errors.New("the model could not write the letter")

// Prepare prepares (or re-prepares) the package for one listing and puts it on the approvals page when it is
// complete. A package that is already waiting for or has the owner's decision is returned as it is: nothing is
// written again and no model is asked. It asks the model at most prep.MaxModelCalls times.
func (o *ApplicationPrep) Prepare(ctx context.Context, userID, listingID uuid.UUID) (*domain.ApplicationPackage, error) {
	if o == nil || o.Store == nil {
		return nil, ErrNoPrep
	}
	l, err := o.Store.Get(ctx, userID, listingID)
	if err != nil {
		return nil, err
	}
	existing, err := o.Store.PackageFor(ctx, userID, listingID)
	if err != nil {
		return nil, err
	}
	if existing != nil && (existing.Status == domain.PackageReadyForApproval || existing.Status == domain.PackageApproved) {
		return existing, nil // waiting for, or past, the owner's decision: whatever the listing's stage, nothing is written again
	}
	if l.Stage != domain.StageVerified && l.Stage != domain.StageDrafted && l.Stage != domain.StageNeedsYou {
		return nil, fmt.Errorf("%w: this listing is %s, so no application can be prepared for it", domain.ErrConflict, l.Stage)
	}
	if existing != nil && existing.Status == domain.PackageDraft {
		return existing, o.hold(ctx, l, existing) // the approval was never requested: ask again, no model
	}

	desc, err := o.Store.ListingDescription(ctx, userID, listingID)
	if err != nil {
		return nil, err
	}
	a, err := o.Store.GetAssessment(ctx, userID, listingID)
	if err != nil {
		return nil, err
	}
	res := prep.Build(ctx, prep.Input{Listing: l, Description: desc, Assessment: a, Profile: o.profile(ctx, userID), Model: o.Model, ModelName: o.ModelName, Now: o.now()})
	if res.ModelErr != nil {
		return nil, fmt.Errorf("%w (%v): nothing was stored, try again", ErrModelUnavailable, res.ModelErr)
	}
	pkg := res.Package
	if err := o.Store.SavePackage(ctx, pkg); err != nil {
		return nil, err
	}
	// Creating a package moves the listing along the pipeline: verified (or needs_you) to drafted.
	if l.Stage != domain.StageDrafted {
		if err := o.Store.Move(ctx, userID, listingID, l.Stage, domain.StageDrafted, "application package prepared", nil); err != nil {
			if existing == nil {
				_ = o.Store.DeletePackage(ctx, userID, pkg.ID)
			}
			return nil, err
		}
	}
	if pkg.Status == domain.PackageDraft {
		return pkg, o.hold(ctx, l, pkg)
	}
	return pkg, nil
}

// hold asks the owner to approve a complete package.
func (o *ApplicationPrep) hold(ctx context.Context, l *domain.Listing, pkg *domain.ApplicationPackage) error {
	if o.Holder == nil {
		return fmt.Errorf("the package is ready but cannot be put on your approvals page: %w", ErrNoPrep)
	}
	approvalID, taskID, err := o.Holder.HoldApplicationPackage(ctx, l, pkg)
	if err != nil {
		return fmt.Errorf("the package is stored but its approval was not requested (the next run asks again): %w", err)
	}
	if err := o.Store.HoldPackage(ctx, l.UserID, pkg.ID, approvalID, taskID); err != nil {
		return err
	}
	pkg.Status, pkg.ApprovalID, pkg.TaskID = domain.PackageReadyForApproval, &approvalID, &taskID
	return nil
}

// Decided applies the owner's decision on a package's approval. Approving moves the listing drafted to approved and
// hands the package's text to startSubmit, which makes the existing submit task: that task asks the owner again at
// send time, so approving a package never sends anything. Rejecting moves the listing back to verified. A decision
// is applied once.
func (o *ApplicationPrep) Decided(ctx context.Context, a *domain.Approval, approved bool, startSubmit func(ctx context.Context, submit *domain.Approval)) {
	if o == nil || o.Store == nil || a == nil {
		return
	}
	id, ok := domain.PackageIDOf(a)
	if !ok {
		return
	}
	pkg, err := o.Store.GetPackage(ctx, a.UserID, id)
	if err != nil {
		o.log().Warn("Package decision for a package that is gone", "package_id", id, "error", err)
		return
	}
	l, err := o.Store.Get(ctx, a.UserID, pkg.ListingID)
	if err != nil {
		o.log().Warn("Package decision for a listing that is gone", "package_id", id, "error", err)
		return
	}
	if !approved {
		if err := o.Store.SetPackageStatus(ctx, a.UserID, id, []string{domain.PackageReadyForApproval}, domain.PackageRejected); err != nil {
			return
		}
		if l.Stage == domain.StageDrafted {
			if err := o.Store.Move(ctx, a.UserID, l.ID, domain.StageDrafted, domain.StageVerified, fmt.Sprintf(packageDecisionNoteFmt, "rejected by you"), nil); err != nil {
				o.log().Warn("Could not move a listing back after a rejected package", "listing_id", l.ID, "error", err)
			}
		}
		return
	}
	if err := o.Store.SetPackageStatus(ctx, a.UserID, id, []string{domain.PackageReadyForApproval}, domain.PackageApproved); err != nil {
		return
	}
	if err := o.Store.Move(ctx, a.UserID, l.ID, l.Stage, domain.StageApproved, fmt.Sprintf(packageDecisionNoteFmt, "approved by you"), pkg.TaskID); err != nil {
		// The listing was closed or moved on while the package waited: nothing may be submitted for it.
		o.log().Warn("Approved package for a listing that can no longer be approved", "listing_id", l.ID, "stage", l.Stage, "error", err)
		_ = o.Store.SetPackageStatus(ctx, a.UserID, id, []string{domain.PackageApproved}, domain.PackageRejected)
		return
	}
	if startSubmit == nil {
		return
	}
	payload, _ := json.Marshal(map[string]string{"company": l.Company, "role": l.Role, "portal_url": l.URL, "resume_path": pkg.ResumeFile, "text": pkg.Text()})
	startSubmit(ctx, &domain.Approval{UserID: a.UserID, TaskID: a.TaskID, ActionType: domain.PackageApprovalAction, ActionPayload: payload})
}

// PrepResult counts what one run did.
type PrepResult struct {
	Prepared   []PrepItem
	Held       int // complete packages whose approval was requested again, without a model
	Remaining  int // candidates left for the next run (time or the per-run cap)
	ModelCalls int
	Problems   []string
	Candidates int
}

// PrepItem is one package a run prepared.
type PrepItem struct {
	Company, Role, Status string
	Missing               int
}

// Run prepares packages for the owner's best eligible open listings: class exceptional or strong, eligibility
// confirmed or likely, best first, at most MaxPackages a run (five by default). A listing that already has a package
// is left alone unless its package still needs the owner and the posting or the profile has changed since, or its
// approval was never requested. Nothing new starts when less than prepReserve of the budget remains; the rest wait
// for the next run.
func (o *ApplicationPrep) Run(ctx context.Context, userID uuid.UUID) (PrepResult, error) {
	var res PrepResult
	if o == nil || o.Store == nil {
		return res, ErrNoPrep
	}
	budget, maxPkgs := o.Budget, o.MaxPackages
	if budget <= 0 {
		budget = DefaultPrepBudget
	}
	if maxPkgs <= 0 {
		maxPkgs = DefaultMaxPackagesRun
	}
	start := o.now()
	deadline := start.Add(budget)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	runCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	profile := o.profile(runCtx, userID)
	if profile.Empty() {
		return res, errors.New("your owner profile is empty, so no application can be written from it")
	}
	profileHash := profile.Hash()

	listings, err := o.Store.ListAssessed(runCtx, userID, domain.AssessedFilter{
		Stages:      []string{domain.StageVerified, domain.StageDrafted},
		Classes:     []string{domain.ValueExceptional, domain.ValueStrong},
		Eligibility: []string{domain.EligibilityConfirmed, domain.EligibilityLikely},
		Limit:       prepCandidateLimit,
	})
	if err != nil {
		return res, err
	}
	briefs, err := o.Store.PackageBriefs(runCtx, userID)
	if err != nil {
		return res, err
	}
	type cand struct {
		l      *domain.Listing
		costly bool // needs a model
	}
	var todo []cand
	for _, l := range listings {
		b := briefs[l.ID]
		switch {
		case b == nil && l.Stage == domain.StageVerified:
			todo = append(todo, cand{l, true})
		case b != nil && b.Status == domain.PackageDraft:
			todo = append(todo, cand{l, false})
		case b != nil && b.Status == domain.PackageNeedsOwner && packageStale(b, l, profileHash):
			todo = append(todo, cand{l, true})
		}
	}
	res.Candidates = len(todo)

	prepared, modelUp := 0, o.Model != nil
	for i, c := range todo {
		if runCtx.Err() != nil || deadline.Sub(o.now()) < prepReserve {
			res.Remaining = len(todo) - i
			break
		}
		if c.costly && prepared >= maxPkgs {
			res.Remaining += len(todo) - i
			break
		}
		if c.costly && !modelUp && o.Model != nil {
			res.Remaining++
			continue
		}
		pkg, err := o.Prepare(runCtx, userID, c.l.ID)
		if pkg != nil && pkg.ModelCalls > 0 {
			res.ModelCalls += pkg.ModelCalls
		}
		if err != nil {
			res.Problems = append(res.Problems, fmt.Sprintf("%s at %s: %v", c.l.Role, c.l.Company, err))
			if runCtx.Err() != nil {
				res.Remaining += len(todo) - i
				break
			}
			if errors.Is(err, ErrModelUnavailable) {
				modelUp = false // the model could not be reached: do not ask again this run
			}
			continue
		}
		if c.costly {
			prepared++
			res.Prepared = append(res.Prepared, PrepItem{Company: c.l.Company, Role: c.l.Role, Status: pkg.Status, Missing: len(pkg.MissingInformation)})
		} else {
			res.Held++
		}
	}
	return res, nil
}

// packageStale reports whether a package that still needs the owner was made from an older posting or profile.
func packageStale(b *domain.PackageBrief, l *domain.Listing, profileHash string) bool {
	if b.ProfileHash != profileHash {
		return true
	}
	cur, old := "", ""
	if l.DescriptionHash != nil {
		cur = *l.DescriptionHash
	}
	if b.DescriptionHash != nil {
		old = *b.DescriptionHash
	}
	return cur != old
}

// Summary is the owner-facing text of one run.
func (r PrepResult) Summary() string {
	var b strings.Builder
	switch {
	case r.Candidates == 0:
		b.WriteString("No application packages to prepare: every exceptional or strong listing you are eligible for already has one.")
	default:
		fmt.Fprintf(&b, "Prepared %d application packages (%d model calls).", len(r.Prepared), r.ModelCalls)
	}
	ready, needs := 0, 0
	for _, p := range r.Prepared {
		if p.Status == domain.PackageReadyForApproval || p.Status == domain.PackageDraft {
			ready++
		} else {
			needs++
		}
	}
	if len(r.Prepared) > 0 {
		fmt.Fprintf(&b, " %d are waiting on your approvals page; %d need information from you first.", ready, needs)
		for i, p := range r.Prepared {
			state := "waiting for your approval"
			if p.Status == domain.PackageNeedsOwner {
				state = fmt.Sprintf("needs %d things from you", p.Missing)
			}
			fmt.Fprintf(&b, "\n%d. %s at %s: %s.", i+1, p.Role, p.Company, state)
		}
	}
	if r.Held > 0 {
		fmt.Fprintf(&b, "\n%d complete packages were put on your approvals page again.", r.Held)
	}
	if r.Remaining > 0 {
		fmt.Fprintf(&b, "\n%d more wait for the next run.", r.Remaining)
	}
	if len(r.Problems) > 0 {
		b.WriteString("\nProblems: " + strings.Join(r.Problems, "; ") + ".")
	}
	b.WriteString("\nNothing was sent to anyone.")
	return b.String()
}

// PrepareApplications runs the APPLICATION PREP routine for its task, in code. handled is false for any other task.
func (w *TrackerLists) PrepareApplications(ctx context.Context, task *domain.Task) (string, bool) {
	if w == nil || w.Prep == nil || task == nil || !domain.IsApplicationPrep(task.Title) {
		return "", false
	}
	res, err := w.Prep.Run(ctx, task.UserID)
	if err != nil {
		return "Application prep could not run: " + err.Error(), true
	}
	return res.Summary(), true
}

// HoldPackageTask answers the task an application package was held on, once the owner has decided: it carries only
// the approval and does nothing. handled is false for any other task.
func (w *TrackerLists) HoldPackageTask(task *domain.Task) (string, bool) {
	if task == nil || !domain.IsPackageTask(task.Title) {
		return "", false
	}
	return "The application package was decided on your approvals page. Nothing was sent: a submission is a separate task that asks you again before it sends anything.", true
}

var _ PrepStore = (*repository.OpportunityRepository)(nil)

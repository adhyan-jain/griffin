package prep

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/griffin/core/internal/assess"
	"github.com/griffin/core/internal/domain"
)

// MaxModelCalls is the most a package may cost: one to write, one to rewrite what failed the fact check.
const MaxModelCalls = 2

// MinPostingChars is the least posting text worth a model call: a lead that has only a title has no posting to ground
// a letter in, and the owner is told to open it.
const MinPostingChars = 200

// Input is everything one package is made from.
type Input struct {
	Listing     *domain.Listing
	Description string
	Assessment  *domain.Assessment // may be nil
	Profile     Profile
	// Model writes the letter (Gemini Flash); nil prepares everything code can and lists the letter as missing.
	Model     assess.ModelFunc
	ModelName string
	Now       time.Time
}

// Result is a built package and what it cost.
type Result struct {
	Package    *domain.ApplicationPackage
	ModelCalls int
	// ModelErr is set when the model could not be reached or the call failed. Nothing should be stored then: the
	// deterministic parts are cheap to build again, and a letter that was never attempted is not a letter that failed.
	ModelErr error
}

// Build prepares one package. Code decides everything it can; the model writes the cover letter (and the "why join"
// paragraph when the posting asks) in at most MaxModelCalls calls, and only text that passes the fact check is kept.
func Build(ctx context.Context, in Input) Result {
	l := in.Listing
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	roleClass := domain.RoleUnknown
	if in.Assessment != nil && in.Assessment.RoleClass != "" {
		roleClass = in.Assessment.RoleClass
	}
	if roleClass == domain.RoleUnknown {
		roleClass, _ = assess.ClassifyRole(l.Role, in.Description)
	}
	p := in.Profile
	pkg := &domain.ApplicationPackage{
		UserID: l.UserID, ListingID: l.ID, RoleClass: roleClass, ResumeFile: domain.ResumeFor(roleClass),
		Deadline: l.Deadline, DescriptionHash: l.DescriptionHash, ProfileHash: p.Hash(),
		SelectedProjects: []domain.PackageProject{}, Answers: []domain.PackageAnswer{}, RequiredDocuments: []domain.RequiredDocument{},
		MissingInformation: []string{}, Evidence: []domain.Reason{},
	}
	var missing []string
	need := func(format string, args ...any) { missing = append(missing, fmt.Sprintf(format, args...)) }

	description := strings.TrimSpace(in.Description)
	havePosting := len(description) >= MinPostingChars
	if !havePosting {
		need("Griffin has only the listing's title and details, not the posting's text, so it could not read the questions or required documents or write a letter from it: open the posting and check them yourself")
	}
	if in.Assessment != nil {
		switch in.Assessment.Eligibility {
		case domain.EligibilityIneligible:
			why := ""
			if len(in.Assessment.EligibilityReasons) > 0 {
				r := in.Assessment.EligibilityReasons[0]
				why = ": " + r.Reason
				if r.Quote != "" {
					why += " (\"" + r.Quote + "\")"
				}
			}
			need("The posting appears to rule you out%s. Check before you apply", why)
		case domain.EligibilityUnknown:
			need("The posting does not say whether you are eligible: check it before you apply")
		}
	}
	if l.Deadline != nil && l.Deadline.Before(now) {
		need("The deadline has passed (%s)", l.Deadline.Format("2 Jan 2006"))
	}
	if p.Empty() {
		need("Your owner profile is empty, so nothing could be written from it: add your profile to the Library")
	}
	if p.Name == "" && !p.Empty() {
		need("Your full name is not in your profile: the letter could not be signed")
	}

	if havePosting {
		pkg.SelectedProjects = SelectProjects(p, l.Role, description)
	}
	var asked []Asked
	if havePosting {
		asked = QuestionsIn(description)
	}

	// The model step: write the letter and, if asked, the "why join" paragraph.
	var draft Draft
	var letterOK, whyOK bool
	wantWhy := false
	for _, a := range asked {
		if a.Key == QWhyJoin {
			wantWhy = true
		}
	}
	calls := 0
	var modelErr error
	switch {
	case !havePosting || p.Empty():
		// nothing to ground a letter in; the reasons are already listed
	case in.Model == nil:
		need("The cover letter was not written: no model is configured")
	default:
		known := Known(p, l.Company, l.Role, l.Location, l.Role+". "+description)
		var problems []string
		var prev Draft
		for calls < MaxModelCalls {
			if ctx.Err() != nil {
				modelErr = ctx.Err()
				break
			}
			reply, err := in.Model(ctx, letterSystem, Prompt(p, pkg.SelectedProjects, l.Company, l.Role, l.Location, description, wantWhy, problems, prev))
			calls++
			if err != nil {
				modelErr = err
				break
			}
			d, perr := ParseDraft(reply)
			if perr != nil {
				problems, prev = []string{perr.Error()}, Draft{}
				continue
			}
			prev = d
			letterProblems := CheckLetter(d.CoverLetter, p, known)
			letterOK = len(letterProblems) == 0
			problems = letterProblems
			whyOK = false
			if wantWhy {
				whyProblems := CheckWhyJoin(d.WhyJoin, p, known)
				whyOK = len(whyProblems) == 0
				problems = append(problems, whyProblems...)
			}
			draft = d
			if len(problems) == 0 {
				break
			}
		}
		if modelErr != nil {
			return Result{ModelCalls: calls, ModelErr: modelErr}
		}
		if !letterOK {
			need("The cover letter could not be written without claims outside your profile and the posting (%s): write it yourself or add the missing facts to your profile", strings.Join(firstN(problems, 3), "; "))
		}
	}
	if letterOK {
		pkg.CoverLetter = draft.CoverLetter
	}

	// Answers: from the profile only. A question the profile cannot answer is the owner's.
	for _, a := range asked {
		if a.Key == QWhyJoin {
			if whyOK {
				pkg.Answers = append(pkg.Answers, domain.PackageAnswer{Question: a.Question, Answer: draft.WhyJoin, Source: "written from your profile and the posting, then fact-checked", Quote: a.Quote})
			} else if havePosting && !p.Empty() {
				need("%q: could not be answered from your profile (the posting asks: \"%s\")", a.Question, a.Quote)
			}
			continue
		}
		if ans, src, ok := AnswerFrom(a.Key, p); ok {
			pkg.Answers = append(pkg.Answers, domain.PackageAnswer{Question: a.Question, Answer: ans, Source: src, Quote: a.Quote})
		} else {
			need("%q: your profile does not say (the posting asks: \"%s\")", a.Question, a.Quote)
		}
	}

	// Required documents.
	docText := l.Role + ". " + description
	if !havePosting {
		docText = ""
	}
	pkg.RequiredDocuments = RequiredDocuments(docText, p, letterOK, pkg.ResumeFile)
	for _, d := range pkg.RequiredDocuments {
		if d.Status == "missing" && d.Name != "Cover letter" { // the letter's gap is already listed above
			need("%s: %s", d.Name, d.Note)
		}
	}

	pkg.Evidence = Evidence(pkg.SelectedProjects, asked, pkg.RequiredDocuments)
	pkg.MissingInformation = dedupe(missing)
	pkg.Status = domain.PackageDraft
	if len(pkg.MissingInformation) > 0 {
		pkg.Status = domain.PackageNeedsOwner
	}
	pkg.ModelCalls = calls
	if calls > 0 && in.ModelName != "" {
		name := in.ModelName
		pkg.ModelUsed = &name
	}
	return Result{Package: pkg, ModelCalls: calls}
}

func firstN(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func dedupe(s []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

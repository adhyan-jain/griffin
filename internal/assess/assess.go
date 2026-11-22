package assess

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/griffin/core/internal/domain"
)

// Input is everything the assessor reads about one listing.
type Input struct {
	Title       string
	Company     string
	Location    string
	Pay         string // the verbatim pay text the source gave ("₹ 30,000 /month"), "" when it gave none
	Description string
	// SourceKind and Verification say how close the record is to the employer (domain.SourceKind*, Verification*).
	SourceKind   string
	Verification string
	// CompanyKind is the stored kind of the employer ("startup", "enterprise", "lab", "foundation", "unknown").
	CompanyKind string
	// Key is domain.AssessmentKey: the description hash (or a hash of the fields), the key a model call is cached under.
	Key string
	// OwnerText is the owner's profile text, when there is one; it adds to the owner's skills.
	OwnerText string
}

// Findings are the deterministic stages' conclusions, before the model and the score.
type Findings struct {
	Employment, EmploymentQuote string
	Role, RoleQuote             string
	Loc                         LocationResult
	Grad                        GradResult
	Comp                        *Comp
	Duration, DurationQuote     string
	Skills                      []domain.SkillMatch
	Depth, Mentorship, PPO      Signal
	TextLen                     int

	// Model holds the verified model conclusions merged in (zero when no model was used).
	Model ModelReply
}

// Deterministic runs every code-only stage on one listing.
func Deterministic(in Input) Findings {
	var f Findings
	f.TextLen = len(norm(in.Description))
	f.Employment, f.EmploymentQuote = ClassifyEmployment(in.Title, in.Description)
	f.Role, f.RoleQuote = ClassifyRole(in.Title, in.Description)
	f.Loc = AssessLocation(in.Location, in.Description)
	f.Grad = AssessGrad(in.Description)
	f.Comp = ParseCompensation(in.Pay, in.Title, in.Description)
	f.Duration, f.DurationQuote = ParseDuration(in.Title, in.Description)
	f.Skills = MatchSkills(in.Title, in.Description, OwnerSkills(in.OwnerText))
	f.Depth = DepthSignal(in.Description)
	f.Mentorship = MentorshipSignal(in.Description)
	f.PPO = PPOSignal(in.Description)
	return f
}

// Merge folds verified model conclusions into the findings. A model fills what code left unknown: it never
// overrides a role code settled, a blocker code found or a signal code read (depth may be re-read, see below).
func (f *Findings) Merge(m ModelReply) {
	f.Model = m
	if (f.Role == domain.RoleOtherTech || f.Role == domain.RoleUnknown) && m.RoleClass != "" {
		f.Role, f.RoleQuote = m.RoleClass, m.RoleQuote
	}
	// Counting terms is crude, so a verified reading of the text may replace a count that did not find depth; it
	// never replaces one that did.
	if f.Depth.Level != DepthDeep && m.Depth.Level != "" {
		f.Depth = Signal{m.Depth.Level, "The model read the posting's engineering depth", m.Depth.Quote}
	}
	if f.Mentorship.Level == "unknown" && m.Mentorship.Level == "present" {
		f.Mentorship = Signal{"present", "The model found mentorship in the posting", m.Mentorship.Quote}
	}
	if f.PPO.Level == "unknown" && m.PPO.Level != "" {
		f.PPO = Signal{m.PPO.Level, "The model read the full-time offer terms", m.PPO.Quote}
	}
	have := map[string]bool{}
	for _, s := range f.Skills {
		have[s.Skill] = true
	}
	for _, s := range m.Skills {
		if !have[s.Skill] {
			f.Skills = append(f.Skills, s)
		}
	}
}

// Eligibility decides whether the owner may apply and why, from the findings. Order: a quoted blocker makes it
// ineligible (from code, then from the model); an explicit fit makes it confirmed; no blocker in a posting that was
// really read, in a place the owner can work, makes it likely; anything else is unknown, for the review queue.
func (f Findings) Eligibility() (string, []domain.Reason) {
	var blockers []domain.Reason
	blockers = append(blockers, f.Loc.Blockers...)
	blockers = append(blockers, f.Grad.Blockers...)
	if len(blockers) > 0 {
		return domain.EligibilityIneligible, blockers
	}
	if f.Model.Eligibility == domain.EligibilityIneligible {
		return domain.EligibilityIneligible, f.Model.EligibilityReasons
	}
	placeOK := f.Loc.Fit == domain.LocationIndia || f.Loc.Fit == domain.LocationRemoteOK
	if f.Grad.Fit == domain.GradFit && placeOK {
		reasons := []domain.Reason{*f.Grad.Reason}
		if f.Loc.Quote != "" {
			reasons = append(reasons, domain.Reason{Reason: "The role is in a place the owner can work (" + f.Loc.Fit + ")", Quote: f.Loc.Quote})
		}
		return domain.EligibilityConfirmed, reasons
	}
	if f.Model.Eligibility == domain.EligibilityConfirmed || f.Model.Eligibility == domain.EligibilityLikely {
		if f.Loc.Fit != domain.LocationOutside {
			return f.Model.Eligibility, f.Model.EligibilityReasons
		}
	}
	if placeOK && f.Grad.Soft == nil && f.TextLen >= minModelText {
		reasons := []domain.Reason{{Reason: fmt.Sprintf("No restriction on graduation year, degree or work authorisation was found in %d characters of the posting; it does not state the 2028 graduating year either", f.TextLen)}}
		if f.Loc.Quote != "" {
			reasons = append(reasons, domain.Reason{Reason: "The role is in a place the owner can work (" + f.Loc.Fit + ")", Quote: f.Loc.Quote})
		}
		return domain.EligibilityLikely, reasons
	}
	var why []domain.Reason
	if f.Grad.Soft != nil {
		why = append(why, *f.Grad.Soft)
	}
	switch {
	case f.TextLen < minModelText:
		why = append(why, domain.Reason{Reason: "The listing has too little text to judge eligibility from"})
	case f.Loc.Fit == domain.LocationUnknown:
		why = append(why, domain.Reason{Reason: "The posting does not say where the owner may work from"})
	case f.Loc.Fit == domain.LocationOutside:
		why = append(why, domain.Reason{Reason: "The role is outside India and the posting does not say it is open to the owner", Quote: f.Loc.Quote})
	case f.Loc.Fit == domain.LocationRelocation:
		why = append(why, domain.Reason{Reason: "The role is outside India with relocation or visa support; whether the owner may apply is not stated", Quote: f.Loc.Quote})
	}
	return domain.EligibilityUnknown, why
}

// Options configures one assessment.
type Options struct {
	// Model is the model to ask when code cannot settle something; nil asks nothing.
	Model     ModelFunc
	ModelName string
	// AllowModel is false when the run's model budget is spent.
	AllowModel bool
	Now        func() time.Time
}

// Result is one assessment and what it cost.
type Result struct {
	Assessment  *domain.Assessment
	ModelCalled bool
	ModelErr    error
	// CallFailed is true when the model could not be reached (as opposed to answering badly): the run stops asking.
	CallFailed bool
}

// Assess assesses one listing. prev is the listing's earlier assessment, if any: a model answer it holds for the
// same key is reused, never re-asked. The model is asked at most once per key, only when code left eligibility or the
// role unsettled, and only when the run's budget allows.
func Assess(ctx context.Context, in Input, prev *domain.Assessment, opt Options) Result {
	now := time.Now
	if opt.Now != nil {
		now = opt.Now
	}
	owner := OwnerSkills(in.OwnerText)
	text := norm(in.Description)
	f := Deterministic(in)
	res := Result{}
	var modelKey *string
	var stored json.RawMessage
	var modelName *string

	if prev != nil && prev.ModelKey != nil && *prev.ModelKey == in.Key {
		// Already asked at this key: reuse what passed verification, whatever the rules now say.
		modelKey, modelName = prev.ModelKey, prev.ModelUsed
		if len(prev.ModelOutput) > 0 {
			var r ModelReply
			if json.Unmarshal(prev.ModelOutput, &r) == nil {
				v := Verify(r, text, owner)
				f.Merge(v)
				stored, _ = json.Marshal(v)
			}
		}
	} else if elig, _ := f.Eligibility(); opt.Model != nil && opt.AllowModel && NeedsModel(f, elig) {
		res.ModelCalled = true
		reply, err := opt.Model(ctx, modelSystem, buildPrompt(in, text))
		if err != nil {
			// A call that failed is not recorded as made: it is tried again on a later run.
			res.ModelErr, res.CallFailed = err, true
		} else {
			k := in.Key
			modelKey, modelName = &k, &opt.ModelName
			parsed, perr := ParseReply(reply)
			if perr != nil {
				res.ModelErr = perr
			} else {
				v := Verify(parsed, text, owner)
				f.Merge(v)
				stored, _ = json.Marshal(v)
			}
		}
	}

	elig, reasons := f.Eligibility()
	a := &domain.Assessment{
		AssessmentKey: in.Key, Version: domain.AssessorVersion,
		EmploymentClass: f.Employment, RoleClass: f.Role,
		Eligibility: elig, EligibilityReasons: reasons,
		LocationFit: f.Loc.Fit, GradYearFit: f.Grad.Fit,
		SkillsMatched: f.Skills, AssessedAt: now(),
		ModelUsed: modelName, ModelKey: modelKey, ModelOutput: stored,
	}
	if a.EligibilityReasons == nil {
		a.EligibilityReasons = []domain.Reason{}
	}
	if a.SkillsMatched == nil {
		a.SkillsMatched = []domain.SkillMatch{}
	}
	if f.Comp != nil {
		v := f.Comp.MonthlyINR
		a.CompensationMonthlyINR, a.CompensationQuote = &v, f.Comp.Quote
	}
	if f.Duration != "" {
		d := f.Duration
		a.Duration = &d
	}
	a.ValueScore, a.ValueBreakdown, a.ValueClass = Score(in, f, elig)
	return res.with(a)
}

func (r Result) with(a *domain.Assessment) Result { r.Assessment = a; return r }

// Explain is the one-line reason for an assessment: its best two components, for the routine's summary.
func Explain(a *domain.Assessment) string {
	type c struct {
		name   string
		points float64
		max    float64
		why    string
	}
	var best []c
	for _, comp := range a.ValueBreakdown {
		if comp.Max > 0 && !comp.Unknown {
			best = append(best, c{comp.Name, comp.Points, comp.Max, comp.Reason})
		}
	}
	// Highest share of its maximum first.
	sort.SliceStable(best, func(i, j int) bool { return best[i].points/best[i].max > best[j].points/best[j].max })
	var parts []string
	for _, b := range best[:min(2, len(best))] {
		parts = append(parts, Labels[b.name]+": "+strings.TrimSuffix(b.why, "."))
	}
	if len(parts) == 0 {
		return "not enough in the listing to judge"
	}
	return strings.Join(parts, "; ")
}

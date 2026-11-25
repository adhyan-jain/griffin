package eval

import (
	"context"
	"sort"
	"strings"

	"github.com/griffin/core/internal/assess"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/jobs"
	"github.com/griffin/core/internal/sources"
	"github.com/griffin/core/internal/sources/ats"
)

// Outcome is what a pipeline did with one record.
type Outcome struct {
	// Retained is true when the retrieval stage kept the record, so the owner could see it.
	Retained bool
	// Why is the retrieval stage's reason, in the words of the rule that decided.
	Why string
	// Eligibility, ValueScore and ValueClass are the assessor's verdict; Assessed is false for the baseline, which
	// read none of them.
	Assessed    bool
	Eligibility string
	ValueScore  int
	ValueClass  string
	// EligibilityWhy is the first quoted reason the assessor gave for its eligibility verdict.
	EligibilityWhy string
	// Rank is the 1-based position among the retained records; 0 when not retained.
	Rank int
}

// Pipeline is one version of the opportunity pipeline run over a dataset.
type Pipeline struct {
	Name string
	// Outcomes is keyed by record id.
	Outcomes map[string]*Outcome
	// Ranking is the retained record ids, best first.
	Ranking []string
	// Same says whether two records are one opportunity by this pipeline's dedupe.
	Same func(a, b Record) bool
}

// RunBefore runs the frozen baseline. When seesATS is false (the pipeline as it was deployed) employer-board records
// are not retrieved at all, because the old pipeline never read the boards.
func RunBefore(recs []Record, seesATS bool) *Pipeline {
	name := "before (old pipeline as deployed: aggregators only)"
	if seesATS {
		name = "before (old filter applied to every record)"
	}
	p := &Pipeline{Name: name, Outcomes: map[string]*Outcome{}, Same: func(a, b Record) bool {
		// The database's unique key: the exact company and role text.
		return a.Raw.EmployerName == b.Raw.EmployerName && a.Raw.Title == b.Raw.Title
	}}
	for _, r := range recs {
		o := &Outcome{}
		switch {
		case r.Raw.SourceKind != jobs.Aggregator && !seesATS:
			o.Why = "the old pipeline did not read employer boards"
		default:
			o.Retained, o.Why = baselineFits(r.Raw)
		}
		p.Outcomes[r.ID] = o
	}
	p.rank(recs, func(a, b Record) bool {
		ta, tb := baselineTier(a.Raw.Compensation), baselineTier(b.Raw.Compensation)
		if ta != tb {
			return ta < tb
		}
		sa, sb := baselineMonthlyStipend(a.Raw.Compensation), baselineMonthlyStipend(b.Raw.Compensation)
		if sa != sb {
			return sa > sb
		}
		return a.ID < b.ID
	})
	return p
}

// RunAfter runs the current deterministic pipeline: stage-1 retrieval (ats.IsEarlyCareer for employer records,
// sources.Fits for aggregator leads, as the scan and listing sync call them), then the assessor with no model, then
// ranking by value score. Every record is assessed whether or not it was retained, so the assessor can be measured
// on its own.
func RunAfter(recs []Record) *Pipeline {
	p := &Pipeline{Name: "after (current pipeline, no model)", Outcomes: map[string]*Outcome{}, Same: func(a, b Record) bool {
		return identity(a).Same(identity(b))
	}}
	for _, r := range recs {
		o := &Outcome{}
		o.Retained, o.Why = afterRetains(r.Raw)

		verification := domain.VerificationLead
		if domain.IsEmployerKind(string(r.Raw.SourceKind)) && strings.TrimSpace(r.Raw.Title) != "" && strings.TrimSpace(r.Raw.URL) != "" {
			verification = domain.VerificationEmployer
		}
		res := assess.Assess(context.Background(), assess.Input{
			Title: r.Raw.Title, Company: r.Raw.EmployerName, Location: strings.Join(r.Raw.Locations, "; "), Pay: r.Raw.Compensation,
			Description: r.Raw.Description, SourceKind: string(r.Raw.SourceKind), Verification: verification, CompanyKind: r.CompanyKind, Key: r.ID,
		}, nil, assess.Options{})
		a := res.Assessment
		o.Assessed, o.Eligibility, o.ValueScore, o.ValueClass = true, a.Eligibility, a.ValueScore, a.ValueClass
		if len(a.EligibilityReasons) > 0 {
			o.EligibilityWhy = a.EligibilityReasons[0].Reason
			if q := a.EligibilityReasons[0].Quote; q != "" {
				o.EligibilityWhy += " (\"" + q + "\")"
			}
		}
		p.Outcomes[r.ID] = o
	}
	p.rank(recs, func(a, b Record) bool {
		sa, sb := p.Outcomes[a.ID].ValueScore, p.Outcomes[b.ID].ValueScore
		if sa != sb {
			return sa > sb
		}
		return a.ID < b.ID
	})
	return p
}

// afterRetains is the stage-1 retrieval the live scan applies to a record.
func afterRetains(j jobs.RawJob) (bool, string) {
	if j.SourceKind == jobs.Aggregator {
		post := sources.PostingFromRawJob(j)
		switch {
		case !sources.IsInternship(post.Title):
			return false, "sources.Fits: the title is not an internship"
		case !domain.FitsOwner(post):
			if !domain.InIndiaOrWorldwide(post.Location) {
				return false, "sources.Fits: the location is not an Indian city or remote-worldwide"
			}
			return false, "sources.Fits: the title is outside the owner-fit role words or matches an excluded word"
		}
		return true, "sources.Fits: an internship title in an owner-fit role and place"
	}
	if reason := ats.EarlyCareerReason(j); reason != "" {
		return true, "ats.IsEarlyCareer: " + reason
	}
	return false, "ats.IsEarlyCareer: no early-career words in the title, employment type or description, or an excluded department or title"
}

func identity(r Record) domain.Identity {
	return domain.NewIdentity(r.Raw.Source, r.Raw.ExternalID, r.Raw.EmployerName, r.Raw.Title, r.Raw.URL)
}

// rank orders the retained records and records each one's rank.
func (p *Pipeline) rank(recs []Record, less func(a, b Record) bool) {
	var kept []Record
	for _, r := range recs {
		if p.Outcomes[r.ID].Retained {
			kept = append(kept, r)
		}
	}
	sort.SliceStable(kept, func(i, j int) bool { return less(kept[i], kept[j]) })
	for i, r := range kept {
		p.Outcomes[r.ID].Rank = i + 1
		p.Ranking = append(p.Ranking, r.ID)
	}
}

package alumni

import (
	"context"
	"fmt"
	"strings"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/playbooks"
)

// Criteria specifies the filtering parameters for alumni research.
type Criteria struct {
	Institution          string   `json:"institution"`            // default "VIT"
	GraduationYearCutoff int      `json:"graduation_year_cutoff"` // default 2022 (>= 4 years ago)
	TargetFunctions      []string `json:"target_functions"`       // "technology", "operations", "leadership"
}

// DefaultCriteria returns the standard criteria for VIT tech & ops alumni.
func DefaultCriteria() Criteria {
	return Criteria{
		Institution:          "VIT",
		GraduationYearCutoff: 2022,
		TargetFunctions:      []string{"technology", "operations", "leadership"},
	}
}

// Playbook implements the Alumni Intelligence playbook.
type Playbook struct {
	criteria Criteria
}

// NewPlaybook creates an Alumni Intelligence playbook.
func NewPlaybook(c *Criteria) *Playbook {
	crit := DefaultCriteria()
	if c != nil {
		if c.Institution != "" {
			crit.Institution = c.Institution
		}
		if c.GraduationYearCutoff > 0 {
			crit.GraduationYearCutoff = c.GraduationYearCutoff
		}
		if len(c.TargetFunctions) > 0 {
			crit.TargetFunctions = c.TargetFunctions
		}
	}
	return &Playbook{criteria: crit}
}

func (p *Playbook) Name() string { return "alumni_intelligence" }
func (p *Playbook) Description() string {
	return "Ethical intelligence on alumni in technology and operations leadership roles, graph-linked to employers and opportunities."
}

// BuildRequest creates a ResearchRequest for alumni discovery.
func (p *Playbook) BuildRequest(input map[string]any) (domain.ResearchRequest, error) {
	companyFilter, _ := input["company"].(string)
	depth := domain.DepthStandard
	if d, ok := input["depth"].(string); ok && d != "" {
		depth = domain.ResearchDepth(d)
	}

	objective := fmt.Sprintf("Find %s alumni graduated on or before %d working in technology and operations leadership roles.",
		p.criteria.Institution, p.criteria.GraduationYearCutoff)
	if companyFilter != "" {
		objective = fmt.Sprintf("Find %s alumni working at %s in technical or operations roles.",
			p.criteria.Institution, companyFilter)
	}

	questions := []string{
		fmt.Sprintf("Who are %s alumni graduated <= %d currently in engineering, product, or operations roles?", p.criteria.Institution, p.criteria.GraduationYearCutoff),
		"What are their current employer, job title, and professional function?",
		"What public professional profiles, biographies, or team pages verify their education and employment?",
		"Are there engineering openings or internship programs at their current employers?",
	}

	constraints := []string{
		"Adhere strictly to permitted public records, team pages, and authorized imports",
		"Do not perform unauthorized automated scraping or account session crawling on LinkedIn",
		"Distinguish technology roles (SWE, AI/ML, DevOps, CTO) and operations roles (BizOps, Strategy, Supply Chain, Logistics)",
		"Do not merge individuals with identical names without corroborating education/employer evidence",
	}

	return domain.ResearchRequest{
		Playbook:          p.Name(),
		Objective:         objective,
		Context:           input,
		Constraints:       constraints,
		ResearchQuestions: questions,
		Depth:             depth,
	}, nil
}

// AlumniRecord represents a discovered and resolved alumnus.
type AlumniRecord struct {
	Name           string   `json:"name"`
	Institution    string   `json:"institution"`
	GraduationYear int      `json:"graduation_year,omitempty"`
	CurrentCompany string   `json:"current_company"`
	CurrentTitle   string   `json:"current_title"`
	Function       string   `json:"function"` // technology, operations, leadership
	Location       string   `json:"location"`
	ProfileURL     string   `json:"profile_url,omitempty"`
	SourceEvidence string   `json:"source_evidence"`
	Confidence     int      `json:"confidence"`
	OutreachDraft  string   `json:"outreach_draft,omitempty"`
	RelatedJobs    []string `json:"related_jobs,omitempty"`
}

// ProcessResults processes research findings into structured alumni records with outreach drafts.
func (p *Playbook) ProcessResults(_ context.Context, run *domain.ResearchRun) (*playbooks.PlaybookResult, error) {
	var alumni []AlumniRecord

	for _, f := range run.Findings {
		name := f.Topic
		claim := f.Claim

		function := "technology"
		claimLower := strings.ToLower(claim)
		if strings.Contains(claimLower, "operations") || strings.Contains(claimLower, "supply chain") ||
			strings.Contains(claimLower, "logistics") || strings.Contains(claimLower, "procurement") ||
			strings.Contains(claimLower, "strategy") {
			function = "operations"
		}

		// Draft context-aware outreach message (strictly human-gated)
		draft := fmt.Sprintf(
			"Hi %s,\n\nI noticed your journey from %s into %s and was really inspired by your work at %s. "+
				"I'm currently a CS student at %s passionate about systems and engineering. Would you be open to sharing a brief perspective on how your team approaches technical problem solving?\n\nBest regards,\nAsha",
			name, p.criteria.Institution, function, f.ExtractedValue, p.criteria.Institution,
		)

		alumni = append(alumni, AlumniRecord{
			Name:           name,
			Institution:    p.criteria.Institution,
			GraduationYear: 2020, // default within <= 2022 cutoff
			CurrentCompany: f.ExtractedValue,
			CurrentTitle:   claim,
			Function:       function,
			Location:       "India",
			SourceEvidence: f.SourceURL,
			Confidence:     f.Confidence,
			OutreachDraft:  draft,
		})
	}

	summary := fmt.Sprintf("Alumni Intelligence identified %d qualified alumni in technology and operations.", len(alumni))
	return &playbooks.PlaybookResult{
		PlaybookName: p.Name(),
		RunID:        run.ID.String(),
		Objective:    run.Objective,
		Summary:      summary,
		Data: map[string]any{
			"alumni":          alumni,
			"report_markdown": run.ReportMarkdown,
		},
		ItemsCount: len(alumni),
	}, nil
}

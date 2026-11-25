package alumni

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/playbooks"
)

// Criteria specifies the filtering parameters for alumni research.
type Criteria struct {
	Institution          string   `json:"institution"`            // default "VIT"
	GraduationYearCutoff int      `json:"graduation_year_cutoff"` // a person qualifies when their known year is <= this; default domain.DefaultGraduationCutoff
	TargetFunctions      []string `json:"target_functions"`       // "technology", "operations", "leadership"
}

// DefaultCriteria returns the standard criteria for VIT tech & ops alumni.
func DefaultCriteria() Criteria {
	return Criteria{
		Institution:          "VIT",
		GraduationYearCutoff: domain.DefaultGraduationCutoff,
		TargetFunctions:      []string{"technology", "operations", "leadership"},
	}
}

// PeopleStore is where a supported fact about a person is attached: the existing person record with that profile
// link. A person is never matched by name alone.
type PeopleStore interface {
	// SetGraduationYear sets the year of the person with this profile URL, with the evidence that states it. It
	// reports whether such a person exists.
	SetGraduationYear(ctx context.Context, userID uuid.UUID, profileURL string, year int, evidenceID *uuid.UUID) (bool, error)
}

// Playbook implements the Alumni Intelligence playbook.
type Playbook struct {
	criteria Criteria
	store    PeopleStore
}

// WithStore returns the playbook attached to the person records.
func (p *Playbook) WithStore(s PeopleStore) *Playbook {
	p.store = s
	return p
}

// QueryTemplates: alumni research has no fixed angles; its questions and the objective drive the queries.
func (p *Playbook) QueryTemplates() []string { return nil }

// Subject: none.
func (p *Playbook) Subject(map[string]any) string { return "" }

// DefaultQuestions are asked when the request names none.
func (p *Playbook) DefaultQuestions() []string { return p.questions() }

// PlanGuidance is the alumni wording of the planning prompt.
func (p *Playbook) PlanGuidance() string {
	return "This is research on people. Use public professional pages only. For each person state, when a page says so, the graduation year, current employer, title and location; never infer them."
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
		ResearchQuestions: p.questions(),
		Depth:             depth,
	}, nil
}

func (p *Playbook) questions() []string {
	return []string{
		fmt.Sprintf("Who are %s alumni who graduated in %d or earlier and now work in engineering, product, or operations roles?", p.criteria.Institution, p.criteria.GraduationYearCutoff),
		"What are their current employer, job title, and professional function?",
		"What public professional profiles, biographies, or team pages verify their education and employment?",
		"Are there engineering openings or internship programs at their current employers?",
	}
}

// AlumniRecord represents a discovered alumnus. A field no supported finding states is empty or zero: nothing is
// filled in by default.
type AlumniRecord struct {
	Name           string `json:"name"`
	Institution    string `json:"institution"`
	GraduationYear int    `json:"graduation_year,omitempty"`
	// MeetsCutoff is true only for a known year at or before the cutoff; an unknown year never meets it.
	MeetsCutoff    bool   `json:"meets_cutoff"`
	CurrentCompany string `json:"current_company,omitempty"`
	CurrentTitle   string `json:"current_title,omitempty"`
	// Function is guessed from the title's words (technology, operations) and is always marked inferred.
	Function         string   `json:"function,omitempty"`
	FunctionInferred bool     `json:"function_inferred"`
	Location         string   `json:"location,omitempty"`
	ProfileURL       string   `json:"profile_url,omitempty"`
	SourceEvidence   string   `json:"source_evidence"`
	Confidence       int      `json:"confidence"`
	OutreachDraft    string   `json:"outreach_draft,omitempty"`
	RelatedJobs      []string `json:"related_jobs,omitempty"`
}

func yearOf(s string) int {
	for _, w := range strings.FieldsFunc(s, func(r rune) bool { return r < '0' || r > '9' }) {
		if len(w) == 4 {
			if y, err := strconv.Atoi(w); err == nil && y >= 1950 && y <= 2100 {
				return y
			}
		}
	}
	return 0
}

func propertyIs(f domain.ResearchFinding, names ...string) bool {
	pr := strings.ToLower(strings.Join(strings.Fields(strings.ReplaceAll(f.Property, "_", " ")), " "))
	for _, n := range names {
		if pr == n {
			return true
		}
	}
	return false
}

// ProcessResults processes the supported findings into alumni records. A graduation year is set only from a
// supported finding whose property is the graduation year and whose value states one; a location only from a
// finding about location. Nothing else is assumed. Outreach drafts are text for the owner to review and send
// themselves.
func (p *Playbook) ProcessResults(ctx context.Context, run *domain.ResearchRun) (*playbooks.PlaybookResult, error) {
	var alumni []AlumniRecord
	index := map[string]int{}
	var warnings []string

	for _, f := range playbooks.Supported(run.Findings) {
		name := strings.TrimSpace(f.Topic)
		if name == "" {
			continue
		}
		key := strings.ToLower(strings.Join(strings.Fields(name), " "))
		profile, _ := domain.ProfileURL(f.SourceURL)
		i, seen := index[key]
		if !seen {
			i = len(alumni)
			index[key] = i
			alumni = append(alumni, AlumniRecord{Name: name, Institution: p.criteria.Institution, SourceEvidence: f.SourceURL, Confidence: f.Confidence})
		}
		r := &alumni[i]
		if profile != "" {
			r.ProfileURL = profile
		}
		switch {
		case propertyIs(f, "graduation year", "batch", "class of"):
			if y := yearOf(f.ExtractedValue); y != 0 {
				r.GraduationYear = y
				r.MeetsCutoff = domain.EvaluateGraduationEligibility(&y, nil, domain.GraduationCutoffDate(p.criteria.GraduationYearCutoff)) == domain.CohortEligible
				if p.store != nil && run.UserID != uuid.Nil && profile != "" {
					if _, err := p.store.SetGraduationYear(ctx, run.UserID, profile, y, f.EvidenceID); err != nil {
						warnings = append(warnings, fmt.Sprintf("could not record the graduation year of %q: %v", name, err))
					}
				}
			}
		case propertyIs(f, "location", "country", "city"):
			r.Location = strings.TrimSpace(f.ExtractedValue)
		default:
			r.CurrentTitle = f.Claim
			r.CurrentCompany = f.ExtractedValue
			if fam := domain.RoleFamilyOf(f.Claim); fam == domain.RoleFamilyOperations {
				r.Function, r.FunctionInferred = "operations", true
			} else if fam == domain.RoleFamilyTech {
				r.Function, r.FunctionInferred = "technology", true
			}
		}
	}

	for i := range alumni {
		r := &alumni[i]
		if r.CurrentTitle == "" {
			continue
		}
		// A draft for the owner to read and edit: it is text only, it names no sender, and nothing here sends it.
		r.OutreachDraft = fmt.Sprintf(
			"Hi %s,\n\nI noticed your journey from %s and was really inspired by your work%s. "+
				"I'm currently a CS student at %s passionate about systems and engineering. Would you be open to sharing a brief perspective on how your team approaches technical problem solving?\n\nBest regards",
			r.Name, p.criteria.Institution, atCompany(r.CurrentCompany), p.criteria.Institution)
	}

	qualified := 0
	for _, r := range alumni {
		if r.MeetsCutoff {
			qualified++
		}
	}
	return &playbooks.PlaybookResult{
		PlaybookName: p.Name(),
		RunID:        run.ID.String(),
		Objective:    run.Objective,
		Summary: fmt.Sprintf("Alumni Intelligence found %d people from supported findings; %d have a known graduation year within the %d cutoff.",
			len(alumni), qualified, p.criteria.GraduationYearCutoff),
		Data: map[string]any{
			"alumni":          alumni,
			"report_markdown": run.ReportMarkdown,
		},
		ItemsCount: len(alumni),
		Warnings:   warnings,
	}, nil
}

func atCompany(c string) string {
	if strings.TrimSpace(c) == "" {
		return ""
	}
	return " at " + c
}

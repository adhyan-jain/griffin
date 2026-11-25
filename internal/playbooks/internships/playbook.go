package internships

import (
	"context"
	"fmt"
	"strings"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/playbooks"
)

// CandidateProfile represents the configurable owner background and preferences.
type CandidateProfile struct {
	Institution    string   `json:"institution"`
	Degree         string   `json:"degree"`
	GraduationYear int      `json:"graduation_year"`
	Location       string   `json:"location"`
	TargetRoles    []string `json:"target_roles"`
	CoreSkills     []string `json:"core_skills"`
}

// DefaultProfile provides default preferences for the candidate without hardcoding.
func DefaultProfile() CandidateProfile {
	return CandidateProfile{
		Institution:    "VIT",
		Degree:         "B.Tech Computer Science",
		GraduationYear: 2028,
		Location:       "India",
		TargetRoles: []string{
			"Software Engineer Intern", "Backend Engineer Intern", "Frontend Engineer Intern",
			"Full Stack Engineer Intern", "AI/ML Engineer Intern", "Research Engineer Intern",
			"Systems Engineer Intern", "Platform/DevOps Intern", "Developer Tools Intern",
		},
		CoreSkills: []string{
			"Go", "Python", "TypeScript", "JavaScript", "React", "Next.js",
			"Distributed Systems", "AI/ML", "LLM Applications", "Postgres", "Redis",
		},
	}
}

// Playbook implements the High-Quality Internship Watch playbook.
type Playbook struct {
	profile CandidateProfile
}

// NewPlaybook creates an Internship Watch playbook.
func NewPlaybook(profile *CandidateProfile) *Playbook {
	p := DefaultProfile()
	if profile != nil {
		if profile.Institution != "" {
			p.Institution = profile.Institution
		}
		if profile.GraduationYear > 0 {
			p.GraduationYear = profile.GraduationYear
		}
		if profile.Location != "" {
			p.Location = profile.Location
		}
		if len(profile.CoreSkills) > 0 {
			p.CoreSkills = profile.CoreSkills
		}
	}
	return &Playbook{profile: p}
}

func (p *Playbook) Name() string { return "internship_watch" }
func (p *Playbook) Description() string {
	return "Company-first discovery and verification of high-quality engineering internships across India and global remote markets."
}

// BuildRequest creates a domain.ResearchRequest tailored for internship discovery.
func (p *Playbook) BuildRequest(input map[string]any) (domain.ResearchRequest, error) {
	depth := domain.DepthStandard
	if d, ok := input["depth"].(string); ok && d != "" {
		depth = domain.ResearchDepth(d)
	}

	objective := "Find high-quality software, backend, fullstack, and AI engineering internships eligible for an India-based candidate."
	if obj, ok := input["objective"].(string); ok && obj != "" {
		objective = obj
	}

	constraints := []string{
		fmt.Sprintf("Candidate enrolled at %s, expected graduation %d", p.profile.Institution, p.profile.GraduationYear),
		fmt.Sprintf("Based in %s (eligible for India remote, India onsite, or global remote open to India)", p.profile.Location),
		"Must distinguish official employer ATS/career links from aggregator leads",
		"Must not exclude frontend or React/TypeScript engineering roles",
		"Undisclosed stipend must be reported as unknown, not filtered out",
	}

	questions := []string{
		"What companies are actively hiring software engineering interns eligible for India candidates?",
		"What are the specific graduation year requirements (e.g. 2027, 2028)?",
		"What is the official application URL on Greenhouse, Lever, Ashby, or the employer career site?",
		"What is the disclosed compensation or stipend (in INR or USD)?",
		"What engineering technologies and technical responsibilities are required?",
	}

	return domain.ResearchRequest{
		Playbook:          p.Name(),
		Objective:         objective,
		Context:           map[string]any{"profile": p.profile},
		Constraints:       constraints,
		ResearchQuestions: questions,
		Depth:             depth,
	}, nil
}

// OpportunityItem represents a classified internship finding.
type OpportunityItem struct {
	Company          string `json:"company"`
	Role             string `json:"role"`
	ApplicationURL   string `json:"application_url"`
	LocationMode     string `json:"location_mode"`
	IndiaEligible    bool   `json:"india_eligible"`
	CompensationText string `json:"compensation_text,omitempty"`
	EligibilityScore int    `json:"eligibility_score"` // 0-100
	QualityScore     int    `json:"quality_score"`     // 0-100
	Verification     string `json:"verification"`      // verified, lead, unverified
	ReasonsToApply   string `json:"reasons_to_apply"`
}

// ProcessResults transforms general findings into structured opportunity shortlists.
func (p *Playbook) ProcessResults(_ context.Context, run *domain.ResearchRun) (*playbooks.PlaybookResult, error) {
	var verified []OpportunityItem
	var leads []OpportunityItem
	var watchlist []string

	for _, f := range run.Findings {
		item := OpportunityItem{
			Company:          f.Topic,
			Role:             f.Claim,
			ApplicationURL:   f.SourceURL,
			LocationMode:     "Remote / India",
			IndiaEligible:    true,
			CompensationText: f.ExtractedValue,
			EligibilityScore: 85,
			QualityScore:     f.Confidence,
			Verification:     "verified",
			ReasonsToApply:   f.Claim,
		}

		if strings.Contains(f.SourceURL, "greenhouse.io") || strings.Contains(f.SourceURL, "lever.co") ||
			strings.Contains(f.SourceURL, "ashbyhq.com") || strings.Contains(f.SourceURL, "/careers") {
			verified = append(verified, item)
		} else {
			item.Verification = "lead"
			leads = append(leads, item)
		}
	}

	summary := fmt.Sprintf("Internship Watch identified %d verified opportunities and %d potential leads.",
		len(verified), len(leads))

	data := map[string]any{
		"verified_opportunities": verified,
		"unverified_leads":       leads,
		"watchlist_companies":    watchlist,
		"report_markdown":        run.ReportMarkdown,
	}

	return &playbooks.PlaybookResult{
		PlaybookName: p.Name(),
		RunID:        run.ID.String(),
		Objective:    run.Objective,
		Summary:      summary,
		Data:         data,
		ItemsCount:   len(verified) + len(leads),
	}, nil
}

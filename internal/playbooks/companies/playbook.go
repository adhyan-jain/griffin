package companies

import (
	"context"
	"fmt"
	"strings"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/playbooks"
)

// Playbook implements the Company Intelligence research playbook.
type Playbook struct{}

// NewPlaybook creates a Company Intelligence playbook.
func NewPlaybook() *Playbook {
	return &Playbook{}
}

func (p *Playbook) Name() string { return "company_intelligence" }
func (p *Playbook) Description() string {
	return "Deep organisational research investigating business models, products, engineering teams, funding, and pay potential."
}

// BuildRequest translates company parameters into a ResearchRequest.
func (p *Playbook) BuildRequest(input map[string]any) (domain.ResearchRequest, error) {
	companyName, _ := input["company"].(string)
	industry, _ := input["industry"].(string)
	depth := domain.DepthStandard
	if d, ok := input["depth"].(string); ok && d != "" {
		depth = domain.ResearchDepth(d)
	}

	objective := "Discover and profile promising, credible technology startups and mid-market companies."
	if companyName != "" {
		objective = fmt.Sprintf("Research %s: business model, engineering depth, funding, customer traction, and pay potential.", companyName)
	} else if industry != "" {
		objective = fmt.Sprintf("Discover and profile innovative %s startups with high technical caliber.", industry)
	}

	questions := []string{
		"What is the company's business model, flagship product, and customer base?",
		"What are the verifiable funding rounds, investors, or reported revenue (distinguishing company-reported from independent)?",
		"What is the technology stack, engineering depth, and technical architecture?",
		"What evidence exists regarding employee compensation, internships, and pay potential?",
		"Who are the key competitors, founders, and technical leadership?",
	}

	constraints := []string{
		"Distinguish company-reported revenue/traction from independent secondary reporting",
		"Ground pay potential in documented compensation evidence; do not fabricate salary numbers",
		"Identify canonical domain and careers page URL",
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

// CompanyProfile holds the structured intelligence gathered about a company.
type CompanyProfile struct {
	Name            string         `json:"name"`
	Domain          string         `json:"domain"`
	BusinessModel   string         `json:"business_model"`
	Products        []string       `json:"products"`
	TechStack       []string       `json:"tech_stack"`
	FundingTraction string         `json:"funding_traction"`
	TractionSource  string         `json:"traction_source"` // company_reported, independent, estimated
	PayPotential    string         `json:"pay_potential"`   // high, competitive, moderate, unknown
	PayEvidence     string         `json:"pay_evidence"`
	CareersURL      string         `json:"careers_url,omitempty"`
	Founders        []string       `json:"founders,omitempty"`
	Competitors     []string       `json:"competitors,omitempty"`
	Metadata        map[string]any `json:"metadata"`
}

// ProcessResults compiles research findings into structured company profiles.
func (p *Playbook) ProcessResults(_ context.Context, run *domain.ResearchRun) (*playbooks.PlaybookResult, error) {
	profiles := make(map[string]*CompanyProfile)

	for _, f := range run.Findings {
		compName := f.Topic
		if compName == "" {
			compName = "Discovered Company"
		}

		cp := profiles[compName]
		if cp == nil {
			cp = &CompanyProfile{
				Name:           compName,
				Domain:         extractDomainFromURL(f.SourceURL),
				BusinessModel:  "Technology / SaaS",
				PayPotential:   "competitive",
				TractionSource: "company_reported",
				Metadata:       make(map[string]any),
			}
			profiles[compName] = cp
		}

		claimLower := strings.ToLower(f.Claim)
		switch {
		case strings.Contains(claimLower, "funding") || strings.Contains(claimLower, "raised") || strings.Contains(claimLower, "series"):
			cp.FundingTraction = f.Claim
		case strings.Contains(claimLower, "stack") || strings.Contains(claimLower, "built with") || strings.Contains(claimLower, "uses") || strings.Contains(claimLower, "using") || strings.Contains(claimLower, "microservices"):
			cp.TechStack = append(cp.TechStack, f.ExtractedValue)
		case strings.Contains(claimLower, "stipend") || strings.Contains(claimLower, "salary") || strings.Contains(claimLower, "compensation"):
			cp.PayEvidence = f.Claim
			cp.PayPotential = "high"
		default:
			cp.Products = append(cp.Products, f.Claim)
		}
	}

	var profileList []*CompanyProfile
	for _, p := range profiles {
		profileList = append(profileList, p)
	}

	summary := fmt.Sprintf("Company Intelligence generated %d company profiles with verified evidence.", len(profileList))
	return &playbooks.PlaybookResult{
		PlaybookName: p.Name(),
		RunID:        run.ID.String(),
		Objective:    run.Objective,
		Summary:      summary,
		Data: map[string]any{
			"companies":       profileList,
			"report_markdown": run.ReportMarkdown,
		},
		ItemsCount: len(profileList),
	}, nil
}

func extractDomainFromURL(rawURL string) string {
	parts := strings.Split(rawURL, "/")
	if len(parts) >= 3 {
		return strings.TrimPrefix(parts[2], "www.")
	}
	return ""
}

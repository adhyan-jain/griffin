package companies

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/playbooks"
)

// Store is where a company's research is attached: the existing companies table and its observations. The
// playbook keeps no company list of its own.
type Store interface {
	// ResolveCompany finds the owner's company by name, alias or domain (domain.NormalizeEmployer is the alias
	// rule the opportunity ingestion uses), creating a candidate row only when none matches.
	ResolveCompany(ctx context.Context, userID uuid.UUID, nameOrDomain string) (*domain.Company, error)
	// AddCompanyObservation stores one observation; the same value from the same source twice is stored once.
	AddCompanyObservation(ctx context.Context, o *domain.CompanyObservation) (bool, error)
}

// Playbook implements the Company Intelligence research playbook.
type Playbook struct {
	store Store
	now   func() time.Time
}

// NewPlaybook creates a Company Intelligence playbook that records nothing until it has a store.
func NewPlaybook() *Playbook {
	return &Playbook{now: time.Now}
}

// WithStore returns the playbook attached to the companies store.
func (p *Playbook) WithStore(s Store) *Playbook {
	p.store = s
	return p
}

func (p *Playbook) Name() string { return "company_intelligence" }
func (p *Playbook) Description() string {
	return "Deep organisational research investigating business models, products, engineering teams, funding, and pay."
}

// QueryTemplates are the angles a company is searched from.
func (p *Playbook) QueryTemplates() []string {
	return []string{
		"%s business model product",
		"%s funding investors valuation",
		"%s engineering tech stack",
		"%s competitors alternatives",
		"%s careers jobs hiring",
	}
}

// Subject is the company the run is about.
func (p *Playbook) Subject(ctx map[string]any) string { return playbooks.StringIn(ctx, "company") }

// DefaultQuestions are asked of every company.
func (p *Playbook) DefaultQuestions() []string {
	return []string{
		"What is the company's business model, flagship product, and customer base?",
		"What are the verifiable funding rounds, investors, or reported revenue (distinguishing company-reported from independent)?",
		"What is the technology stack, engineering depth, and technical architecture?",
		"What evidence exists regarding employee compensation, internships, and pay?",
		"Who are the key competitors, founders, and technical leadership?",
	}
}

// PlanGuidance is the company wording of the planning prompt.
func (p *Playbook) PlanGuidance() string {
	return "This is company research. Cover the business model and product, funding and traction (say whether each figure is company-reported or independent), hiring and pay evidence, and the company's official domain and careers page."
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
		objective = fmt.Sprintf("Research %s: business model, engineering depth, funding, customer traction, and pay.", companyName)
	} else if industry != "" {
		objective = fmt.Sprintf("Discover and profile innovative %s startups with high technical caliber.", industry)
	}

	constraints := []string{
		"Distinguish company-reported revenue/traction from independent secondary reporting",
		"Ground pay in documented compensation evidence; do not fabricate salary numbers",
		"Identify canonical domain and careers page URL",
	}

	return domain.ResearchRequest{
		Playbook:          p.Name(),
		Objective:         objective,
		Context:           input,
		Constraints:       constraints,
		ResearchQuestions: p.DefaultQuestions(),
		Depth:             depth,
	}, nil
}

// CompanyProfile holds what the supported findings say about a company. A field no finding fills stays empty:
// unknown is unknown.
type CompanyProfile struct {
	Name            string   `json:"name"`
	CompanyID       string   `json:"company_id,omitempty"`
	Domain          string   `json:"domain,omitempty"`
	Products        []string `json:"products,omitempty"`
	TechStack       []string `json:"tech_stack,omitempty"`
	FundingTraction string   `json:"funding_traction,omitempty"`
	FundingOrigin   string   `json:"funding_origin,omitempty"` // company_reported, independent, unknown
	PayEvidence     string   `json:"pay_evidence,omitempty"`
	Observations    int      `json:"observations_recorded"`
}

var (
	reFunding    = regexp.MustCompile(`funding|raise|series|round|invest`)
	reRevenue    = regexp.MustCompile(`revenue|\barr\b|sales`)
	reProfit     = regexp.MustCompile(`profit|break.?even|burn`)
	reStipend    = regexp.MustCompile(`stipend|intern`)
	reAdvertised = regexp.MustCompile(`advertis|posted|listing|job post`)
	reEmployee   = regexp.MustCompile(`employee|glassdoor|levels|self.?report|reviews?`)
	rePay        = regexp.MustCompile(`pay|salary|compensation|ctc`)
	reHiring     = regexp.MustCompile(`hiring|opening|headcount|recruit`)
	reProduct    = regexp.MustCompile(`product`)
	reMarket     = regexp.MustCompile(`market|customer|competitor|industry`)
)

// PropertyOf maps the property a finding names to one observation property. A finding that names none, or one that
// cannot be told apart (a bare "salary" is neither advertised nor employee-reported), is "" and is not recorded.
func PropertyOf(property string) string {
	s := strings.ToLower(strings.TrimSpace(property))
	if s == "" {
		return ""
	}
	if domain.IsCompanyProperty(strings.ReplaceAll(s, " ", "_")) {
		return strings.ReplaceAll(s, " ", "_")
	}
	switch {
	case reProfit.MatchString(s):
		return domain.PropProfitability
	case reRevenue.MatchString(s):
		return domain.PropRevenue
	case reFunding.MatchString(s):
		return domain.PropFunding
	case rePay.MatchString(s) || reStipend.MatchString(s):
		switch {
		case reStipend.MatchString(s):
			return domain.PropInternshipStipend
		case reEmployee.MatchString(s):
			return domain.PropEmployeeReportedPay
		case reAdvertised.MatchString(s):
			return domain.PropAdvertisedPay
		}
		return ""
	case reHiring.MatchString(s):
		return domain.PropHiringSignal
	case reProduct.MatchString(s):
		return domain.PropProduct
	case reMarket.MatchString(s):
		return domain.PropMarket
	}
	return ""
}

var atsHosts = []string{"greenhouse.io", "lever.co", "ashbyhq.com", "workable.com", "smartrecruiters.com", "recruitee.com"}

// ClaimOriginFor says who a claim comes from, by the page it was read on: the company's own domain (or its job
// board) is company-reported. Nothing else is told apart from the URL alone, so everything else is unknown.
func ClaimOriginFor(sourceURL, companyDomain string) string {
	u, err := url.Parse(strings.TrimSpace(sourceURL))
	if err != nil || u.Hostname() == "" {
		return domain.OriginUnknown
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	d := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(companyDomain)), "www.")
	if d != "" && (host == d || strings.HasSuffix(host, "."+d)) {
		return domain.OriginCompanyReported
	}
	for _, a := range atsHosts {
		if host == a || strings.HasSuffix(host, "."+a) {
			return domain.OriginCompanyReported
		}
	}
	return domain.OriginUnknown
}

// ProcessResults compiles the run's supported findings into company profiles and, when it has a store, records
// each typed fact as an observation on the company's existing row. Unverified and conflicting findings are ignored.
func (p *Playbook) ProcessResults(ctx context.Context, run *domain.ResearchRun) (*playbooks.PlaybookResult, error) {
	profiles := make(map[string]*CompanyProfile)
	var order []string
	var warnings []string

	for _, f := range playbooks.Supported(run.Findings) {
		name := strings.TrimSpace(f.Subject)
		if name == "" {
			name = strings.TrimSpace(f.Topic)
		}
		if name == "" {
			continue // no company is named, so nothing can be attached to one
		}
		key := domain.NormalizeEmployer(name)
		cp := profiles[key]
		if cp == nil {
			cp = &CompanyProfile{Name: name}
			profiles[key] = cp
			order = append(order, key)
			if p.store != nil && run.UserID != uuid.Nil {
				c, err := p.store.ResolveCompany(ctx, run.UserID, name)
				if err != nil {
					warnings = append(warnings, fmt.Sprintf("could not resolve company %q: %v", name, err))
				} else {
					cp.CompanyID = c.ID.String()
					cp.Name = c.Name
					if c.Domain != nil {
						cp.Domain = *c.Domain
					}
				}
			}
		}
		origin := ClaimOriginFor(f.SourceURL, cp.Domain)
		prop := PropertyOf(f.Property)
		if prop == "" {
			// A claim's own words settle only the unambiguous properties.
			if c := PropertyOf(f.Claim); c == domain.PropFunding || c == domain.PropRevenue || c == domain.PropProfitability {
				prop = c
			}
		}
		value := strings.TrimSpace(f.ExtractedValue)
		if value == "" {
			value = strings.TrimSpace(f.Claim)
		}

		switch prop {
		case domain.PropFunding:
			cp.FundingTraction, cp.FundingOrigin = value, origin
		case domain.PropProduct:
			cp.Products = append(cp.Products, value)
		case domain.PropInternshipStipend, domain.PropAdvertisedPay, domain.PropEmployeeReportedPay:
			cp.PayEvidence = value
		}
		if lc := strings.ToLower(f.Claim + " " + f.Property); strings.Contains(lc, "stack") || strings.Contains(lc, "built with") || strings.Contains(lc, " using ") || strings.Contains(lc, "microservices") {
			cp.TechStack = append(cp.TechStack, value)
		}

		if prop == "" || p.store == nil || cp.CompanyID == "" || f.EvidenceID == nil || value == "" {
			continue
		}
		cid, _ := uuid.Parse(cp.CompanyID)
		added, err := p.store.AddCompanyObservation(ctx, &domain.CompanyObservation{
			UserID: run.UserID, CompanyID: cid, Property: prop, Value: value, EvidenceID: f.EvidenceID,
			SourceURL: f.SourceURL, ClaimOrigin: origin, ObservedAt: p.now().UTC(),
		})
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("could not record %s for %q: %v", prop, cp.Name, err))
		} else if added {
			cp.Observations++
		}
	}

	sort.Strings(order)
	profileList := make([]*CompanyProfile, 0, len(order))
	for _, k := range order {
		profileList = append(profileList, profiles[k])
	}
	return &playbooks.PlaybookResult{
		PlaybookName: p.Name(),
		RunID:        run.ID.String(),
		Objective:    run.Objective,
		Summary:      fmt.Sprintf("Company Intelligence built %d company profiles from supported findings.", len(profileList)),
		Data: map[string]any{
			"companies":       profileList,
			"report_markdown": run.ReportMarkdown,
		},
		ItemsCount: len(profileList),
		Warnings:   warnings,
	}, nil
}

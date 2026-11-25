package companies

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
)

func TestCompanyIntelligencePlaybook(t *testing.T) {
	pb := NewPlaybook()

	if pb.Name() != "company_intelligence" {
		t.Fatalf("expected name company_intelligence, got %s", pb.Name())
	}

	req, err := pb.BuildRequest(map[string]any{"company": "ProcureHive", "industry": "B2B Supply Chain"})
	if err != nil {
		t.Fatalf("BuildRequest error: %v", err)
	}
	if len(req.ResearchQuestions) < 3 {
		t.Fatal("expected research questions for company intelligence")
	}

	run := &domain.ResearchRun{
		ID:        uuid.New(),
		Objective: req.Objective,
		Findings: []domain.ResearchFinding{
			{
				Topic:          "ProcureHive",
				Claim:          "Raised $8M Series A from Nexus Venture Partners",
				SourceURL:      "https://techcrunch.com/2026/02/procurehive-funding",
				ExtractedValue: "$8M",
				Confidence:     92,
			},
			{
				Topic:          "ProcureHive",
				Claim:          "Engineered backend microservices using Go and PostgreSQL",
				SourceURL:      "https://procurehive.com/engineering",
				ExtractedValue: "Go, PostgreSQL",
				Confidence:     95,
			},
		},
		ReportMarkdown: "# Company Profile: ProcureHive",
	}

	res, err := pb.ProcessResults(context.Background(), run)
	if err != nil {
		t.Fatalf("ProcessResults error: %v", err)
	}
	if res.ItemsCount != 1 {
		t.Fatalf("expected 1 company profile, got %d", res.ItemsCount)
	}

	comps, ok := res.Data["companies"].([]*CompanyProfile)
	if !ok || len(comps) != 1 {
		t.Fatalf("expected 1 company in profile list, got %v", comps)
	}
	p := comps[0]
	if p.Name != "ProcureHive" {
		t.Fatalf("expected company ProcureHive, got %s", p.Name)
	}
	if p.FundingTraction == "" {
		t.Fatal("expected funding traction to be extracted")
	}
	if len(p.TechStack) == 0 {
		t.Fatal("expected tech stack to be extracted")
	}
}

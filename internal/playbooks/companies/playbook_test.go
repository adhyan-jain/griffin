package companies

import (
	"encoding/json"

	"context"
	"github.com/stretchr/testify/require"
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
				Status:         domain.EvidenceSupported,
			},
			{
				Topic:          "ProcureHive",
				Claim:          "Engineered backend microservices using Go and PostgreSQL",
				SourceURL:      "https://procurehive.com/engineering",
				ExtractedValue: "Go, PostgreSQL",
				Confidence:     95,
				Status:         domain.EvidenceSupported,
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

type fakeStore struct {
	company *domain.Company
	added   []*domain.CompanyObservation
}

func (f *fakeStore) ResolveCompany(_ context.Context, _ uuid.UUID, name string) (*domain.Company, error) {
	if f.company == nil {
		d := "acme.io"
		f.company = &domain.Company{ID: uuid.New(), Name: name, Domain: &d}
	}
	return f.company, nil
}
func (f *fakeStore) AddCompanyObservation(_ context.Context, o *domain.CompanyObservation) (bool, error) {
	f.added = append(f.added, o)
	return true, nil
}

func TestProcessResultsIgnoresUnverifiedAndConflictingFindings(t *testing.T) {
	store := &fakeStore{}
	pb := NewPlaybook().WithStore(store)
	ev := uuid.New()
	run := &domain.ResearchRun{ID: uuid.New(), UserID: uuid.New(), Findings: []domain.ResearchFinding{
		{Topic: "Acme", Subject: "Acme", Property: "funding", Claim: "Acme raised $5M", ExtractedValue: "$5M", SourceURL: "https://acme.io/blog", EvidenceID: &ev, Status: domain.EvidenceSupported},
		{Topic: "Acme", Subject: "Acme", Property: "funding", Claim: "Acme raised $50M", ExtractedValue: "$50M", SourceURL: "https://blog.example.com/a", EvidenceID: &ev, Status: domain.EvidenceUnverified},
		{Topic: "Acme", Subject: "Acme", Property: "revenue", Claim: "Acme earns $9M", ExtractedValue: "$9M", SourceURL: "https://blog.example.com/b", EvidenceID: &ev, Status: domain.EvidenceConflicting},
		{Topic: "Ghost", Subject: "Ghost", Property: "funding", Claim: "Ghost raised $1M", ExtractedValue: "$1M", Status: ""},
	}}
	res, err := pb.ProcessResults(context.Background(), run)
	require.NoError(t, err)
	require.Equal(t, 1, res.ItemsCount, "only the company with a supported finding")
	require.Len(t, store.added, 1)
	require.Equal(t, "$5M", store.added[0].Value)
	require.Equal(t, domain.PropFunding, store.added[0].Property)
	require.Equal(t, domain.OriginCompanyReported, store.added[0].ClaimOrigin, "the page is on the company's own domain")
	require.Equal(t, &ev, store.added[0].EvidenceID)
}

func TestNothingIsInventedForACompany(t *testing.T) {
	pb := NewPlaybook()
	run := &domain.ResearchRun{ID: uuid.New(), Findings: []domain.ResearchFinding{
		{Topic: "Acme", Claim: "Acme makes widgets", SourceURL: "https://news.example.com/acme", Status: domain.EvidenceSupported},
	}}
	res, err := pb.ProcessResults(context.Background(), run)
	require.NoError(t, err)
	b, err := json.Marshal(res.Data["companies"])
	require.NoError(t, err)
	for _, invented := range []string{"SaaS", "competitive", "company_reported", "high", "verified evidence"} {
		require.NotContains(t, string(b)+res.Summary, invented)
	}
}

func TestPropertiesAreSeparateAndOriginIsUnknownByDefault(t *testing.T) {
	for in, want := range map[string]string{
		"funding": domain.PropFunding, "annual revenue": domain.PropRevenue, "profitability": domain.PropProfitability,
		"internship stipend": domain.PropInternshipStipend, "advertised salary": domain.PropAdvertisedPay,
		"employee reported salary": domain.PropEmployeeReportedPay, "hiring signal": domain.PropHiringSignal,
		"salary": "", "": "", "favourite colour": "",
	} {
		require.Equal(t, want, PropertyOf(in), in)
	}
	require.Equal(t, domain.OriginCompanyReported, ClaimOriginFor("https://careers.acme.io/x", "acme.io"))
	require.Equal(t, domain.OriginCompanyReported, ClaimOriginFor("https://boards.greenhouse.io/acme", ""))
	require.Equal(t, domain.OriginUnknown, ClaimOriginFor("https://techcrunch.com/acme", "acme.io"))
	require.Equal(t, domain.OriginUnknown, ClaimOriginFor("not a url", "acme.io"))
}

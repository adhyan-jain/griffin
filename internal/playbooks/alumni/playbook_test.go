package alumni

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
)

func TestAlumniIntelligencePlaybook(t *testing.T) {
	pb := NewPlaybook(nil)

	if pb.Name() != "alumni_intelligence" {
		t.Fatalf("expected name alumni_intelligence, got %s", pb.Name())
	}

	req, err := pb.BuildRequest(map[string]any{"company": "Razorpay"})
	if err != nil {
		t.Fatalf("BuildRequest error: %v", err)
	}

	run := &domain.ResearchRun{
		ID:        uuid.New(),
		Objective: req.Objective,
		Findings: []domain.ResearchFinding{
			{
				Topic:          "Vikramaditya Roy",
				Claim:          "Staff Software Engineer",
				ExtractedValue: "Razorpay",
				SourceURL:      "https://razorpay.com/team",
				Confidence:     90,
				Status:         domain.EvidenceSupported,
			},
			{
				Topic:          "Priya Sundaram",
				Claim:          "Director of Operations & Supply Chain",
				ExtractedValue: "Delhivery",
				SourceURL:      "https://example.com/speaker-bio",
				Confidence:     88,
				Status:         domain.EvidenceSupported,
			},
		},
		ReportMarkdown: "# Alumni Findings",
	}

	res, err := pb.ProcessResults(context.Background(), run)
	if err != nil {
		t.Fatalf("ProcessResults error: %v", err)
	}
	if res.ItemsCount != 2 {
		t.Fatalf("expected 2 alumni, got %d", res.ItemsCount)
	}

	records, ok := res.Data["alumni"].([]AlumniRecord)
	if !ok || len(records) != 2 {
		t.Fatalf("expected 2 alumni records, got %v", records)
	}

	if records[0].Function != "technology" {
		t.Fatalf("expected technology function for SWE, got %s", records[0].Function)
	}
	if records[1].Function != "operations" {
		t.Fatalf("expected operations function for Operations Director, got %s", records[1].Function)
	}
	if records[0].OutreachDraft == "" {
		t.Fatal("expected outreach draft to be prepared")
	}
}

type fakePeople struct{ years map[string]int }

func (f *fakePeople) SetGraduationYear(_ context.Context, _ uuid.UUID, profile string, year int, _ *uuid.UUID) (bool, error) {
	f.years[profile] = year
	return true, nil
}

func TestProcessResultsIgnoresUnverifiedAndNeverInventsFacts(t *testing.T) {
	people := &fakePeople{years: map[string]int{}}
	pb := NewPlaybook(nil).WithStore(people)
	ev := uuid.New()
	run := &domain.ResearchRun{ID: uuid.New(), UserID: uuid.New(), Findings: []domain.ResearchFinding{
		{Topic: "Riya Sen", Property: "role", Claim: "Staff Software Engineer", ExtractedValue: "Razorpay", SourceURL: "https://razorpay.com/team", Status: domain.EvidenceSupported},
		{Topic: "Unverified Person", Claim: "Software Engineer", ExtractedValue: "X", Status: domain.EvidenceUnverified},
		{Topic: "Conflicted Person", Claim: "Software Engineer", ExtractedValue: "Y", Status: domain.EvidenceConflicting},
		{Topic: "Year Person", Property: "graduation_year", Claim: "Class of 2019", ExtractedValue: "2019", SourceURL: "https://www.linkedin.com/in/year-person/", EvidenceID: &ev, Status: domain.EvidenceSupported},
	}}
	res, err := pb.ProcessResults(context.Background(), run)
	require.NoError(t, err)
	records := res.Data["alumni"].([]AlumniRecord)
	require.Len(t, records, 2, "unverified and conflicting findings are ignored")

	riya := records[0]
	require.Equal(t, 0, riya.GraduationYear, "a year is never defaulted")
	require.False(t, riya.MeetsCutoff, "an unknown year never passes the cutoff")
	require.Empty(t, riya.Location, "a location is never defaulted")
	require.True(t, riya.FunctionInferred, "the role family is a guess from the title")
	require.NotContains(t, riya.OutreachDraft, "Asha")

	year := records[1]
	require.Equal(t, 2019, year.GraduationYear)
	require.True(t, year.MeetsCutoff)
	require.Equal(t, 2019, people.years["https://www.linkedin.com/in/year-person/"], "recorded on the person with that profile link")
}

func TestCutoffIsConfigurable(t *testing.T) {
	require.Equal(t, 2022, NewPlaybook(nil).criteria.GraduationYearCutoff)
	require.Equal(t, 2019, NewPlaybook(&Criteria{GraduationYearCutoff: 2019}).criteria.GraduationYearCutoff)
}

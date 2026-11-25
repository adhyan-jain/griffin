package alumni

import (
	"context"
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
			},
			{
				Topic:          "Priya Sundaram",
				Claim:          "Director of Operations & Supply Chain",
				ExtractedValue: "Delhivery",
				SourceURL:      "https://example.com/speaker-bio",
				Confidence:     88,
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

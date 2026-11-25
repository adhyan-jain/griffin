package internships

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
)

func TestInternshipWatchPlaybook(t *testing.T) {
	pb := NewPlaybook(nil)

	if pb.Name() != "internship_watch" {
		t.Fatalf("expected name internship_watch, got %s", pb.Name())
	}

	req, err := pb.BuildRequest(map[string]any{"depth": "deep"})
	if err != nil {
		t.Fatalf("BuildRequest error: %v", err)
	}
	if req.Depth != domain.DepthDeep {
		t.Fatalf("expected depth deep, got %s", req.Depth)
	}
	if len(req.Constraints) < 3 {
		t.Fatal("expected candidate constraints")
	}

	// Test processing results
	run := &domain.ResearchRun{
		ID:        uuid.New(),
		Objective: req.Objective,
		Findings: []domain.ResearchFinding{
			{
				Topic:          "Razorpay",
				Claim:          "Software Engineer Intern",
				SourceURL:      "https://boards.greenhouse.io/razorpay/jobs/123",
				ExtractedValue: "₹45,000 / month",
				Confidence:     95,
			},
			{
				Topic:          "StartupX",
				Claim:          "Frontend Engineer Intern",
				SourceURL:      "https://example.com/blog/hiring",
				ExtractedValue: "Undisclosed",
				Confidence:     70,
			},
		},
		ReportMarkdown: "# Internship Shortlist",
	}

	res, err := pb.ProcessResults(context.Background(), run)
	if err != nil {
		t.Fatalf("ProcessResults error: %v", err)
	}
	if res.ItemsCount != 2 {
		t.Fatalf("expected 2 items, got %d", res.ItemsCount)
	}

	verified, ok := res.Data["verified_opportunities"].([]OpportunityItem)
	if !ok || len(verified) != 1 {
		t.Fatalf("expected 1 verified opportunity on Greenhouse ATS, got %v", verified)
	}
	if verified[0].Company != "Razorpay" || verified[0].Verification != "verified" {
		t.Fatalf("unexpected verified item: %+v", verified[0])
	}

	leads, ok := res.Data["unverified_leads"].([]OpportunityItem)
	if !ok || len(leads) != 1 {
		t.Fatalf("expected 1 unverified lead, got %v", leads)
	}
	if leads[0].Company != "StartupX" || leads[0].Verification != "lead" {
		t.Fatalf("unexpected lead item: %+v", leads[0])
	}
}

package main

import (
	"strings"
	"testing"

	"github.com/griffin/core/internal/domain"
)

func TestProfileTextKeepsFactsVerbatimAndLeavesOutThePhone(t *testing.T) {
	p := &domain.OwnerProfile{
		FullName: "Ada Lovelace", Email: "ada@example.com", Phone: "+91 99999 99999", GitHub: "https://github.com/ada",
		Degree: "B.Tech CSE", School: "VIT", GradYear: "2028",
		Facts: []domain.ProfileFact{{ID: "f1", Project: "Engine", Text: "Built the Analytical Engine"}},
	}
	out := profileText(p)
	for _, want := range []string{"Ada Lovelace", "ada@example.com", "https://github.com/ada", "[f1] Engine: Built the Analytical Engine", "B.Tech CSE, VIT"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "99999") {
		t.Fatal("the phone number must never be uploaded")
	}
}

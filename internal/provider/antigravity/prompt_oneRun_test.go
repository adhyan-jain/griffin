package antigravity

import (
	"strings"
	"testing"

	"github.com/griffin/core/internal/domain"
)

func TestEveryPromptSaysToFinishInThisRun(t *testing.T) {
	p := buildPrompt(&domain.Task{Title: "ALUMNI OUTREACH TRACKER", Goal: "grow the list"}, 1, RuntimeFacts{})
	if !strings.Contains(p, "Never start background jobs") {
		t.Fatalf("prompt lacks the one-run rule:\n%s", p)
	}
}

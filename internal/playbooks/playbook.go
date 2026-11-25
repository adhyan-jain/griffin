package playbooks

import (
	"context"

	"github.com/griffin/core/internal/domain"
)

// PlaybookResult encapsulates the structured output of a domain research playbook.
type PlaybookResult struct {
	PlaybookName string         `json:"playbook_name"`
	RunID        string         `json:"run_id"`
	Objective    string         `json:"objective"`
	Summary      string         `json:"summary"`
	Data         map[string]any `json:"data"`
	ItemsCount   int            `json:"items_count"`
	Warnings     []string       `json:"warnings,omitempty"`
}

// Playbook is the interface implemented by all domain-specific research playbooks.
// A playbook defines declarative research questions, evaluation criteria, and transforms
// general research findings into high-value domain artifacts.
type Playbook interface {
	Name() string
	Description() string
	BuildRequest(input map[string]any) (domain.ResearchRequest, error)
	ProcessResults(ctx context.Context, run *domain.ResearchRun) (*PlaybookResult, error)
}

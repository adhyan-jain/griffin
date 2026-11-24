package playbooks

import (
	"context"
	"fmt"
	"strings"

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

// Playbook is the interface implemented by all domain-specific research playbooks. The generic engine works with
// none: a playbook only adds domain wording (query templates, default questions, plan guidance) and turns the
// run's supported findings into domain records.
type Playbook interface {
	Name() string
	Description() string
	BuildRequest(input map[string]any) (domain.ResearchRequest, error)
	// QueryTemplates are search queries with one %s for the subject (Subject of the run's context).
	QueryTemplates() []string
	// Subject names what the run is about, read from the run's context ("" when it names none).
	Subject(ctx map[string]any) string
	// DefaultQuestions are asked when the request names none.
	DefaultQuestions() []string
	// PlanGuidance is domain wording added to the planning prompt ("" for none).
	PlanGuidance() string
	// ProcessResults turns the run's findings into domain records. It is called once, after synthesis, and
	// ignores every finding that is not supported.
	ProcessResults(ctx context.Context, run *domain.ResearchRun) (*PlaybookResult, error)
}

// Registry maps the playbook ids a request may name (domain.ResearchPlaybooks) to their playbook. "generic" has
// none: a missing entry means the plain engine.
type Registry map[string]Playbook

// For returns the run's playbook, or nil.
func (r Registry) For(id string) Playbook {
	if r == nil {
		return nil
	}
	return r[id]
}

// Supported keeps only the findings whose quote was found in the fetched page. Unverified and conflicting
// findings are never treated as fact by a playbook.
func Supported(findings []domain.ResearchFinding) []domain.ResearchFinding {
	var out []domain.ResearchFinding
	for _, f := range findings {
		if f.Status == domain.EvidenceSupported {
			out = append(out, f)
		}
	}
	return out
}

// ExpandTemplates fills each template's %s with subject; no subject gives no queries.
func ExpandTemplates(templates []string, subject string) []string {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return nil
	}
	out := make([]string, 0, len(templates))
	for _, t := range templates {
		out = append(out, fmt.Sprintf(t, subject))
	}
	return out
}

// StringIn reads a string from a request context.
func StringIn(ctx map[string]any, key string) string {
	s, _ := ctx[key].(string)
	return strings.TrimSpace(s)
}

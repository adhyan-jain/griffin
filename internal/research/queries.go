package research

import (
	"fmt"
	"strings"
)

// QueryExpander generates targeted, multi-angle search queries.
type QueryExpander struct {
	history map[string]bool
}

// NewQueryExpander creates a QueryExpander with an empty history set.
func NewQueryExpander() *QueryExpander {
	return &QueryExpander{history: make(map[string]bool)}
}

// MarkExecuted adds a query to the history to prevent duplicate searches.
func (q *QueryExpander) MarkExecuted(query string) {
	q.history[strings.ToLower(strings.TrimSpace(query))] = true
}

// HasExecuted reports whether a query was already run.
func (q *QueryExpander) HasExecuted(query string) bool {
	return q.history[strings.ToLower(strings.TrimSpace(query))]
}

// ExpandLead generates follow-up queries for a newly discovered entity or lead.
func (q *QueryExpander) ExpandLead(lead, contextObjective string) []string {
	lead = strings.TrimSpace(lead)
	if lead == "" {
		return nil
	}

	var candidates []string
	candidates = append(candidates,
		fmt.Sprintf("%s business model product", lead),
		fmt.Sprintf("%s funding investors valuation", lead),
		fmt.Sprintf("%s engineering tech stack", lead),
		fmt.Sprintf("%s competitors alternatives", lead),
		fmt.Sprintf("%s careers jobs hiring", lead),
	)

	var filtered []string
	for _, c := range candidates {
		norm := strings.ToLower(strings.TrimSpace(c))
		if !q.history[norm] {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

// ExpandGap generates targeted queries to resolve an unanswered question.
func (q *QueryExpander) ExpandGap(question string) []string {
	qClean := strings.TrimSpace(question)
	qClean = strings.TrimSuffix(qClean, "?")

	var candidates []string
	candidates = append(candidates, qClean)

	words := strings.Fields(qClean)
	if len(words) > 3 {
		// Concise search without stop words
		candidates = append(candidates, strings.Join(words[:len(words)-1], " "))
	}

	var filtered []string
	for _, c := range candidates {
		norm := strings.ToLower(strings.TrimSpace(c))
		if !q.history[norm] {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

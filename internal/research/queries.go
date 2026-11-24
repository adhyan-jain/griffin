package research

import (
	"strings"

	"github.com/griffin/core/internal/playbooks"
)

// QueryExpander generates targeted, multi-angle search queries. Its history is per run: the engine builds one from
// the run's own persisted executed queries (NewQueryExpanderFrom), so no state is shared between runs or users and
// nothing here needs a lock.
type QueryExpander struct {
	history map[string]bool
}

// NewQueryExpander creates a QueryExpander with an empty history set.
func NewQueryExpander() *QueryExpander {
	return &QueryExpander{history: make(map[string]bool)}
}

// NewQueryExpanderFrom creates a QueryExpander whose history is the given queries.
func NewQueryExpanderFrom(queries ...[]string) *QueryExpander {
	q := NewQueryExpander()
	for _, list := range queries {
		for _, s := range list {
			q.MarkExecuted(s)
		}
	}
	return q
}

// MarkExecuted adds a query to the history to prevent duplicate searches.
func (q *QueryExpander) MarkExecuted(query string) {
	q.history[strings.ToLower(strings.TrimSpace(query))] = true
}

// HasExecuted reports whether a query was already run.
func (q *QueryExpander) HasExecuted(query string) bool {
	return q.history[strings.ToLower(strings.TrimSpace(query))]
}

// ExpandTemplates fills a playbook's query templates (each with one %s) with the subject, leaving out queries
// already run. The generic engine has no templates.
func (q *QueryExpander) ExpandTemplates(templates []string, subject string) []string {
	var filtered []string
	for _, c := range playbooks.ExpandTemplates(templates, subject) {
		if !q.HasExecuted(c) {
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

package research

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"unicode"

	"github.com/griffin/core/internal/domain"
)

// minQuoteChars is the shortest quote that can support a claim; a few characters match almost any page.
const minQuoteChars = 12

var quoteFolds = strings.NewReplacer(
	"‘", "'", "’", "'", "‚", "'", "‛", "'", "′", "'",
	"“", `"`, "”", `"`, "„", `"`, "‟", `"`, "″", `"`,
	"‐", "-", "‑", "-", "‒", "-", "–", "-", "—", "-", "−", "-",
	" ", " ", " ", " ", " ", " ", "​", "",
)

// NormalizeText lower-cases s, folds smart quotes and dashes to plain ones and collapses all whitespace runs to a
// single space, so a quote can be compared with page text that was laid out differently.
func NormalizeText(s string) string {
	s = quoteFolds.Replace(strings.ToLower(s))
	return strings.Join(strings.FieldsFunc(s, unicode.IsSpace), " ")
}

// QuoteInText reports whether quote, normalised, is a substring of the normalised page text. A quote shorter than
// minQuoteChars never counts.
func QuoteInText(quote, pageText string) bool {
	q := NormalizeText(quote)
	if len([]rune(q)) < minQuoteChars {
		return false
	}
	return strings.Contains(NormalizeText(pageText), q)
}

func clampConfidence(c int) int {
	return max(0, min(100, c))
}

// dedupeKey identifies one claim and quote from one page, so extracting the same page twice stores it once.
func dedupeKey(canonicalURL, claim, quote string) string {
	h := sha256.Sum256([]byte(canonicalURL + "\x00" + NormalizeText(claim) + "\x00" + NormalizeText(quote)))
	return hex.EncodeToString(h[:])
}

// ConflictValue is one source's value in a conflict.
type ConflictValue struct {
	Value      string `json:"value"`
	SourceURL  string `json:"source_url"`
	FindingID  string `json:"finding_id"`
	EvidenceID string `json:"evidence_id,omitempty"`
}

// Conflict is a subject and property on which different sources give different values. Neither is picked.
type Conflict struct {
	Subject  string          `json:"subject"`
	Property string          `json:"property"`
	Values   []ConflictValue `json:"values"`
}

func conflictable(f domain.ResearchFinding) bool {
	return f.Status == domain.EvidenceSupported || f.Status == domain.EvidenceConflicting
}

// DetectConflicts groups the supported findings by (subject, property) and, where different sources give
// different values, marks every finding of the group conflicting. It returns the conflicts and the indexes of the
// findings whose status it just changed. A finding keeps its conflicting status once it has it.
func DetectConflicts(findings []domain.ResearchFinding) ([]Conflict, []int) {
	type entry struct {
		idx   int
		value string
		src   string
	}
	groups := map[string][]entry{}
	var order []string
	for i, f := range findings {
		subj, prop, val := NormalizeText(f.Subject), NormalizeText(f.Property), NormalizeText(f.ExtractedValue)
		if !conflictable(f) || subj == "" || prop == "" || val == "" {
			continue
		}
		k := subj + "\x00" + prop
		if _, seen := groups[k]; !seen {
			order = append(order, k)
		}
		groups[k] = append(groups[k], entry{i, val, CanonicalURL(f.SourceURL)})
	}
	var conflicts []Conflict
	var changed []int
	for _, k := range order {
		g := groups[k]
		values, sources := map[string]bool{}, map[string]bool{}
		for _, e := range g {
			values[e.value] = true
			sources[e.src] = true
		}
		// A conflict needs two different values that come from two different sources.
		if len(values) < 2 || len(sources) < 2 {
			continue
		}
		first := findings[g[0].idx]
		c := Conflict{Subject: first.Subject, Property: first.Property}
		for _, e := range g {
			f := &findings[e.idx]
			if f.Status != domain.EvidenceConflicting {
				f.Status = domain.EvidenceConflicting
				changed = append(changed, e.idx)
			}
			cv := ConflictValue{Value: f.ExtractedValue, SourceURL: f.SourceURL, FindingID: f.ID}
			if f.EvidenceID != nil {
				cv.EvidenceID = f.EvidenceID.String()
			}
			c.Values = append(c.Values, cv)
		}
		conflicts = append(conflicts, c)
	}
	sort.Ints(changed)
	return conflicts, changed
}

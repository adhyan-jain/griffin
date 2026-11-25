// Package eval is the evaluation benchmark of Griffin's opportunity system. It holds a dataset of real postings
// (captured from public employer boards and aggregator pages, with the URL and day they were read) and runs the
// pipeline's deterministic stages over them offline: no network and no model. Every label in the dataset is a
// proposal until the owner reviews it (Labels.Reviewed), and metrics computed on unreviewed labels are reported as
// such, never as measured accuracy.
package eval

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/griffin/core/internal/jobs"
)

//go:embed testdata/dataset.json
var embeddedDataset []byte

// Eligibility labels, in the order the owner reads them.
const (
	Confirmed  = "confirmed"
	Likely     = "likely"
	Unknown    = "unknown"
	Ineligible = "ineligible"
)

// Quality labels: what the posting is worth to the owner if the owner could take it, best first.
const (
	Exceptional = "exceptional"
	Strong      = "strong"
	Good        = "good"
	Exploratory = "exploratory"
	Low         = "low"
)

// EligibilityClasses and QualityClasses are the label values, in display order.
var (
	EligibilityClasses = []string{Confirmed, Likely, Unknown, Ineligible}
	QualityClasses     = []string{Exceptional, Strong, Good, Exploratory, Low}
)

// Labels are the proposed ground truth for one posting.
//
//   - IsInternship: an internship, co-op, apprenticeship or student fellowship the owner could apply to as a
//     student. A full-time new-graduate role is not one.
//   - Eligible: whether the owner (2028 B.Tech CSE, VIT Vellore, India) may apply, judged only by the posting's
//     location, work-authorisation, graduation-year and degree wording. "unknown" when the posting does not settle it.
//   - Quality: the value of the posting to the owner if the owner could take it (role, employer, pay, learning).
//   - DuplicateOf: the id of the first record that is the same posting (same link, or the same role and place
//     seen again); "" when the record is not a duplicate. The same role in another city is a different posting.
type Labels struct {
	IsInternship bool     `json:"is_internship"`
	Eligible     string   `json:"eligible"`
	Quality      string   `json:"quality"`
	DuplicateOf  string   `json:"duplicate_of"`
	Reasons      []string `json:"reasons"`
	// Reviewed is false until the owner has confirmed or edited these labels.
	Reviewed bool `json:"reviewed"`
}

// Record is one real posting, as a source returned it, with proposed labels.
type Record struct {
	ID string `json:"id"`
	// SourceURL is the board API or page the posting was read from; Raw.URL is the posting's own link.
	SourceURL   string `json:"source_url"`
	FetchedDate string `json:"fetched_date"`
	// DescriptionTruncated is true when Raw.Description was cut at 6000 characters to keep the dataset small.
	DescriptionTruncated bool `json:"description_truncated"`
	// CompanyKind is the seeded kind of the employer ("startup", "enterprise", ...) for employer-board records.
	CompanyKind string      `json:"company_kind"`
	Tags        []string    `json:"tags"`
	Raw         jobs.RawJob `json:"raw"`
	Labels      Labels      `json:"labels"`
}

// Parse reads and validates a dataset.
func Parse(b []byte) ([]Record, error) {
	var recs []Record
	if err := json.Unmarshal(b, &recs); err != nil {
		return nil, fmt.Errorf("eval: dataset: %w", err)
	}
	return recs, Validate(recs)
}

// Embedded is the dataset shipped with the package.
func Embedded() ([]Record, error) { return Parse(embeddedDataset) }

// Marshal writes a dataset in the form it is stored in (indented, HTML characters not escaped).
func Marshal(recs []Record) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", " ")
	if err := enc.Encode(recs); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func contains(set []string, v string) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}

// Validate checks that every record is complete and every label is one the benchmark knows.
func Validate(recs []Record) error {
	ids := map[string]bool{}
	for _, r := range recs {
		if r.ID == "" || ids[r.ID] {
			return fmt.Errorf("eval: record id %q is empty or repeated", r.ID)
		}
		ids[r.ID] = true
	}
	for _, r := range recs {
		switch {
		case strings.TrimSpace(r.Raw.Title) == "" || strings.TrimSpace(r.Raw.URL) == "":
			return fmt.Errorf("eval: %s has no title or link: a record is a real posting", r.ID)
		case r.SourceURL == "" || r.FetchedDate == "":
			return fmt.Errorf("eval: %s has no source URL or fetch date", r.ID)
		case !contains(EligibilityClasses, r.Labels.Eligible):
			return fmt.Errorf("eval: %s has the eligibility label %q", r.ID, r.Labels.Eligible)
		case !contains(QualityClasses, r.Labels.Quality):
			return fmt.Errorf("eval: %s has the quality label %q", r.ID, r.Labels.Quality)
		case r.Labels.DuplicateOf == r.ID:
			return fmt.Errorf("eval: %s is a duplicate of itself", r.ID)
		case r.Labels.DuplicateOf != "" && !ids[r.Labels.DuplicateOf]:
			return fmt.Errorf("eval: %s is a duplicate of the unknown record %s", r.ID, r.Labels.DuplicateOf)
		case len(r.Labels.Reasons) == 0:
			return fmt.Errorf("eval: %s has no reason for its labels", r.ID)
		}
	}
	return nil
}

// relevant is the benchmark's "worth showing the owner" test: an internship the owner is not barred from, whose
// quality is good or better.
func (r Record) relevant() bool {
	l := r.Labels
	return l.IsInternship && l.Eligible != Ineligible && qualityRank(l.Quality) <= qualityRank(Good)
}

// qualityRank is 0 for exceptional down to 4 for low; an unknown label ranks below low.
func qualityRank(q string) int {
	for i, c := range QualityClasses {
		if c == q {
			return i
		}
	}
	return len(QualityClasses)
}

// goldClusters groups records by the posting they repeat: every record maps to the id of its cluster's first record.
func goldClusters(recs []Record) map[string]string {
	parent := map[string]string{}
	for _, r := range recs {
		parent[r.ID] = r.Labels.DuplicateOf
	}
	root := func(id string) string {
		seen := 0
		for parent[id] != "" && seen < len(recs) {
			id = parent[id]
			seen++
		}
		return id
	}
	out := map[string]string{}
	for _, r := range recs {
		out[r.ID] = root(r.ID)
	}
	return out
}

// sourceNames lists the sources present in a dataset, sorted.
func sourceNames(recs []Record) []string {
	set := map[string]bool{}
	for _, r := range recs {
		set[r.Raw.Source] = true
	}
	var out []string
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

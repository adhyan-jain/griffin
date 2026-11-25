package eval

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Summary describes a dataset.
type Summary struct {
	Records      int            `json:"records"`
	Reviewed     int            `json:"reviewed"`
	BySource     map[string]int `json:"by_source"`
	ByTag        map[string]int `json:"by_tag"`
	Internships  int            `json:"internships"`
	Relevant     int            `json:"relevant"`
	Eligibility  map[string]int `json:"eligibility_labels"`
	Quality      map[string]int `json:"quality_labels"`
	Duplicates   int            `json:"labelled_duplicates"`
	FetchedDates []string       `json:"fetched_dates"`
	Truncated    int            `json:"truncated_descriptions"`
}

// Section is the three pipelines measured over one set of records.
type Section struct {
	Records  int       `json:"records"`
	Pipeline []Metrics `json:"pipelines"` // before as deployed, before filter-only, after
}

// Report is the benchmark's output. Proposed is measured on every record's proposed (unreviewed) label; Reviewed only
// on the records whose labels the owner has confirmed, and is nil while none has.
type Report struct {
	Dataset  Summary  `json:"dataset"`
	Proposed Section  `json:"proposed_label_metrics"`
	Reviewed *Section `json:"reviewed_label_metrics,omitempty"`
}

func section(recs []Record) Section {
	return Section{Records: len(recs), Pipeline: []Metrics{
		Evaluate(recs, RunBefore(recs, false)),
		Evaluate(recs, RunBefore(recs, true)),
		Evaluate(recs, RunAfter(recs)),
	}}
}

// Summarize counts what a dataset holds.
func Summarize(recs []Record) Summary {
	s := Summary{Records: len(recs), BySource: map[string]int{}, ByTag: map[string]int{}, Eligibility: map[string]int{}, Quality: map[string]int{}}
	dates := map[string]bool{}
	for _, r := range recs {
		s.BySource[r.Raw.Source]++
		for _, t := range r.Tags {
			s.ByTag[t]++
		}
		s.Eligibility[r.Labels.Eligible]++
		s.Quality[r.Labels.Quality]++
		if r.Labels.IsInternship {
			s.Internships++
		}
		if r.relevant() {
			s.Relevant++
		}
		if r.Labels.Reviewed {
			s.Reviewed++
		}
		if r.Labels.DuplicateOf != "" {
			s.Duplicates++
		}
		if r.DescriptionTruncated {
			s.Truncated++
		}
		dates[r.FetchedDate] = true
	}
	for d := range dates {
		s.FetchedDates = append(s.FetchedDates, d)
	}
	sort.Strings(s.FetchedDates)
	return s
}

// Build runs the benchmark over a dataset.
func Build(recs []Record) Report {
	r := Report{Dataset: Summarize(recs), Proposed: section(recs)}
	var reviewed []Record
	for _, rec := range recs {
		if rec.Labels.Reviewed {
			reviewed = append(reviewed, rec)
		}
	}
	if len(reviewed) > 0 {
		sec := section(reviewed)
		r.Reviewed = &sec
	}
	return r
}

// MarshalReport writes a report as indented JSON.
func MarshalReport(r Report) ([]byte, error) { return json.MarshalIndent(r, "", " ") }

func pct(v float64) string { return fmt.Sprintf("%.1f%%", 100*v) }

func sortedKeys(m map[string]int) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func countList(m map[string]int, order []string) string {
	var parts []string
	for _, k := range order {
		parts = append(parts, fmt.Sprintf("%s %d", k, m[k]))
	}
	return strings.Join(parts, ", ")
}

// Markdown renders the report.
func (r Report) Markdown() string {
	var b strings.Builder
	d := r.Dataset
	b.WriteString("# Opportunity pipeline benchmark\n\n")
	b.WriteString("Every number below is computed on **proposed, unreviewed labels**: a first reading of each posting by the assistant that built the benchmark, ")
	b.WriteString("never confirmed by the owner. They show where the pipeline and a careful reader disagree. They are not measured accuracy until the owner reviews the labels (`go run ./cmd/eval_opportunities review`).\n\n")

	fmt.Fprintf(&b, "## Dataset\n\n%d real postings, fetched %s, %d with reviewed labels (%d pending review).\n\n", d.Records, strings.Join(d.FetchedDates, ", "), d.Reviewed, d.Records-d.Reviewed)
	fmt.Fprintf(&b, "- Sources: %s\n", countList(d.BySource, sortedKeys(d.BySource)))
	fmt.Fprintf(&b, "- Internships (proposed): %d; not internships: %d\n", d.Internships, d.Records-d.Internships)
	fmt.Fprintf(&b, "- Eligibility labels: %s\n", countList(d.Eligibility, EligibilityClasses))
	fmt.Fprintf(&b, "- Quality labels: %s\n", countList(d.Quality, QualityClasses))
	fmt.Fprintf(&b, "- Relevant (internship, not ineligible, quality good or better): %d\n", d.Relevant)
	fmt.Fprintf(&b, "- Labelled duplicates of an earlier record: %d\n", d.Duplicates)
	fmt.Fprintf(&b, "- Descriptions cut at 6000 characters: %d\n\n", d.Truncated)

	b.WriteString("## Proposed-label metrics (unreviewed)\n\n")
	b.WriteString(r.Proposed.table())
	for _, m := range r.Proposed.Pipeline {
		b.WriteString(m.detail())
	}

	b.WriteString("\n## Reviewed-label metrics (pending)\n\n")
	if r.Reviewed == nil {
		b.WriteString("No label has been reviewed yet, so there are no reviewed-label metrics. They appear here once the owner has confirmed labels.\n")
	} else {
		fmt.Fprintf(&b, "Computed on the %d records whose labels the owner reviewed.\n\n", r.Reviewed.Records)
		b.WriteString(r.Reviewed.table())
	}
	return b.String()
}

func (s Section) table() string {
	var b strings.Builder
	a, f, n := s.Pipeline[0], s.Pipeline[1], s.Pipeline[2]
	b.WriteString("| Metric | Before: as deployed (aggregators only) | Before: old filter on every record | After: current pipeline |\n|---|---|---|---|\n")
	row := func(name string, get func(Metrics) string) {
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", name, get(a), get(f), get(n))
	}
	row("Records retrieved (of "+itoa(a.Records)+")", func(m Metrics) string { return itoa(m.Retained) })
	row("Recall@100 (relevant postings in the top 100)", func(m Metrics) string { return pct(m.Ranking.RecallAt100) })
	row("Recall@50", func(m Metrics) string { return pct(m.Ranking.RecallAt50) })
	row("Precision@20", func(m Metrics) string { return pct(m.Ranking.PrecisionAt20) })
	row("Precision@10", func(m Metrics) string { return pct(m.Ranking.PrecisionAt10) })
	row("Internship retrieval precision", func(m Metrics) string { return pct(m.Retrieval.Precision) })
	row("Internship retrieval recall", func(m Metrics) string { return pct(m.Retrieval.Recall) })
	row("Relevant postings retrieved", func(m Metrics) string {
		return fmt.Sprintf("%s (%d of %d)", pct(m.Retrieval.RelevantRecall), m.Ranking.Relevant-len(m.FalseNegatives), m.Ranking.Relevant)
	})
	row("Eligibility accuracy (4 classes)", func(m Metrics) string {
		if m.Eligibility == nil {
			return "not assessed"
		}
		return fmt.Sprintf("%s (%d of %d)", pct(m.Eligibility.Accuracy), m.Eligibility.Correct, m.Eligibility.N)
	})
	row("Labelled-ineligible postings shown as open", func(m Metrics) string {
		return fmt.Sprintf("%d of %d retained", m.Retrieval.IneligibleLeaked, m.Retrieval.IneligibleRetained)
	})
	row("Value class matches quality label", func(m Metrics) string {
		if m.Quality == nil {
			return "not assessed (stipend tier only)"
		}
		return fmt.Sprintf("%s exact, %s within one class", pct(ratio(m.Quality.Exact, m.Quality.N)), pct(ratio(m.Quality.WithinOne, m.Quality.N)))
	})
	row("Duplicate rate among retained", func(m Metrics) string {
		return fmt.Sprintf("%s found (labels say %s)", pct(m.Duplicates.Rate), pct(m.Duplicates.GoldRate))
	})
	row("Duplicate pairs: precision / recall", func(m Metrics) string {
		return fmt.Sprintf("%s / %s", pct(m.Duplicates.Precision), pct(m.Duplicates.Recall))
	})
	b.WriteString("\n### Source coverage: relevant postings retrieved per source\n\n| Source | Relevant | Before: as deployed | Before: filter only | After |\n|---|---|---|---|---|\n")
	for i, c := range n.Coverage {
		fmt.Fprintf(&b, "| %s | %d | %d (%s) | %d (%s) | %d (%s) |\n", c.Source, c.Relevant,
			a.Coverage[i].Retrieved, pct(a.Coverage[i].Share), f.Coverage[i].Retrieved, pct(f.Coverage[i].Share), c.Retrieved, pct(c.Share))
	}
	b.WriteString("\n")
	return b.String()
}

const listCap = 25

func writeItems(b *strings.Builder, title string, items []Item) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "\n**%s (%d)**\n\n", title, len(items))
	for i, it := range items {
		if i == listCap {
			fmt.Fprintf(b, "- ... and %d more (all are in the JSON output)\n", len(items)-listCap)
			break
		}
		fmt.Fprintf(b, "- `%s` %s: %s [%s]. Label: %s. Pipeline: %s. Why: %s\n", it.ID, it.Employer, it.Title, it.Location, it.Label, it.Got, it.Why)
	}
}

func (m Metrics) detail() string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n### Detail: %s\n", m.Pipeline)
	fmt.Fprintf(&b, "\nRetrieved %d of %d. Relevant in the top 20: %d of %d relevant postings.\n", m.Retained, m.Records, m.Ranking.RelevantInTop20, m.Ranking.Relevant)
	if m.Ranking.PrecisionAt20Eligible != nil {
		fmt.Fprintf(&b, "Precision@20 with the postings the pipeline itself marks ineligible removed: %s.\n", pct(*m.Ranking.PrecisionAt20Eligible))
	}
	if e := m.Eligibility; e != nil {
		fmt.Fprintf(&b, "\nEligibility confusion matrix (rows: proposed label, columns: pipeline), over the %d internships:\n\n| label \\ pipeline | %s |\n|---|---|---|---|---|\n", e.N, strings.Join(EligibilityClasses, " | "))
		for _, l := range EligibilityClasses {
			fmt.Fprintf(&b, "| %s | %d | %d | %d | %d |\n", l, e.Matrix[l][Confirmed], e.Matrix[l][Likely], e.Matrix[l][Unknown], e.Matrix[l][Ineligible])
		}
		fmt.Fprintf(&b, "\nThe pipeline's \"ineligible\" verdict: precision %s, recall %s.\n", pct(e.IneligiblePrecision), pct(e.IneligibleRecall))
	}
	if q := m.Quality; q != nil {
		fmt.Fprintf(&b, "\nValue class vs quality label (rows: label, columns: pipeline), over the %d internships not labelled ineligible:\n\n| label \\ pipeline | %s |\n|---|---|---|---|---|---|\n", q.N, strings.Join(QualityClasses, " | "))
		for _, l := range QualityClasses {
			fmt.Fprintf(&b, "| %s | %d | %d | %d | %d | %d |\n", l, q.Matrix[l][Exceptional], q.Matrix[l][Strong], q.Matrix[l][Good], q.Matrix[l][Exploratory], q.Matrix[l][Low])
		}
	}
	writeItems(&b, "False negatives: relevant postings the retrieval stage dropped", m.FalseNegatives)
	writeItems(&b, "False positives: retained postings that are not internships", m.FalsePositives)
	writeItems(&b, "Top-20 postings that are not relevant", m.TopFalsePositives)
	if e := m.Eligibility; e != nil {
		writeItems(&b, "Eligibility leaks: labelled ineligible, not blocked", e.Leaks)
		writeItems(&b, "Eligibility over-blocks: blocked, but labelled open or unknown", e.OverBlocks)
	}
	writeItems(&b, "False merges: one opportunity by identity, two by the labels", m.Duplicates.FalseMerges)
	writeItems(&b, "Missed duplicates", m.Duplicates.Missed)
	return b.String()
}

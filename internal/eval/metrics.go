package eval

import (
	"sort"
	"strconv"
	"strings"
)

// Item is one record in a list of errors, with the words of the rule that caused it.
type Item struct {
	ID       string `json:"id"`
	Source   string `json:"source"`
	Employer string `json:"employer"`
	Title    string `json:"title"`
	Location string `json:"location"`
	// Label is what the proposed label says; Got is what the pipeline did; Why is the pipeline's reason.
	Label string `json:"label"`
	Got   string `json:"got"`
	Why   string `json:"why"`
}

func item(r Record, label, got, why string) Item {
	return Item{ID: r.ID, Source: r.Raw.Source, Employer: r.Raw.EmployerName, Title: r.Raw.Title, Location: strings.Join(r.Raw.Locations, "; "), Label: label, Got: got, Why: why}
}

// RankMetrics measure the ranking of the retained records against the quality labels. A record is relevant when it is
// an internship, not labelled ineligible, with quality good or better. Recall is over every relevant record in the
// dataset, so a relevant record the retrieval stage dropped counts against recall at every cutoff.
type RankMetrics struct {
	Relevant        int     `json:"relevant"`
	RelevantInTop20 int     `json:"relevant_in_top20"`
	PrecisionAt10   float64 `json:"precision_at_10"`
	PrecisionAt20   float64 `json:"precision_at_20"`
	RecallAt20      float64 `json:"recall_at_20"`
	RecallAt50      float64 `json:"recall_at_50"`
	RecallAt100     float64 `json:"recall_at_100"`
	// PrecisionAt20Eligible ranks the same records after removing those the pipeline itself marks ineligible.
	PrecisionAt20Eligible *float64 `json:"precision_at_20_pipeline_eligible,omitempty"`
}

// RetrievalMetrics measure stage 1 against the is_internship label.
type RetrievalMetrics struct {
	TP, FP, FN, TN int
	Precision      float64 `json:"precision"`
	Recall         float64 `json:"recall"`
	// RelevantRecall is the share of relevant records the stage kept (the owner-fit recall).
	RelevantRecall float64 `json:"relevant_recall"`
	// IneligibleRetained counts retained internships the labels say the owner cannot take.
	IneligibleRetained int `json:"ineligible_retained"`
	// IneligibleLeaked counts those the pipeline did not mark ineligible (the baseline marks none).
	IneligibleLeaked int `json:"ineligible_leaked"`
}

// SourceCoverage is the share of a source's relevant records that the pipeline retrieved.
type SourceCoverage struct {
	Source    string  `json:"source"`
	Records   int     `json:"records"`
	Relevant  int     `json:"relevant"`
	Retrieved int     `json:"relevant_retrieved"`
	Share     float64 `json:"share"`
}

// EligibilityMetrics measure the assessor's eligibility verdict against the label, over every internship record.
type EligibilityMetrics struct {
	N        int     `json:"n"`
	Correct  int     `json:"correct"`
	Accuracy float64 `json:"accuracy"`
	// Matrix[label][pipeline] counts records.
	Matrix map[string]map[string]int `json:"matrix"`
	// Ineligible is precision and recall of the pipeline's "ineligible" verdict.
	IneligiblePrecision float64 `json:"ineligible_precision"`
	IneligibleRecall    float64 `json:"ineligible_recall"`
	// Leaks are records labelled ineligible that the pipeline did not block; OverBlocks the reverse.
	Leaks      []Item `json:"leaks"`
	OverBlocks []Item `json:"over_blocks"`
}

// QualityMetrics compare the value class with the quality label, over internships not labelled ineligible.
type QualityMetrics struct {
	N         int                       `json:"n"`
	Exact     int                       `json:"exact"`
	WithinOne int                       `json:"within_one"`
	Matrix    map[string]map[string]int `json:"matrix"`
}

// DuplicateMetrics measure dedupe over the retained records.
type DuplicateMetrics struct {
	Retained int `json:"retained"`
	// Clusters is the number of distinct opportunities the pipeline's identity finds; GoldClusters the labelled number.
	Clusters     int `json:"clusters"`
	GoldClusters int `json:"gold_clusters"`
	// Rate is the share of retained records that repeat an earlier one, by the pipeline and by the labels.
	Rate      float64 `json:"rate"`
	GoldRate  float64 `json:"gold_rate"`
	GoldPairs int     `json:"gold_pairs"`
	Found     int     `json:"found_pairs"`
	TruePairs int     `json:"true_pairs"`
	Precision float64 `json:"pair_precision"`
	Recall    float64 `json:"pair_recall"`
	// FalseMerges are pairs the pipeline calls one opportunity that the labels call two (for example the same role in two cities).
	FalseMerges []Item `json:"false_merges"`
	Missed      []Item `json:"missed_duplicates"`
}

// Metrics is everything measured for one pipeline over one set of records.
type Metrics struct {
	Pipeline string `json:"pipeline"`
	Records  int    `json:"records"`
	Retained int    `json:"retained"`

	Ranking     RankMetrics         `json:"ranking"`
	Retrieval   RetrievalMetrics    `json:"retrieval"`
	Coverage    []SourceCoverage    `json:"coverage"`
	Eligibility *EligibilityMetrics `json:"eligibility,omitempty"`
	Quality     *QualityMetrics     `json:"quality,omitempty"`
	Duplicates  DuplicateMetrics    `json:"duplicates"`

	// Errors, with the reason each happened.
	FalseNegatives    []Item `json:"false_negatives"`       // relevant records the retrieval stage dropped
	FalsePositives    []Item `json:"false_positives"`       // retained records that are not internships
	TopFalsePositives []Item `json:"top20_false_positives"` // top-20 records that are not relevant
}

func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

// Evaluate measures a pipeline's outcomes over the records it was run on.
func Evaluate(recs []Record, p *Pipeline) Metrics {
	byID := map[string]Record{}
	for _, r := range recs {
		byID[r.ID] = r
	}
	m := Metrics{Pipeline: p.Name, Records: len(recs), Retained: len(p.Ranking)}

	// Ranking.
	relevantSet := map[string]bool{}
	for _, r := range recs {
		if r.relevant() {
			relevantSet[r.ID] = true
		}
	}
	hits := func(k int) int {
		n := 0
		for i, id := range p.Ranking {
			if i >= k {
				break
			}
			if relevantSet[id] {
				n++
			}
		}
		return n
	}
	prec := func(k int) float64 { return ratio(hits(k), min(k, len(p.Ranking))) }
	m.Ranking = RankMetrics{
		Relevant: len(relevantSet), RelevantInTop20: hits(20),
		PrecisionAt10: prec(10), PrecisionAt20: prec(20),
		RecallAt20: ratio(hits(20), len(relevantSet)), RecallAt50: ratio(hits(50), len(relevantSet)), RecallAt100: ratio(hits(100), len(relevantSet)),
	}
	if hasAssessment(p) {
		var kept []string
		for _, id := range p.Ranking {
			if p.Outcomes[id].Eligibility != Ineligible {
				kept = append(kept, id)
			}
		}
		n := 0
		for i, id := range kept {
			if i >= 20 {
				break
			}
			if relevantSet[id] {
				n++
			}
		}
		v := ratio(n, min(20, len(kept)))
		m.Ranking.PrecisionAt20Eligible = &v
	}
	for i, id := range p.Ranking {
		if i >= 20 {
			break
		}
		if !relevantSet[id] {
			r := byID[id]
			m.TopFalsePositives = append(m.TopFalsePositives, item(r, "is_internship="+boolStr(r.Labels.IsInternship)+" eligible="+r.Labels.Eligible+" quality="+r.Labels.Quality, rankStr(i+1, p.Outcomes[id]), strings.Join(r.Labels.Reasons, " ")))
		}
	}

	// Retrieval and coverage.
	cov := map[string]*SourceCoverage{}
	for _, s := range sourceNames(recs) {
		cov[s] = &SourceCoverage{Source: s}
	}
	for _, r := range recs {
		o := p.Outcomes[r.ID]
		c := cov[r.Raw.Source]
		c.Records++
		if r.relevant() {
			c.Relevant++
			if o.Retained {
				c.Retrieved++
			}
		}
		switch {
		case r.Labels.IsInternship && o.Retained:
			m.Retrieval.TP++
		case !r.Labels.IsInternship && o.Retained:
			m.Retrieval.FP++
			m.FalsePositives = append(m.FalsePositives, item(r, "not an internship: "+strings.Join(r.Labels.Reasons, " "), "retained", o.Why))
		case r.Labels.IsInternship && !o.Retained:
			m.Retrieval.FN++
		default:
			m.Retrieval.TN++
		}
		if r.relevant() && !o.Retained {
			m.FalseNegatives = append(m.FalseNegatives, item(r, "relevant (quality "+r.Labels.Quality+", eligible "+r.Labels.Eligible+"): "+strings.Join(r.Labels.Reasons, " "), "dropped", o.Why))
		}
		if r.Labels.IsInternship && r.Labels.Eligible == Ineligible && o.Retained {
			m.Retrieval.IneligibleRetained++
			if o.Eligibility != Ineligible {
				m.Retrieval.IneligibleLeaked++
			}
		}
	}
	m.Retrieval.Precision = ratio(m.Retrieval.TP, m.Retrieval.TP+m.Retrieval.FP)
	m.Retrieval.Recall = ratio(m.Retrieval.TP, m.Retrieval.TP+m.Retrieval.FN)
	m.Retrieval.RelevantRecall = ratio(len(relevantSet)-len(m.FalseNegatives), len(relevantSet))
	for _, s := range sourceNames(recs) {
		c := cov[s]
		c.Share = ratio(c.Retrieved, c.Relevant)
		m.Coverage = append(m.Coverage, *c)
	}

	if hasAssessment(p) {
		m.Eligibility = evalEligibility(recs, p)
		m.Quality = evalQuality(recs, p)
	}
	m.Duplicates = evalDuplicates(recs, p)
	return m
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func rankStr(rank int, o *Outcome) string {
	s := "rank " + itoa(rank)
	if o.Assessed {
		s += ", score " + itoa(o.ValueScore) + " (" + o.ValueClass + "), eligibility " + o.Eligibility
	}
	return s
}

func itoa(n int) string { return strconv.Itoa(n) }

func hasAssessment(p *Pipeline) bool {
	for _, o := range p.Outcomes {
		return o.Assessed
	}
	return false
}

func newMatrix(classes []string) map[string]map[string]int {
	m := map[string]map[string]int{}
	for _, a := range classes {
		m[a] = map[string]int{}
		for _, b := range classes {
			m[a][b] = 0
		}
	}
	return m
}

// evalEligibility compares the assessor's verdict with the label over every internship record, retained or not: the
// assessor is measured on its own, not through what retrieval happened to keep.
func evalEligibility(recs []Record, p *Pipeline) *EligibilityMetrics {
	e := &EligibilityMetrics{Matrix: newMatrix(EligibilityClasses)}
	var predIn, labelIn, both int
	for _, r := range recs {
		if !r.Labels.IsInternship {
			continue
		}
		o := p.Outcomes[r.ID]
		e.N++
		e.Matrix[r.Labels.Eligible][o.Eligibility]++
		if r.Labels.Eligible == o.Eligibility {
			e.Correct++
		}
		if o.Eligibility == Ineligible {
			predIn++
		}
		if r.Labels.Eligible == Ineligible {
			labelIn++
		}
		if o.Eligibility == Ineligible && r.Labels.Eligible == Ineligible {
			both++
		}
		switch {
		case r.Labels.Eligible == Ineligible && o.Eligibility != Ineligible:
			e.Leaks = append(e.Leaks, item(r, "ineligible: "+strings.Join(r.Labels.Reasons, " "), o.Eligibility, o.EligibilityWhy))
		case r.Labels.Eligible != Ineligible && o.Eligibility == Ineligible:
			e.OverBlocks = append(e.OverBlocks, item(r, r.Labels.Eligible+": "+strings.Join(r.Labels.Reasons, " "), o.Eligibility, o.EligibilityWhy))
		}
	}
	e.Accuracy = ratio(e.Correct, e.N)
	e.IneligiblePrecision = ratio(both, predIn)
	e.IneligibleRecall = ratio(both, labelIn)
	return e
}

func classIndex(classes []string, c string) int {
	for i, x := range classes {
		if x == c {
			return i
		}
	}
	return len(classes)
}

// evalQuality compares the value class with the quality label over internships the labels do not bar the owner from.
func evalQuality(recs []Record, p *Pipeline) *QualityMetrics {
	q := &QualityMetrics{Matrix: newMatrix(QualityClasses)}
	for _, r := range recs {
		if !r.Labels.IsInternship || r.Labels.Eligible == Ineligible {
			continue
		}
		o := p.Outcomes[r.ID]
		q.N++
		q.Matrix[r.Labels.Quality][o.ValueClass]++
		d := classIndex(QualityClasses, r.Labels.Quality) - classIndex(QualityClasses, o.ValueClass)
		if d == 0 {
			q.Exact++
		}
		if d >= -1 && d <= 1 {
			q.WithinOne++
		}
	}
	return q
}

// evalDuplicates finds the opportunities among the retained records by the pipeline's identity and compares the pairs
// with the labelled duplicates.
func evalDuplicates(recs []Record, p *Pipeline) DuplicateMetrics {
	var kept []Record
	for _, r := range recs {
		if p.Outcomes[r.ID].Retained {
			kept = append(kept, r)
		}
	}
	d := DuplicateMetrics{Retained: len(kept)}
	gold := goldClusters(recs)
	// Union-find over retained records by the pipeline's identity.
	parent := make([]int, len(kept))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	goldSeen := map[string]bool{}
	for _, r := range kept {
		goldSeen[gold[r.ID]] = true
	}
	d.GoldClusters = len(goldSeen)
	for i := 0; i < len(kept); i++ {
		for j := i + 1; j < len(kept); j++ {
			a, b := kept[i], kept[j]
			same := p.Same(a, b)
			isGold := gold[a.ID] == gold[b.ID]
			if isGold {
				d.GoldPairs++
			}
			if same {
				d.Found++
				if isGold {
					d.TruePairs++
				} else {
					d.FalseMerges = append(d.FalseMerges, pairItem(a, b, "labelled as different postings", "the pipeline's identity calls them one opportunity"))
				}
				parent[find(i)] = find(j)
			} else if isGold {
				d.Missed = append(d.Missed, pairItem(a, b, "labelled as the same posting", "the pipeline's identity calls them two opportunities"))
			}
		}
	}
	roots := map[int]bool{}
	for i := range kept {
		roots[find(i)] = true
	}
	d.Clusters = len(roots)
	d.Rate = 1 - ratio(d.Clusters, d.Retained)
	d.GoldRate = 1 - ratio(d.GoldClusters, d.Retained)
	if d.Retained == 0 {
		d.Rate, d.GoldRate = 0, 0
	}
	d.Precision = ratio(d.TruePairs, d.Found)
	d.Recall = ratio(d.TruePairs, d.GoldPairs)
	sort.SliceStable(d.FalseMerges, func(i, j int) bool { return d.FalseMerges[i].ID < d.FalseMerges[j].ID })
	return d
}

func pairItem(a, b Record, label, got string) Item {
	it := item(a, label, got, "")
	it.Why = a.ID + " (" + strings.Join(a.Raw.Locations, "; ") + ") and " + b.ID + " \"" + b.Raw.Title + "\" (" + strings.Join(b.Raw.Locations, "; ") + ")"
	return it
}

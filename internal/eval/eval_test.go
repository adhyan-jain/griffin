package eval

import (
	"math"
	"regexp"
	"strings"
	"testing"

	"github.com/griffin/core/internal/jobs"
)

func mustDataset(t *testing.T) []Record {
	t.Helper()
	recs, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	return recs
}

func byID(recs []Record, id string) Record {
	for _, r := range recs {
		if r.ID == id {
			return r
		}
	}
	panic("no record " + id)
}

// ---- the dataset ----------------------------------------------------------------------------------------------------

func TestDatasetIsRealPostingsWithUnreviewedLabels(t *testing.T) {
	recs := mustDataset(t)
	if len(recs) < 150 || len(recs) > 250 {
		t.Fatalf("the benchmark is meant to hold about 150 to 250 postings, has %d", len(recs))
	}
	date := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	sources := map[string]int{}
	for _, r := range recs {
		if r.Labels.Reviewed {
			t.Errorf("%s: no label may be marked reviewed until the owner reviews it", r.ID)
		}
		if !strings.HasPrefix(r.Raw.URL, "https://") || !strings.HasPrefix(r.SourceURL, "https://") || !date.MatchString(r.FetchedDate) {
			t.Errorf("%s: a record carries the posting's link, the URL it was read from and the day: %q %q %q", r.ID, r.Raw.URL, r.SourceURL, r.FetchedDate)
		}
		if r.Raw.Source == "" || r.Raw.SourceKind == "" || r.Raw.EmployerName == "" {
			t.Errorf("%s: source, kind and employer are required: %+v", r.ID, r.Raw)
		}
		if r.Raw.SourceKind != jobs.Aggregator && r.Raw.Description == "" {
			t.Errorf("%s: an employer posting without a description is not a real capture", r.ID)
		}
		sources[r.Raw.Source]++
	}
	// The mix the benchmark is built to cover.
	for _, s := range []string{"greenhouse", "lever", "ashby", "internshala", "unstop", "interndock"} {
		if sources[s] == 0 {
			t.Errorf("no records from %s", s)
		}
	}
	tags := Summarize(recs).ByTag
	for _, tag := range []string{"india-early-career", "tech-intern-abroad", "non-tech-intern", "new-grad-or-early-career", "residency-or-fellowship", "trap-internal-international", "duplicate-role-multiple-locations", "internshala-frontend-design", "internshala-unpaid", "same-listing-two-pages", "cross-source-same-employer"} {
		if tags[tag] == 0 {
			t.Errorf("the tricky case %q is not in the dataset", tag)
		}
	}
}

// A capture error that once read InterDock's logo placeholder ("H") as the title must not come back into the dataset.
func TestDatasetHasNoPlaceholderTitles(t *testing.T) {
	for _, r := range mustDataset(t) {
		if len([]rune(strings.TrimSpace(r.Raw.Title))) <= 3 && r.Raw.Source == "interndock" {
			t.Errorf("%s: the title %q is a logo placeholder", r.ID, r.Raw.Title)
		}
	}
}

func TestValidateRejectsBadLabels(t *testing.T) {
	good := mustDataset(t)[:2]
	bad := append([]Record(nil), good...)
	bad[0].Labels.Eligible = "maybe"
	if Validate(bad) == nil {
		t.Error("an unknown eligibility label must be rejected")
	}
	bad = append([]Record(nil), good...)
	bad[1].Labels.DuplicateOf = "no-such-record"
	if Validate(bad) == nil {
		t.Error("a duplicate_of that names no record must be rejected")
	}
	bad = append([]Record(nil), good...)
	bad[1].Labels.Reasons = nil
	if Validate(bad) == nil {
		t.Error("a label without a reason must be rejected")
	}
}

func TestDatasetRoundTripsThroughTheReviewToolsWriter(t *testing.T) {
	recs := mustDataset(t)
	b, err := Marshal(recs)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Parse(b)
	if err != nil || len(again) != len(recs) {
		t.Fatalf("round trip: %v, %d vs %d", err, len(again), len(recs))
	}
	if again[5].Raw.Description != recs[5].Raw.Description || again[5].Labels.Reasons[0] != recs[5].Labels.Reasons[0] {
		t.Error("the writer changed a record")
	}
}

// ---- the frozen baseline --------------------------------------------------------------------------------------------

func TestBaselineIsTheOldFilter(t *testing.T) {
	job := func(src, title, loc, pay string) jobs.RawJob {
		return jobs.RawJob{Source: src, Title: title, Locations: []string{loc}, Compensation: pay}
	}
	cases := []struct {
		name string
		j    jobs.RawJob
		want bool
	}{
		{"a paid backend intern in India", job("internshala", "Backend Development Intern", "Remote (India)", "₹ 15,000 /month"), true},
		{"the Rs 10k floor drops a lower stipend", job("internshala", "Backend Development Intern", "Remote (India)", "₹ 5,000 /month"), false},
		{"an unpaid role is dropped", job("unstop", "Software Internship", "Remote (India)", "Unpaid"), false},
		{"a role with no stated pay is dropped", job("greenhouse", "Software Engineer Intern", "Bengaluru", ""), false},
		{"InternDock is exempt from the floor", job("interndock", "Software Engineer Intern", "Bengaluru, India", ""), true},
		{"a frontend title is excluded", job("internshala", "Front End Development Intern", "Remote (India)", "₹ 30,000 /month"), false},
		{"a language-named role is not an owner-fit title in the old regex", job("internshala", "Python Development Intern", "Remote (India)", "₹ 30,000 /month"), false},
		{"abroad is dropped", job("internshala", "Software Engineer Intern", "New York, NY", "₹ 30,000 /month"), false},
		{"a full-time role is not an internship", job("internshala", "Software Engineer", "Bengaluru, India", "₹ 30,000 /month"), false},
	}
	for _, c := range cases {
		if got, why := baselineFits(c.j); got != c.want {
			t.Errorf("%s: got %v (%s)", c.name, got, why)
		}
	}
	for pay, want := range map[string]string{"₹ 40,000 /month": "A", "₹ 4,000 - 40,000 /month": "B", "₹ 20,000 /month": "B", "₹ 5,000 /month": "C", "Unpaid": "C", "": "C"} {
		if got := baselineTier(pay); got != want {
			t.Errorf("tier of %q: got %s, want %s", pay, got, want)
		}
	}
}

func TestBaselineDoesNotSeeEmployerBoardsAsDeployed(t *testing.T) {
	recs := mustDataset(t)
	p := RunBefore(recs, false)
	for _, r := range recs {
		if r.Raw.SourceKind != jobs.Aggregator && p.Outcomes[r.ID].Retained {
			t.Fatalf("%s: the old pipeline read no employer boards", r.ID)
		}
	}
	if len(p.Ranking) == 0 {
		t.Fatal("the old pipeline retrieves aggregator listings")
	}
}

// ---- the metrics ----------------------------------------------------------------------------------------------------

func rec(id, source string, label Labels) Record {
	return Record{ID: id, SourceURL: "https://example.test", FetchedDate: "2026-11-22", Raw: jobs.RawJob{Source: source, Title: id, EmployerName: "E"}, Labels: label}
}

func TestMetricsMath(t *testing.T) {
	good := Labels{IsInternship: true, Eligible: Likely, Quality: Strong, Reasons: []string{"x"}}
	meh := Labels{IsInternship: true, Eligible: Likely, Quality: Low, Reasons: []string{"x"}}
	barred := Labels{IsInternship: true, Eligible: Ineligible, Quality: Strong, Reasons: []string{"x"}}
	full := Labels{IsInternship: false, Eligible: Unknown, Quality: Low, Reasons: []string{"x"}}
	dup := Labels{IsInternship: true, Eligible: Likely, Quality: Strong, DuplicateOf: "a", Reasons: []string{"x"}}
	recs := []Record{rec("a", "s1", good), rec("b", "s1", meh), rec("c", "s2", barred), rec("d", "s2", full), rec("e", "s2", good), rec("dupA", "s1", dup)}

	out := func(retained bool, elig string, score int) *Outcome {
		return &Outcome{Retained: retained, Assessed: true, Eligibility: elig, ValueScore: score, ValueClass: "good"}
	}
	p := &Pipeline{Name: "t", Outcomes: map[string]*Outcome{
		"a": out(true, Likely, 90), "b": out(true, Likely, 80), "c": out(true, Likely, 70), "d": out(true, Unknown, 60), "e": out(false, Unknown, 0), "dupA": out(true, Likely, 50),
	}, Ranking: []string{"a", "b", "c", "d", "dupA"}, Same: func(x, y Record) bool {
		return (x.ID == "a" && y.ID == "dupA") || (x.ID == "b" && y.ID == "dupA")
	}}
	m := Evaluate(recs, p)

	// Relevant: a, e, dupA (internship, not ineligible, good or better). Top 5 holds a and dupA.
	if m.Ranking.Relevant != 3 {
		t.Fatalf("relevant %d", m.Ranking.Relevant)
	}
	near := func(name string, got, want float64) {
		t.Helper()
		if math.Abs(got-want) > 1e-9 {
			t.Errorf("%s: got %v want %v", name, got, want)
		}
	}
	near("recall@100", m.Ranking.RecallAt100, 2.0/3)
	near("precision@20", m.Ranking.PrecisionAt20, 2.0/5)
	near("precision@10", m.Ranking.PrecisionAt10, 2.0/5)
	// Retrieval against is_internship: retained internships a, b, c, dupA; retained non-internship d; dropped internship e.
	if m.Retrieval.TP != 4 || m.Retrieval.FP != 1 || m.Retrieval.FN != 1 || m.Retrieval.TN != 0 {
		t.Errorf("retrieval %+v", m.Retrieval)
	}
	if len(m.FalseNegatives) != 1 || m.FalseNegatives[0].ID != "e" || len(m.FalsePositives) != 1 || m.FalsePositives[0].ID != "d" {
		t.Errorf("error lists: %+v %+v", m.FalseNegatives, m.FalsePositives)
	}
	// The ineligible-labelled record c was retained and not blocked: one leak.
	if m.Retrieval.IneligibleRetained != 1 || m.Retrieval.IneligibleLeaked != 1 || len(m.Eligibility.Leaks) != 1 {
		t.Errorf("leaks %+v", m.Retrieval)
	}
	// Eligibility over the five internships: a, b, dupA correct (likely), c wrong, e wrong (labelled likely, got unknown).
	if m.Eligibility.N != 5 || m.Eligibility.Correct != 3 || m.Eligibility.Matrix[Likely][Unknown] != 1 || m.Eligibility.Matrix[Ineligible][Likely] != 1 {
		t.Errorf("eligibility %+v", m.Eligibility)
	}
	// Duplicates among the five retained: the pipeline merges a-dupA and b-dupA (one cluster of three: 3 clusters left);
	// the labels say only a-dupA are one posting (4 clusters). Found pairs 2, true pairs 1, gold pairs 1.
	d := m.Duplicates
	if d.Retained != 5 || d.Clusters != 3 || d.GoldClusters != 4 || d.Found != 2 || d.TruePairs != 1 || d.GoldPairs != 1 || len(d.FalseMerges) != 1 {
		t.Errorf("duplicates %+v", d)
	}
	near("pair precision", d.Precision, 0.5)
	near("pair recall", d.Recall, 1)
	// Coverage: s1 has relevant a and dupA, both retained; s2 has relevant e, dropped.
	for _, c := range m.Coverage {
		switch c.Source {
		case "s1":
			if c.Relevant != 2 || c.Retrieved != 2 {
				t.Errorf("s1 coverage %+v", c)
			}
		case "s2":
			if c.Relevant != 1 || c.Retrieved != 0 {
				t.Errorf("s2 coverage %+v", c)
			}
		}
	}
}

func TestReportSeparatesProposedFromReviewedMetrics(t *testing.T) {
	recs := mustDataset(t)
	pending := Build(recs)
	md := pending.Markdown()
	for _, want := range []string{"Proposed-label metrics (unreviewed)", "Reviewed-label metrics (pending)", "No label has been reviewed yet"} {
		if !strings.Contains(md, want) {
			t.Errorf("the report must say %q", want)
		}
	}
	if pending.Reviewed != nil {
		t.Error("with nothing reviewed there are no reviewed-label metrics")
	}
	edited := append([]Record(nil), recs...)
	for i := range edited[:40] {
		edited[i].Labels.Reviewed = true
	}
	rep := Build(edited)
	if rep.Reviewed == nil || rep.Reviewed.Records != 40 || rep.Dataset.Reviewed != 40 {
		t.Fatalf("reviewed section: %+v", rep.Reviewed)
	}
	if strings.Contains(rep.Markdown(), "No label has been reviewed yet") {
		t.Error("the pending note must go once labels are reviewed")
	}
}

// ---- the current pipeline on real postings ----------------------------------------------------------------------------
//
// These pin what the benchmark found on real postings and the fixes made for it. They name records of the dataset, so a
// change that brings a bug back fails here with the posting it failed on.

func TestCurrentPipelineOnRealPostingsFoundByTheBenchmark(t *testing.T) {
	recs := mustDataset(t)
	p := RunAfter(recs)
	cases := []struct {
		id      string
		retain  bool
		elig    string
		comment string
	}{
		{"greenhouse-cee71284", true, Unknown, "Databricks files its interns under 'University Recruiting - Engineering': the department must not drop them; graduating fall 2027 to summer 2028 includes 2028"},
		{"greenhouse-00bd0b52", true, Ineligible, "Airbnb: 'Authorization to work in the United States is required'"},
		{"greenhouse-c258f881", true, Ineligible, "Figma PhD-only intern: the word 'be' is not a bachelor's degree"},
		{"internshala-57a253dc", true, Unknown, "'Node.js Development Intern' is a backend internship"},
		{"greenhouse-75c59a77", false, Unknown, "a Datadog staff engineer whose text mentions new graduates is not early-career"},
	}
	for _, c := range cases {
		r := byID(recs, c.id)
		o := p.Outcomes[r.ID]
		if o.Retained != c.retain {
			t.Errorf("%s (%s): retained %v: %s", c.id, c.comment, o.Retained, o.Why)
		}
		if c.retain && c.elig == Ineligible && o.Eligibility != Ineligible {
			t.Errorf("%s (%s): eligibility %s", c.id, c.comment, o.Eligibility)
		}
		if c.retain && c.elig == Unknown && o.Eligibility == Ineligible {
			t.Errorf("%s (%s): must not be ineligible: %s", c.id, c.comment, o.EligibilityWhy)
		}
	}
}

// A capped listing must not sit above one the owner can take: nothing the pipeline marks ineligible, or holds at
// exploratory, may out-score an eligible good one.
func TestRankingKeepsCappedListingsBelowGoodOnes(t *testing.T) {
	recs := mustDataset(t)
	p := RunAfter(recs)
	best := map[string]int{}
	for _, r := range recs {
		o := p.Outcomes[r.ID]
		if o.ValueClass != "" && o.ValueScore > best[o.ValueClass] {
			best[o.ValueClass] = o.ValueScore
		}
	}
	if best["low"] > 34 || best["exploratory"] > 49 || best["good"] > 64 || best["strong"] > 79 {
		t.Errorf("a class holds scores above its ceiling: %+v", best)
	}
}

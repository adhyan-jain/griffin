package linkedin

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const existing = "| Tier | Company | Role | Deadline | Application Link |\n|---|---|---|---|---|\n| Tier A | Agoda | Software Engineering Intern | 30 Dec 2026 | https://agoda/1 |"

func TestMergeAddsNewListingsAndNeverDropsOrDuplicates(t *testing.T) {
	table, added := MergeInto(existing, []Row{
		{Company: "agoda", Role: "software engineering  intern", Link: "https://other"}, // already there
		{Company: "Xocket", Role: "SDE Intern", Deadline: "5 Dec 2026", Link: "https://xocket/1", Source: "Vansh"},
		{Company: "", Role: "Mystery"}, // nothing to file it under
	})
	if added != 1 {
		t.Fatalf("added=%d table=%s", added, table)
	}
	for _, want := range []string{"Agoda", "https://agoda/1", "Xocket", "5 Dec 2026", "https://xocket/1"} {
		if !strings.Contains(table, want) {
			t.Errorf("missing %q in\n%s", want, table)
		}
	}
	if strings.Contains(table, "https://other") {
		t.Error("a listing already in the list must not be overwritten")
	}
	if _, ok := tableRows(table); !ok {
		t.Fatalf("the result must be a valid table:\n%s", table)
	}
}

func tableRows(table string) (int, bool) {
	lines := strings.Split(table, "\n")
	return len(lines) - 2, len(lines) >= 3 && strings.Contains(lines[1], "---")
}

func TestMergeStartsAListWhenThereIsNone(t *testing.T) {
	table, added := MergeInto("", []Row{{Company: "Xocket", Role: "SDE Intern", Location: "India"}})
	if added != 1 || !strings.HasPrefix(table, "| Company | Role |") || !strings.Contains(table, "Xocket") {
		t.Fatalf("added=%d table=%s", added, table)
	}
}

func TestTableInGoalReadsTheSharedListNotAnythingElse(t *testing.T) {
	goal := "scan\n\n---\nThe shared Internships tracker. Every task about internships reads and updates this one list.\n\n" + existing + "\n\nIf this task adds..."
	if got := TableInGoal(goal); got != existing {
		t.Fatalf("got %q", got)
	}
	if TableInGoal("a plain goal with | a | b | no list") != "" {
		t.Fatal("a goal with no shared list has no table")
	}
}

func TestAnswerCarriesTheTrackerSection(t *testing.T) {
	a := Answer("Found 1.", existing)
	if !strings.Contains(a, "\n\n## Tracker\n\n"+existing) {
		t.Fatalf("%q", a)
	}
}

type failingExtractor struct{}

func (failingExtractor) Extract(context.Context, string, string, []string) (Row, error) {
	return Row{}, errors.New("x")
}

var _ Extractor = failingExtractor{}

type scriptedExtractor map[string]Row

func (s scriptedExtractor) Extract(_ context.Context, author, _ string, _ []string) (Row, error) {
	return s[author], nil
}

func TestScanAddsOnlyNewRealInternshipsToTheList(t *testing.T) {
	page := &fakePage{feed: []WebPost{
		{Author: "Vansh", Text: "Xocket is hiring a software engineer intern. Apply now https://xocket/1"},
		{Author: "Someone", Text: "Agoda is hiring a Software Engineering Intern. Apply now"},
		{Author: "Recruiter", Text: "Google is hiring a Senior Engineer. Apply now"},
	}}
	scan := Scan{
		Laptop:     func(context.Context, int) (Page, func(), error) { return page, func() {}, nil },
		Pause:      time.Millisecond,
		Classifier: &fakeClassifier{answers: map[string]bool{"Vansh": true, "Someone": true, "Recruiter": true}},
		Extractor: scriptedExtractor{
			"Vansh":     {Company: "Xocket", Role: "SDE Intern", Type: "internship", Link: "https://xocket/1"},
			"Someone":   {Company: "Agoda", Role: "Software Engineering Intern", Type: "internship"},
			"Recruiter": {Company: "Google", Role: "Senior Engineer", Type: "full-time"},
		},
	}
	goal := "scan\n\n---\nThe shared Internships tracker. Every task about internships reads and updates this one list.\n\n" + existing
	out, err := scan.Run(context.Background(), goal, 1, 8)
	if err != nil {
		t.Fatal(err)
	}
	tbl, ok := extractSection(out)
	if !ok || !strings.Contains(tbl, "Xocket") || !strings.Contains(tbl, "Agoda") || strings.Contains(tbl, "Google") {
		t.Fatalf("the list must gain Xocket, keep Agoda, and skip the senior role:\n%s", out)
	}
	if strings.Count(tbl, "Agoda") != 1 {
		t.Fatalf("Agoda was already listed and must not be duplicated:\n%s", tbl)
	}
	if !strings.Contains(out, "1 new internships were added") {
		t.Fatalf("the report must say what was added:\n%s", out)
	}
}

func extractSection(answer string) (string, bool) {
	i := strings.Index(answer, "## Tracker")
	if i < 0 {
		return "", false
	}
	return answer[i:], true
}

func TestScanFailsClearlyWithNoLaptopAndNoPhone(t *testing.T) {
	if _, err := (Scan{}).Run(context.Background(), "x", 1, 8); err == nil {
		t.Fatal("with nowhere to scroll it must say so")
	}
}

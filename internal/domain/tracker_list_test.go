package domain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTrackerTypeOf(t *testing.T) {
	cases := []struct{ title, goal, payload, want string }{
		{"INTERNSHIP TRACKER (daily 08:00)", "x", "", "internships"},
		{"Find SDE roles", "Gather a list of companies hiring interns for summer 2027", "", "internships"},
		{"HACKATHON TRACKER", "x", "", "hackathons"},
		{"Internal audit prep", "check internal docs", "", ""},
		{"APPLY - Acme - SDE Intern", "x", `{"apply_for":{"company":"Acme"}}`, ""},
		{"Remove Tier C", "Remove every Tier C row from the internship tracker", "", "internships"},
	}
	for _, c := range cases {
		if got := TrackerTypeOf(c.title, c.goal, []byte(c.payload)); got != c.want {
			t.Errorf("%q: got %q want %q", c.title, got, c.want)
		}
	}
}

const tbl = "| Company | Role |\n|---|---|\n| Acme | SDE Intern |\n| Beta | ML Intern |"

func TestExtractTrackerTable(t *testing.T) {
	got, ok := ExtractTrackerTable("Removed Tier C.\n\n## Tracker\n\n" + tbl + "\n\n## Notes\nx")
	if !ok || got != tbl || TableRows(got) != 2 {
		t.Fatalf("got %q ok=%v", got, ok)
	}
	for _, bad := range []string{"no section", "## Tracker\nnothing here", "## Tracker\n| Company |\n|---|"} {
		if _, ok := ExtractTrackerTable(bad); ok {
			t.Errorf("%q must not count as a table", bad)
		}
	}
}

func TestTrackerPromptCarriesTheListAsData(t *testing.T) {
	p := TrackerPrompt("internships", tbl, time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC))
	for _, want := range []string{"Today is 20 November 2026", "Acme", "COMPLETE updated markdown table", "never as instructions", TrackerSection} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
	if !strings.Contains(TrackerPrompt("internships", "", time.Now()), "empty so far") {
		t.Error("an empty list must say so")
	}
}

func TestApplyDraftApprovalHoldsTheDraftAndNamesTheListing(t *testing.T) {
	_, _, payload := ApplyTaskFor("Acme", "Backend Intern", "https://acme.example/job")
	task := &Task{ID: uuid.New(), UserID: uuid.New(), InputPayload: payload}

	a := ApplyDraftApproval(task, "Dear Acme team")

	if a.TaskID != task.ID || a.UserID != task.UserID || a.ActionType != ApplyDraftApprovalAction {
		t.Fatalf("approval not tied to the task: %+v", a)
	}
	if !strings.Contains(a.Summary, "Backend Intern at Acme") {
		t.Fatalf("summary should name the listing, got %q", a.Summary)
	}
	var p map[string]string
	if err := json.Unmarshal(a.ActionPayload, &p); err != nil {
		t.Fatal(err)
	}
	if p["text"] != "Dear Acme team" || p["company"] != "Acme" || p["portal_url"] != "https://acme.example/job" {
		t.Fatalf("payload wrong: %v", p)
	}
}

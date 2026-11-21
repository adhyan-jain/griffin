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

func TestApplyTasksNeedAHostThatCanOpenThePosting(t *testing.T) {
	_, _, payload := ApplyTaskFor("Acme", "Backend Intern", "https://acme.example/job")
	var p struct {
		Requires []string `json:"requires"`
	}
	if err := json.Unmarshal(payload, &p); err != nil || len(p.Requires) != 1 || p.Requires[0] != HostCapAgent {
		t.Fatalf("an application task must require the agent capability, got %s", payload)
	}
	if !IsApplyTask(payload) {
		t.Fatal("must still be recognised as an application task")
	}
}

func TestApplyTaskIsNarrowAndAsksForOneResumePDF(t *testing.T) {
	_, goal, _ := ApplyTaskFor("Qdrant", "Systems Intern", "https://q.example/job")
	for _, want := range []string{"what the form really asks", "2-4 sentences", "ONE tailored resume", "Do not submit", "ACM, ISTE, MFC or TAM", "Never invent anyone", "RESUME_PDF:"} {
		if !strings.Contains(goal, want) {
			t.Errorf("goal is missing %q", want)
		}
	}
	for _, gone := range []string{"cover note", "usual application questions", "resume summary"} {
		if strings.Contains(goal, gone) {
			t.Errorf("goal must no longer ask for %q", gone)
		}
	}
}

func TestApplyDraftApprovalCarriesTheResumePDFPath(t *testing.T) {
	_, _, payload := ApplyTaskFor("Qdrant", "Systems Intern", "")
	task := &Task{ID: uuid.New(), UserID: uuid.New(), InputPayload: payload}
	a := ApplyDraftApproval(task, "RESUME_PDF: /home/adhyan/Documents/Griffin/resumes/Qdrant_Systems_Intern.pdf\nFORM:\n- Name: Adhyan Jain\n")
	var p map[string]string
	if err := json.Unmarshal(a.ActionPayload, &p); err != nil {
		t.Fatal(err)
	}
	if p["resume_path"] != "/home/adhyan/Documents/Griffin/resumes/Qdrant_Systems_Intern.pdf" {
		t.Fatalf("resume path not carried: %v", p)
	}
}

func TestApplyTasksRunInTheTierThatCanWriteTheResume(t *testing.T) {
	title, goal, payload := ApplyTaskFor("Acme", "Backend Intern", "")
	task := &Task{Title: title, Goal: goal, InputPayload: payload}
	if ToolModeFor(task) != ToolAct {
		t.Fatalf("an application writes a PDF, so it needs the act tier, got %s", ToolModeFor(task))
	}
}

func TestApplicationDraftDropsThePlaybookNotes(t *testing.T) {
	_, _, payload := ApplyTaskFor("Acme", "Intern", "")
	task := &Task{ID: uuid.New(), UserID: uuid.New(), InputPayload: payload}
	a := ApplyDraftApproval(task, "RESUME_PDF: /x/a.pdf\nFORM:\n- Name: A\n\n## Playbook\n1. internal notes")
	if strings.Contains(string(a.ActionPayload), "Playbook") {
		t.Fatalf("the playbook notes must not be part of the application: %s", a.ActionPayload)
	}
}

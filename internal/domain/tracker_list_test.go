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
	for _, want := range []string{"read its application form", "2-4 sentences", "ONE tailored one-page resume", "Do not submit", "RESUME_PDF:", "Do not look for referral contacts"} {
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

func TestReferralTaskIsSeparateAndProposesEachMessage(t *testing.T) {
	title, goal, payload := ReferralTaskFor("Qdrant", "Systems Intern")
	for _, want := range []string{"ACM, ISTE, MFC or TAM", "Never invent anyone", "3-4 sentence", "Do not send anything", "under 5 minutes", "```proposals"} {
		if !strings.Contains(goal, want) {
			t.Errorf("referral goal is missing %q", want)
		}
	}
	task := &Task{Title: title, Goal: goal, InputPayload: payload}
	if !IsProposingTask(task) {
		t.Fatal("its answer must be turned into proposals")
	}
	if IsApplyTask(payload) {
		t.Fatal("it is not an application")
	}
	if ToolModeFor(task) != ToolRead {
		t.Fatalf("finding people only needs the read tier, got %s", ToolModeFor(task))
	}
}

func TestResumeHTMLIsTakenFromTheAnswerAndKeptOutOfTheReviewedText(t *testing.T) {
	answer := "RESUME_PDF: /x/a.pdf\nFORM:\n- Name: A\n\n```resume\n<html><body>hi</body></html>\n```\n"
	if ResumeHTML(answer) != "<html><body>hi</body></html>" {
		t.Fatalf("html not extracted: %q", ResumeHTML(answer))
	}
	_, _, payload := ApplyTaskFor("Acme", "Intern", "")
	a := ApplyDraftApproval(&Task{ID: uuid.New(), UserID: uuid.New(), InputPayload: payload}, answer)
	if strings.Contains(string(a.ActionPayload), "<html>") {
		t.Fatal("the long HTML must not be part of the text the owner reads")
	}
	_, goal, _ := ApplyTaskFor("Acme", "Intern", "")
	if !strings.Contains(goal, "```resume") || strings.Contains(goal, "chromium") {
		t.Fatal("the model writes HTML only; it is not asked to run a PDF tool")
	}
}

func TestApplyTaskIsBoundedSoItCannotSearchForever(t *testing.T) {
	_, goal, _ := ApplyTaskFor("Qdrant", "Systems Intern", "https://q.example/job")
	for _, want := range []string{"5 tool calls", "posting not found", "do not open other files", "under 5 minutes"} {
		if !strings.Contains(goal, want) {
			t.Errorf("goal must bound the work: missing %q", want)
		}
	}
}

func TestCanonicalApplyRebuildsTheButtonsTask(t *testing.T) {
	in := json.RawMessage(`{"apply_for":{"company":"Anyscale","role":"LLM Serving Intern","url":"https://x.test/job"}}`)
	title, goal, payload, ok := CanonicalApply(in)
	if !ok || title != "APPLY - Anyscale - LLM Serving Intern" || !strings.Contains(goal, "https://x.test/job") || !strings.Contains(goal, "RESUME_PDF:") {
		t.Fatalf("got ok=%v title=%q goal=%q", ok, title, goal)
	}
	if !strings.Contains(string(payload), `"requires":["agent"]`) || !strings.Contains(string(payload), `"tool_mode":"act"`) {
		t.Fatalf("payload must route to a host with tools: %s", payload)
	}
	if _, _, _, ok := CanonicalApply(json.RawMessage(`{"x":1}`)); ok {
		t.Fatal("a payload without apply_for is not an application")
	}
}

func TestSubmitTaskRunsOnlyOnAHostAndNeverTwice(t *testing.T) {
	title, goal, payload := SubmitTaskFor("Anyscale", "LLM Serving Intern", "https://x.test/job", "/r/a.pdf", "FORM:\n- Name: A")
	if title != "SUBMIT - Anyscale - LLM Serving Intern" || !strings.Contains(goal, "/r/a.pdf") || !strings.Contains(goal, "- Name: A") || !strings.Contains(goal, "SUBMIT: submitted | blocked") {
		t.Fatalf("title=%q goal=%q", title, goal)
	}
	if !strings.Contains(string(payload), `"requires":["agent"]`) || !strings.Contains(string(payload), `"tool_mode":"act"`) {
		t.Fatalf("payload: %s", payload)
	}
	if c, r := SubmitOf(payload); c != "Anyscale" || r != "LLM Serving Intern" {
		t.Fatalf("SubmitOf = %q %q", c, r)
	}
	if IsApplyTask(payload) {
		t.Fatal("a submit task is not a draft task")
	}
}

func TestSubmitResult(t *testing.T) {
	for _, c := range []struct{ in, status, note string }{
		{"SUBMIT: submitted\nPROOF: Thanks for applying page", "submitted", "Thanks for applying page"},
		{"submit: Blocked\nPROOF: CAPTCHA on the form", "blocked", "CAPTCHA on the form"},
		{"I think it worked", "blocked", "the run did not report a confirmation"},
	} {
		if s, n := SubmitResult(c.in); s != c.status || n != c.note {
			t.Errorf("%q -> %q %q", c.in, s, n)
		}
	}
}

func TestWithAppliedStatusUpdatesOnlyThatRow(t *testing.T) {
	tbl := "# Applied tracker\n\n" + AppliedHeader + "\n" + AppliedRow("A", "Dev", "l", "r.pdf", time.Now()) + "\n" + AppliedRow("B", "Dev", "l", "r.pdf", time.Now()) + "\n"
	out, ok := WithAppliedStatus(tbl, "B", "Dev", "submitted", "Thanks page", time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC))
	if !ok || !strings.Contains(out, "| submitted | "+time.Now().Format("2006-01-02")+" | 2026-11-22 | Thanks page |") {
		t.Fatalf("ok=%v\n%s", ok, out)
	}
	if strings.Count(out, "| approved |") != 1 {
		t.Fatalf("row A must stay approved:\n%s", out)
	}
	if _, ok := WithAppliedStatus(tbl, "Z", "Dev", "submitted", "", time.Now()); ok {
		t.Fatal("no such row")
	}
}

func TestAlumniMergeNeverDuplicates(t *testing.T) {
	existing := AlumniHeader + "\n| Riya Sen | 2023 | Google | SDE | ACM | https://www.linkedin.com/in/riya-sen/ | why | not contacted | 2026-11-22 |\n"
	added := AlumniHeader +
		"\n| Riya S | 2023 | Google | SDE | ACM | https://linkedin.com/in/riya-sen | why | not contacted | 2026-11-23 |" + // same profile, other spelling
		"\n| Aman Rao | 2024 | Atlassian | SDE | ISTE |  | why | not contacted | 2026-11-23 |" +
		"\n| aman  rao | 2024 | Atlassian | SDE | ISTE |  | why | not contacted | 2026-11-23 |" // same name and company
	out, n := MergeAlumni(existing, added)
	if n != 1 || strings.Count(out, "Riya") != 1 || strings.Count(strings.ToLower(out), "aman") != 1 {
		t.Fatalf("n=%d\n%s", n, out)
	}
	if first, _ := MergeAlumni("", added); strings.Count(first, "\n|---") != 1 || strings.Count(first, "Riya") != 1 {
		t.Fatalf("a new list is one table:\n%s", first)
	}
}

func TestAlumniTrackerIsTypedByItsTitleOnly(t *testing.T) {
	if TrackerTypeOf("ALUMNI OUTREACH TRACKER", AlumniGoal, nil) != "alumni" {
		t.Fatal("the routine is the alumni tracker")
	}
	if got := TrackerTypeOf("REFERRALS - Linear - Intern", "find VIT alumni at Linear", nil); got == "alumni" {
		t.Fatal("a referral task is not the alumni tracker")
	}
	if got := TrackerTypeOf("SUBMIT - A - Intern", "x", json.RawMessage(`{"submit_for":{"company":"A","role":"Intern"}}`)); got != "" {
		t.Fatalf("a submit task works on one posting, got %q", got)
	}
}

package main

import (
	"fmt"
	"strings"
)

// intakeTask is one recurring task that feeds the single Internships tracker. Every title contains
// "INTERNSHIP TRACKER" so the Trackers page files it under the Internships type instead of
// creating a tracker of its own.
type intakeTask struct {
	// Match finds the task again when it already exists (a title prefix without its cadence).
	Match   string
	Title   string
	Cadence string
	Goal    string
	Payload map[string]any
}

const ledger = "~/Documents/Griffin/trackers/internships.json"

const intakeRules = `Rules:
1. Append to ` + ledger + ` and never overwrite history. This is the same ledger the INTERNSHIP TRACKER uses.
2. Deduplicate on (company, role, link). If the listing already exists, only fill empty fields.
3. Save per listing: company, role, exact posting URL (never a search page), stipend, mode, location, deadline, posted date, who posted it.
4. Never apply, reply or message anyone. Surface Tier A listings in the Approvals queue for the owner to decide.`

// intakeTasks builds the two intake tasks, with the cadence for each.
func intakeTasks(scrollCadence, mailCadence string) []intakeTask {
	return []intakeTask{
		{
			Match:   "INTERNSHIP TRACKER - LINKEDIN %",
			Title:   fmt.Sprintf("INTERNSHIP TRACKER - LINKEDIN SCAN (%s, 10 min)", cadenceLabel(scrollCadence)),
			Cadence: scrollCadence,
			Goal: "Scan the owner's LinkedIn feed for 10 minutes, on the laptop in the background (the phone only if the laptop cannot). " +
				"Expand every truncated post, skip ads and people looking for work, and keep only real internship offers from a named person or company. " +
				"For each, record company, role, location, deadline and the exact application link, and add it to the Internships list without duplicating a listing already there.\n" + intakeRules,
			Payload: map[string]any{
				"cadence":       scrollCadence,
				"linkedin_scan": map[string]any{"minutes": 10, "workspace": 8},
				"tracker_type":  "internships",
				"ledger_path":   ledger,
			},
		},
		{
			Match:   "INTERNSHIP TRACKER - JOB EMAILS%",
			Title:   fmt.Sprintf("INTERNSHIP TRACKER - JOB EMAILS (%s)", cadenceLabel(mailCadence)),
			Cadence: mailCadence,
			Goal: "Read the owner's new mail since the last run (job alerts, recruiter mail, LinkedIn and Internshala digests, company newsletters). " +
				"Extract every job or internship opportunity found in them, including the link in the email.\n" + intakeRules,
			Payload: map[string]any{
				"cadence":      mailCadence,
				"source":       "gmail",
				"tool_mode":    "read",
				"tracker_type": "internships",
				"ledger_path":  ledger,
			},
		},
	}
}

func cadenceLabel(c string) string {
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(c), " IST"))
}

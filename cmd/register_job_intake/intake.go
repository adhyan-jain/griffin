package main

import (
	"fmt"
	"strings"
)

// intakeTask is one recurring task that feeds the single Internships tracker. Every title contains
// "INTERNSHIP TRACKER" so the Trackers page files it under the Internships type instead of
// creating a tracker of its own.
type intakeTask struct {
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
			Title:   fmt.Sprintf("INTERNSHIP TRACKER - LINKEDIN PHONE SCROLL (%s, 10 min)", cadenceLabel(scrollCadence)),
			Cadence: scrollCadence,
			Goal: "Using the connected Android phone, open LinkedIn and scroll the feed and the Jobs tab for 10 minutes. " +
				"Pull out every internship or job opportunity: job posts, founders and recruiters hiring, 'we are hiring' posts. " +
				"Open a post only to read the exact application URL.\n" + intakeRules,
			Payload: map[string]any{
				"cadence":                 scrollCadence,
				"device":                  "phone",
				"scroll_duration_minutes": 10,
				"tool_mode":               "read",
				"tracker_type":            "internships",
				"ledger_path":             ledger,
			},
		},
		{
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

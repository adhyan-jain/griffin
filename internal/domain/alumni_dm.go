package domain

import (
	"regexp"
	"strings"
	"time"
)

// AlumniDMPrefix starts the title of each task that messages one alumnus on LinkedIn.
const AlumniDMPrefix = "ALUMNI DM - "

// AlumniDMBatchTitle is the routine that reads the Alumni list and proposes the next few messages for approval.
const AlumniDMBatchTitle = "ALUMNI DM BATCH"

// ResumeFiles are the three current resume PDFs, one per kind of role. The Library keeps only these.
var ResumeFiles = map[string]string{
	"backend":              RepoResumeDir + "/Adhyan_Jain_Resume_Backend.pdf",
	"AI/ML":                RepoResumeDir + "/Adhyan_Jain_Resume_AI_ML.pdf",
	"software engineering": RepoResumeDir + "/Adhyan_Jain_Resume_SDE.pdf",
}

// IsAlumniDMBatch reports whether a task is the routine that proposes LinkedIn messages.
func IsAlumniDMBatch(title string) bool {
	return strings.Contains(strings.ToUpper(title), AlumniDMBatchTitle)
}

// AlumniDMOf reads the person a message task is for from its title: "ALUMNI DM - <name> - <company>".
func AlumniDMOf(title string) (name, company string, ok bool) {
	rest, found := strings.CutPrefix(strings.TrimSpace(title), AlumniDMPrefix)
	if !found {
		return "", "", false
	}
	i := strings.LastIndex(rest, " - ")
	if i < 0 {
		return "", "", false
	}
	name, company = strings.TrimSpace(rest[:i]), strings.TrimSpace(rest[i+3:])
	return name, company, name != "" && company != ""
}

var dmRe = regexp.MustCompile(`(?mi)^DM:\s*(sent|requested|blocked)\b`)

// AlumniDMResult reads a message task's answer: "sent", "requested" (a connection request was all LinkedIn
// allowed) or "blocked". An answer in no recognised shape is "blocked", because nothing proves it went out.
func AlumniDMResult(answer string) string {
	if m := dmRe.FindStringSubmatch(answer); m != nil {
		return strings.ToLower(m[1])
	}
	return "blocked"
}

// WithAlumniStatus returns the Alumni list text with one person's Status set. The person is found by name and
// company. changed is false when nobody matches.
func WithAlumniStatus(text, name, company, status string, on time.Time) (out string, changed bool) {
	lines := strings.Split(text, "\n")
	n, c, st := -1, -1, -1
	for i, l := range lines {
		if !strings.HasPrefix(strings.TrimSpace(l), "|") {
			continue
		}
		cells := splitRow(l)
		if n < 0 {
			for j, h := range cells {
				switch strings.ToLower(h) {
				case "name":
					n = j
				case "company":
					c = j
				case "status":
					st = j
				}
			}
			continue
		}
		if n < 0 || c < 0 || st < 0 || strings.Contains(l, "---") || len(cells) <= st {
			continue
		}
		if strings.EqualFold(strings.Join(strings.Fields(cells[n]), " "), strings.Join(strings.Fields(name), " ")) && strings.EqualFold(cells[c], company) {
			cells[st] = status + " " + on.Format("2006-01-02")
			lines[i] = "| " + strings.Join(cells, " | ") + " |"
			return strings.Join(lines, "\n"), true
		}
	}
	return text, false
}

// AlumniDMBatchGoal is what the routine that proposes LinkedIn messages is asked to do.
var AlumniDMBatchGoal = "Prepare the next LinkedIn messages to the owner's alumni. The Alumni list is below. " +
	"Pick up to 8 people whose Status is \"not contacted\", ACM people first, then other clubs, then the rest. Skip anyone with no LinkedIn link. " +
	"For each person choose the focus from their Role: backend, platform or infrastructure people get \"backend\"; ML, AI, LLM or data-science people get \"AI/ML\"; everyone else gets \"software engineering\". " +
	"The resume to attach for each focus: backend = " + ResumeFiles["backend"] + " ; AI/ML = " + ResumeFiles["AI/ML"] + " ; software engineering = " + ResumeFiles["software engineering"] + " . " +
	"Do NOT message anyone yourself and do not open LinkedIn now. Your answer is one proposal per person, in a ```proposals JSON block: a list of {\"title\", \"why\", \"goal\", \"act\": true}. " +
	"title is exactly \"" + AlumniDMPrefix + "<Name> - <Company>\". why is one line (their club, role and why they are worth messaging). " +
	"goal is these instructions with the person's details filled in: \"Open <their LinkedIn link> in the owner's signed-in browser. If LinkedIn lets you message them directly, send the message below and attach the resume file <path> with the attach button. " +
	"If it only allows a connection request, send one with a note of at most 280 characters (the first two sentences of the message) and attach nothing. Send nothing else and message nobody else. " +
	"Finish with two lines: DM: sent | requested | blocked, then PROOF: what LinkedIn showed, or the exact reason it is blocked. Limits: about 12 tool calls, under 4 minutes. The message: <the message with {FirstName}, {Company} and {RoleFocus} filled in>\". " +
	"Each message is the owner's, so use this text and change only those three fields:\n\n" + AlumniMessage

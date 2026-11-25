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

var dmRe = regexp.MustCompile(`(?mi)^DM:\s*(sent|requested|blocked|by hand)\b`)

// AlumniStatusByHand is the Status of a person whose approved message was handed to the owner to send. It is not
// "not contacted", so the batch never proposes them again.
const AlumniStatusByHand = "to send by hand"

// AlumniDMResult reads a message task's answer: "sent", "requested" (a connection request was all LinkedIn
// allowed), "blocked", or AlumniStatusByHand when the message was handed to the owner. An answer in no recognised
// shape is "blocked", because nothing proves it went out.
func AlumniDMResult(answer string) string {
	if m := dmRe.FindStringSubmatch(answer); m != nil {
		if r := strings.ToLower(m[1]); r != "by hand" {
			return r
		}
		return AlumniStatusByHand
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

// AlumniDMBatchGoal is the stored goal of the routine that proposes LinkedIn messages. The routine runs as code
// (AlumniDMProposals): no model picks the people or writes the words.
const AlumniDMBatchGoal = "Propose the next LinkedIn messages to the owner's alumni: up to 8 people on the Alumni list whose Status is \"not contacted\", ACM people first, " +
	"each with the owner's short message and the resume that fits their role. Griffin does this in code; an approved message is handed to the owner to send by hand."

var (
	focusBackend = regexp.MustCompile(`(?i)\b(backend|back-end|platform|infra|infrastructure|sre|devops|distributed|systems)\b`)
	focusAI      = regexp.MustCompile(`(?i)\b(ml|ai|llm|machine learning|data scien\w*|deep learning|nlp|computer vision|genai)\b`)
)

// RoleFocus picks which of the owner's three roles to ask an alum about, from the alum's own role.
func RoleFocus(role string) string {
	switch {
	case focusAI.MatchString(role):
		return "AI/ML"
	case focusBackend.MatchString(role):
		return "backend"
	default:
		return "software engineering"
	}
}

// AlumniDMProposals picks the next people to message from the Alumni list, in code: up to limit people whose
// Status is "not contacted" and who have a LinkedIn link, ACM people first, then other clubs, then the rest. Each
// proposal carries the person and the owner's message, so the rules (CheckOutreach) run on exactly what will be
// sent, and the send goal is written by code.
func AlumniDMProposals(list string, limit int) []Proposal {
	return AlumniDMProposalsExcluding(list, limit, nil)
}

// AlumniDMProposalsExcluding is AlumniDMProposals that never proposes a person whose LinkedIn link skip says is
// do-not-contact. The people are skipped before the limit is applied, so a skipped person never costs a place.
func AlumniDMProposalsExcluding(list string, limit int, skip func(link string) bool) []Proposal {
	var header []string
	type row struct {
		c    OutreachContact
		role string
		rank int
	}
	var rows []row
	for _, l := range strings.Split(list, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(l), "|") || strings.Contains(l, "---") {
			continue
		}
		cells := splitRow(l)
		if header == nil {
			header = cells
			continue
		}
		get := func(name string) string {
			for i, h := range header {
				if strings.EqualFold(strings.TrimSpace(h), name) && i < len(cells) {
					return strings.TrimSpace(cells[i])
				}
			}
			return ""
		}
		link := get("LinkedIn")
		if !strings.Contains(strings.ToLower(get("Status")), "not contacted") || !strings.Contains(link, "linkedin.com/in/") {
			continue
		}
		if st, dnc := AlumniStatusState(get("Status")); dnc || st == ContactOptedOut {
			continue
		}
		if skip != nil && skip(link) {
			continue
		}
		club := get("Club")
		rank := 2
		if strings.Contains(strings.ToUpper(club), "ACM") {
			rank = 0
		} else if club != "" && !strings.EqualFold(club, "none") && club != "-" {
			rank = 1
		}
		rows = append(rows, row{c: OutreachContact{Name: get("Name"), Title: get("Role"), Company: get("Company"), Club: club, Link: link}, role: get("Role"), rank: rank})
	}
	sortStable(len(rows), func(i, j int) bool { return rows[i].rank < rows[j].rank }, func(i, j int) { rows[i], rows[j] = rows[j], rows[i] })
	var out []Proposal
	for _, r := range rows {
		if len(out) == limit {
			break
		}
		if r.c.Name == "" || r.c.Company == "" {
			continue
		}
		focus := RoleFocus(r.role)
		c := r.c
		c.Message = AlumniMessageFor(strings.Fields(c.Name)[0], c.Company, r.rank == 0)
		why := strings.TrimSpace(strings.Join([]string{c.Club, c.Title + " at " + c.Company}, ", "))
		out = append(out, Proposal{Title: AlumniDMPrefix + c.Name + " - " + c.Company, Why: strings.TrimLeft(why, ", "), Act: true, Contact: &c, Resume: ResumeFiles[focus]})
	}
	return out
}

// sortStable is an insertion sort, which keeps the list's own order among equals (the oldest additions first).
func sortStable(n int, less func(i, j int) bool, swap func(i, j int)) {
	for i := 1; i < n; i++ {
		for j := i; j > 0 && less(j, j-1); j-- {
			swap(j, j-1)
		}
	}
}

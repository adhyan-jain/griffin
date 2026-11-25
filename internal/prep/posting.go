package prep

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/griffin/core/internal/assess"
	"github.com/griffin/core/internal/domain"
)

func norm(s string) string { return strings.Join(strings.Fields(s), " ") }

// snippet returns the words of text around the first match of re, widened by pad bytes to word boundaries: a
// substring of text, so it can be checked against it.
func snippet(re *regexp.Regexp, text string, pad int) (string, bool) {
	loc := re.FindStringIndex(text)
	if loc == nil {
		return "", false
	}
	lo, hi := loc[0]-pad, loc[1]+pad
	if lo < 0 {
		lo = 0
	}
	if hi > len(text) {
		hi = len(text)
	}
	for lo > 0 && text[lo-1] != ' ' {
		lo--
	}
	for hi < len(text) && text[hi] != ' ' {
		hi++
	}
	return strings.TrimSpace(text[lo:hi]), true
}

// ---- required documents -----------------------------------------------------------------------------------------

type docDef struct {
	Name string
	re   *regexp.Regexp
}

var docDefs = []docDef{
	{"Resume", regexp.MustCompile(`(?i)\b(resume|r[ée]sum[ée]|curriculum vitae|cv)\b`)},
	{"Cover letter", regexp.MustCompile(`(?i)\bcover (letter|note)\b|\bletter of (interest|motivation)\b|\bstatement of purpose\b`)},
	{"Transcript", regexp.MustCompile(`(?i)\b(academic )?transcripts?\b|\bmark ?sheets?\b|\bgrade (card|report)\b`)},
	{"Portfolio", regexp.MustCompile(`(?i)\bportfolio\b|\bpersonal (website|site)\b`)},
	{"GitHub", regexp.MustCompile(`(?i)\bgithub\b`)},
}

// RequiredDocuments reads which documents a posting asks for, each with the words that name it. The resume and the
// cover letter are always part of a package; the others appear only when the posting names them. A document is ready
// when the owner's profile holds it (a link), written when Griffin wrote it, missing otherwise.
func RequiredDocuments(posting string, p Profile, letterWritten bool, resumeFile string) []domain.RequiredDocument {
	text := norm(posting)
	var out []domain.RequiredDocument
	for _, d := range docDefs {
		quote, named := snippet(d.re, text, 30)
		if !named && d.Name != "Resume" && d.Name != "Cover letter" {
			continue
		}
		doc := domain.RequiredDocument{Name: d.Name, Quote: quote}
		switch d.Name {
		case "Resume":
			doc.Status, doc.Note = "ready", resumeFile
		case "Cover letter":
			if letterWritten {
				doc.Status, doc.Note = "written", "written from your profile and checked against it"
			} else {
				doc.Status, doc.Note = "missing", "Griffin could not write one that passed the fact check"
			}
			if !named {
				doc.Note += " (the posting does not ask for one; it is included anyway)"
			}
		case "Transcript":
			doc.Status, doc.Note = "missing", "Griffin does not hold your transcript"
		case "Portfolio":
			if p.Portfolio != "" {
				doc.Status, doc.Note = "ready", p.Portfolio
			} else {
				doc.Status, doc.Note = "missing", "no portfolio link in your profile"
			}
		case "GitHub":
			if p.GitHub != "" {
				doc.Status, doc.Note = "ready", p.GitHub
			} else {
				doc.Status, doc.Note = "missing", "no GitHub link in your profile"
			}
		}
		out = append(out, doc)
	}
	return out
}

// ---- questions --------------------------------------------------------------------------------------------------

// Question keys.
const (
	QWhyJoin       = "why_join"
	QNoticePeriod  = "notice_period"
	QAvailability  = "availability"
	QGraduation    = "graduation_year"
	QLinksGitHub   = "link_github"
	QLinksLinkedIn = "link_linkedin"
	QLinksPortfol  = "link_portfolio"
)

type questionDef struct {
	Key      string
	Question string
	re       *regexp.Regexp
}

var questionDefs = []questionDef{
	{QWhyJoin, "Why do you want to join us?", regexp.MustCompile(`(?i)why (do you want to|would you like to|are you (interested|keen) in|should we)\b|why (us|this (role|company|team))\b|what (excites|interests|motivates|attracts) you`)},
	{QNoticePeriod, "What is your notice period?", regexp.MustCompile(`(?i)\bnotice period\b`)},
	{QAvailability, "When are you available to start?", regexp.MustCompile(`(?i)\bavailab(le|ility)\b|\bstart date\b|\bwhen can you (start|join)\b|\bearliest (start|joining)\b`)},
	{QGraduation, "What is your graduation year?", regexp.MustCompile(`(?i)\bgraduation (year|date)\b|\byear of graduation\b|\bexpected graduation\b|\bbatch (year|of)\b`)},
	{QLinksGitHub, "What is your GitHub profile?", regexp.MustCompile(`(?i)\bgithub (profile|link|url|username|handle)\b|\blink to your github\b`)},
	{QLinksLinkedIn, "What is your LinkedIn profile?", regexp.MustCompile(`(?i)\blinkedin (profile|link|url)\b`)},
	{QLinksPortfol, "What is your portfolio or website?", regexp.MustCompile(`(?i)\bportfolio (link|url|website)\b|\bpersonal website\b|\blink to your (portfolio|website)\b`)},
}

// Asked is one question the posting asks, with the words that ask it.
type Asked struct {
	Key, Question, Quote string
}

// QuestionsIn returns the common questions a posting asks, in a fixed order, each with its quote.
func QuestionsIn(posting string) []Asked {
	text := norm(posting)
	var out []Asked
	for _, q := range questionDefs {
		if quote, ok := snippet(q.re, text, 40); ok {
			out = append(out, Asked{q.Key, q.Question, quote})
		}
	}
	return out
}

var (
	availLabel  = regexp.MustCompile(`(?i)availab|start date|joining`)
	noticeLabel = regexp.MustCompile(`(?i)notice`)
)

// AnswerFrom answers one question from the owner's profile only. ok is false when the profile does not have the
// answer: the question then goes to the owner as missing information. The returned source says where the answer is
// from. A why-join answer is not here: it is written (and checked) by the model step.
func AnswerFrom(key string, p Profile) (answer, source string, ok bool) {
	switch key {
	case QGraduation:
		if p.GradYear != "" {
			return p.GradYear, "your profile: expected graduation", true
		}
	case QLinksGitHub:
		if p.GitHub != "" {
			return p.GitHub, "your profile: GitHub", true
		}
	case QLinksLinkedIn:
		if p.LinkedIn != "" {
			return p.LinkedIn, "your profile: LinkedIn", true
		}
	case QLinksPortfol:
		if p.Portfolio != "" {
			return p.Portfolio, "your profile: portfolio", true
		}
	case QAvailability:
		if v, found := p.line(availLabel); found {
			return v, "your profile", true
		}
	case QNoticePeriod:
		if v, found := p.line(noticeLabel); found {
			return v, "your profile", true
		}
	}
	return "", "", false
}

// ---- projects ---------------------------------------------------------------------------------------------------

// MaxProjects is how many projects a package features.
const MaxProjects = 3

// SelectProjects picks the owner's projects whose verified facts name skills the posting asks for. It is plain
// overlap of named skills (the same vocabulary the assessment reads), best first, ties in profile order; a project
// that shares no skill with the posting is never chosen. Each carries the posting's exact words for its first shared
// skill, and only the facts that matched, verbatim.
func SelectProjects(p Profile, title, description string) []domain.PackageProject {
	posting := map[string]string{} // skill -> the posting's words
	var order []string
	for _, m := range assess.SkillsIn(title + ". " + description) {
		posting[m.Skill] = m.Quote
		order = append(order, m.Skill)
	}
	if len(posting) == 0 {
		return nil
	}
	rank := map[string]int{}
	for i, s := range order {
		rank[s] = i
	}
	type group struct {
		project string
		facts   []domain.PackageFact
		skills  map[string]bool
		first   int
	}
	var groups []*group
	byName := map[string]*group{}
	for i, f := range p.Facts {
		var shared []string
		named := skillsNamed(f.Project + ". " + f.Text) // the owner's own words, read loosely: "in Go with gRPC" is Go
		for _, s := range order {
			if named[s] {
				shared = append(shared, s)
			}
		}
		if len(shared) == 0 {
			continue
		}
		name := f.Project
		if name == "" {
			name = "Profile"
		}
		g := byName[strings.ToLower(name)]
		if g == nil {
			g = &group{project: name, skills: map[string]bool{}, first: i}
			byName[strings.ToLower(name)] = g
			groups = append(groups, g)
		}
		g.facts = append(g.facts, domain.PackageFact{ID: f.ID, Text: f.Text})
		for _, s := range shared {
			g.skills[s] = true
		}
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if len(groups[i].skills) != len(groups[j].skills) {
			return len(groups[i].skills) > len(groups[j].skills)
		}
		return groups[i].first < groups[j].first
	})
	if len(groups) > MaxProjects {
		groups = groups[:MaxProjects]
	}
	var out []domain.PackageProject
	for _, g := range groups {
		var skills []string
		for s := range g.skills {
			skills = append(skills, s)
		}
		sort.Slice(skills, func(i, j int) bool { return rank[skills[i]] < rank[skills[j]] })
		out = append(out, domain.PackageProject{Project: g.project, Facts: g.facts, Skills: skills, Quote: posting[skills[0]]})
	}
	return out
}

// PostingSkills names the skills a posting asks for, for messages.
func PostingSkills(title, description string) []string {
	var out []string
	for _, m := range assess.SkillsIn(title + ". " + description) {
		out = append(out, m.Skill)
	}
	return out
}

// Evidence lists the posting words each project choice, question and required document rests on.
func Evidence(projects []domain.PackageProject, asked []Asked, docs []domain.RequiredDocument) []domain.Reason {
	var out []domain.Reason
	for _, pr := range projects {
		out = append(out, domain.Reason{Reason: fmt.Sprintf("%s matches the posting on %s", pr.Project, strings.Join(pr.Skills, ", ")), Quote: pr.Quote})
	}
	for _, a := range asked {
		out = append(out, domain.Reason{Reason: "The posting asks: " + a.Question, Quote: a.Quote})
	}
	for _, d := range docs {
		if d.Quote != "" && d.Name != "Resume" {
			out = append(out, domain.Reason{Reason: "The posting names the " + strings.ToLower(d.Name), Quote: d.Quote})
		}
	}
	return out
}

package domain

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// The Alumni list is grown by code: a LinkedIn people search on the laptop reads the result cards, and the rules
// below decide, from what each card literally says, who is a working VIT alumnus. No model is involved, so
// nobody is invented: a person is listed only when their own card shows a company, and a club or a batch only
// when their own headline shows one.

// AlumniQueries are the people searches the routine rotates through, kept in one place.
var AlumniQueries = []string{
	"VIT Vellore software engineer",
	"VIT Vellore SDE",
	"VIT Vellore machine learning engineer",
	"ACM VIT",
	"Vellore Institute of Technology backend engineer",
}

// Limits that protect the owner's LinkedIn account: a run is at most 3 searches of 2 result pages each.
const (
	MaxPeopleQueries = 3
	MaxPeoplePages   = 2
	DefaultPeopleMax = 40
	maxPeopleMax     = 100
)

// LinkedInPeople is a task that searches LinkedIn people for VIT alumni and hands the new ones back as rows of
// the Alumni list. It only reads.
type LinkedInPeople struct {
	Queries   []string `json:"queries"` // empty: today's share of AlumniQueries
	Max       int      `json:"max"`     // most people to hand back
	Workspace int      `json:"workspace"`
}

// ParseLinkedInPeople reads the payload's "linkedin_people" object; nil when the task has none.
func ParseLinkedInPeople(payload json.RawMessage) (*LinkedInPeople, error) {
	var p struct {
		People *LinkedInPeople `json:"linkedin_people"`
	}
	if len(payload) == 0 || json.Unmarshal(payload, &p) != nil || p.People == nil {
		return nil, nil
	}
	s := p.People
	if s.Max == 0 {
		s.Max = DefaultPeopleMax
	}
	if s.Max < 1 || s.Max > maxPeopleMax {
		return nil, fmt.Errorf("%w: a people search hands back 1 to %d people, got %d", ErrValidation, maxPeopleMax, s.Max)
	}
	if len(s.Queries) > MaxPeopleQueries {
		return nil, fmt.Errorf("%w: a people search runs at most %d queries, got %d", ErrValidation, MaxPeopleQueries, len(s.Queries))
	}
	for i, q := range s.Queries {
		if s.Queries[i] = strings.Join(strings.Fields(q), " "); s.Queries[i] == "" || len(s.Queries[i]) > 100 {
			return nil, fmt.Errorf("%w: each query is 1 to 100 characters", ErrValidation)
		}
	}
	if s.Workspace < 0 || s.Workspace > 10 {
		return nil, fmt.Errorf("%w: workspace must be 0 to 10, got %d", ErrValidation, s.Workspace)
	}
	return s, nil
}

// ApprovalActionType is the approval a host asks for when the owner has set LinkedIn to "ask".
func (s *LinkedInPeople) ApprovalActionType() string { return "host_action:browser:read:linkedin.com" }

// Summary describes the search to the owner.
func (s *LinkedInPeople) Summary() string {
	return "Search LinkedIn people for VIT alumni and add working engineers to your Alumni list (read only: nobody is messaged or followed)"
}

// QueriesFor is what a run searches: the task's own queries, else three of AlumniQueries, starting somewhere
// different each day so the daily runs do not repeat one search.
func (s *LinkedInPeople) QueriesFor(day time.Time) []string {
	if len(s.Queries) > 0 {
		return s.Queries
	}
	var out []string
	for i := 0; i < MaxPeopleQueries; i++ {
		out = append(out, AlumniQueries[(day.YearDay()+i)%len(AlumniQueries)])
	}
	return out
}

// PersonHit is one people-search result card as the page showed it.
type PersonHit struct {
	Name     string `json:"name"`
	Headline string `json:"headline"`
	URL      string `json:"url"`
	Location string `json:"location"`
	Snippet  string `json:"snippet"` // the card's other lines, such as "Current: ..." or "Past: ..."
}

// ProfileURL turns a link found on a result card into the profile's canonical address; false when it is not a
// LinkedIn profile.
func ProfileURL(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.HasSuffix(strings.ToLower(u.Hostname()), "linkedin.com") {
		return "", false
	}
	parts := strings.Split(strings.Trim(u.EscapedPath(), "/"), "/")
	if len(parts) < 2 || parts[0] != "in" || parts[1] == "" {
		return "", false
	}
	// A search card sometimes links a session-only id ("ACoAA...") instead of the public handle; it is no link the
	// owner could use later. A very long handle would also trip the list's credential check (PlaybookLooksSafe) and
	// cost the whole table, so such a person is left out.
	if strings.HasPrefix(parts[1], "ACo") || len(parts[1]) > 31 {
		return "", false
	}
	return "https://www.linkedin.com/in/" + parts[1] + "/", true
}

// AlumniPerson is one person who passed every rule.
type AlumniPerson struct {
	Name, Batch, Company, Role, Club, LinkedIn string
}

// AlumniSearchStats says what happened to the cards a search read.
type AlumniSearchStats struct {
	Seen, Kept                                                     int
	NoProfile, NoCompany, Recruiters, Students, NotVIT, Duplicates int
}

// Report is the stats in one line for the owner.
func (s AlumniSearchStats) Report() string {
	return fmt.Sprintf("%d people read: %d kept; skipped %d with no company in their headline, %d recruiters or HR, %d students or interns, %d with nothing showing VIT, %d with no profile link, %d already seen this run.",
		s.Seen, s.Kept, s.NoCompany, s.Recruiters, s.Students, s.NotVIT, s.NoProfile, s.Duplicates)
}

var (
	headSplit = regexp.MustCompile(`\s*[|•·]\s*|\s+[–—-]\s+`)
	atSign    = regexp.MustCompile(`^(.+?)\s*@\s*(.+)$`)
	atWord    = regexp.MustCompile(`(?i)^(.+?)\s+at\s+(.+)$`)
	coCut     = regexp.MustCompile(`(?i)\s*\(|,|\s+(ex|previously|formerly)[-\s]`)
	notACo    = regexp.MustCompile(`(?i)^(self[- ]?employed|freelanc\w*|stealth.*|looking.*|seeking.*|open to.*|available.*|unemployed|currently.*|building.*|vit\b.*|vellore.*|.*\b(university|college|institute|school|acm|ieee|club|chapter|society|fest)\b.*)$`)
	// engRole is a job the owner can ask about a referral for; club posts ("Security Lead", "co-leader") are not.
	engRole     = regexp.MustCompile(`(?i)\b(engineer\w*|sde\w*|swe|developer|dev|programmer|scientist|architect|devops|sre|mle|ml|ai|data|backend|frontend|full[- ]?stack|software|technical lead|tech lead|member of technical staff|mts)\b`)
	recruiter   = regexp.MustCompile(`(?i)\b(recruit\w*|talent|hr|human resources|staffing|headhunter|sourcer|campus (hiring|relations)|people (partner|operations|ops))\b`)
	studentAny  = regexp.MustCompile(`(?i)\b(student|students|pursuing|aspiring|fresher|open to work|seeking|looking for|final[- ]year|pre[- ]final|undergrad\w*)\b`)
	studentRole = regexp.MustCompile(`(?i)\b(intern|interns|internship|trainee|apprentice)\b`)
	vitRe       = regexp.MustCompile(`\bVIT\b|(?i:\bVITians?\b|Vellore Institute)`)
	otherVIT    = regexp.MustCompile(`(?i)\bVIT[\s-]*(Bhopal|AP|Chennai|Amaravati|Pune|Mumbai|Nagpur)\b`)
	vellore     = regexp.MustCompile(`(?i)\bVellore\b`)
	acmRe       = regexp.MustCompile(`\bACM\b`)
	yearApos    = regexp.MustCompile(`['’‘](\d{2})\b`)
	yearFull    = regexp.MustCompile(`\b(20[0-3]\d)\b`)
)

// RoleAndCompany reads a headline such as "SDE-2 @ Razorpay | VIT '22 | ACM" or "Software Engineer at Google".
// ok is false when no company can be read: the headline's own words are all this uses.
func RoleAndCompany(headline string) (role, company string, ok bool) {
	for _, seg := range headSplit.Split(headline, -1) {
		seg = strings.TrimSpace(seg)
		m := atSign.FindStringSubmatch(seg)
		if m == nil {
			m = atWord.FindStringSubmatch(seg)
		}
		if m == nil {
			continue
		}
		role = strings.Trim(strings.TrimSpace(m[1]), ".,;:-–—")
		company = strings.TrimSpace(coCut.Split(m[2], 2)[0])
		company = strings.Trim(company, ".,;:-–— ")
		if role == "" || company == "" || len(role) > 60 || len(company) > 60 || notACo.MatchString(company) {
			return "", "", false
		}
		return role, company, true
	}
	return "", "", false
}

// BatchOf reads a graduation year the headline shows ("'22", "2022"); "" when it shows none.
func BatchOf(headline string) string {
	if m := yearApos.FindStringSubmatch(headline); m != nil {
		return "20" + m[1]
	}
	if m := yearFull.FindStringSubmatch(headline); m != nil {
		return m[1]
	}
	return ""
}

// AlumniFromHit applies the rules to one card. When the person does not qualify, skip says why ("profile",
// "company", "recruiter", "student" or "vit").
func AlumniFromHit(h PersonHit, today time.Time) (p AlumniPerson, skip string) {
	link, ok := ProfileURL(h.URL)
	name := strings.Join(strings.Fields(strings.ReplaceAll(h.Name, "|", " ")), " ")
	if !ok || name == "" || strings.EqualFold(name, "LinkedIn Member") {
		return p, "profile"
	}
	if recruiter.MatchString(h.Headline) {
		return p, "recruiter"
	}
	batch := BatchOf(h.Headline)
	if batch != "" && batch > fmt.Sprint(today.Year()) {
		return p, "student" // a batch that has not graduated yet
	}
	if studentAny.MatchString(h.Headline) {
		return p, "student"
	}
	role, company, ok := RoleAndCompany(h.Headline)
	if !ok {
		return p, "company"
	}
	if studentRole.MatchString(role) {
		return p, "student"
	}
	if strings.HasPrefix(role, "@") || !engRole.MatchString(role) {
		return p, "company" // not an engineering job: a club post, a team name, or a misread headline
	}
	evidence := h.Headline + "\n" + h.Snippet
	if !vitRe.MatchString(evidence) || otherVIT.MatchString(evidence) && !vellore.MatchString(evidence) {
		return p, "vit"
	}
	club := ""
	if acmRe.MatchString(h.Headline) {
		club = "ACM VIT"
	}
	return AlumniPerson{Name: name, Batch: batch, Company: company, Role: role, Club: club, LinkedIn: link}, ""
}

func cell(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "|", "/")), " ")
}

// AlumniRows applies the rules to the cards and returns the Alumni table of the people who passed, at most max,
// one per LinkedIn profile, in the exact shape of AlumniHeader. table is "" when nobody passed. The server merges
// the table into the list and drops anyone already on it (MergeAlumni).
func AlumniRows(hits []PersonHit, today time.Time, max int) (table string, st AlumniSearchStats) {
	var rows []string
	seen := map[string]bool{}
	for _, h := range hits {
		st.Seen++
		p, skip := AlumniFromHit(h, today)
		switch skip {
		case "profile":
			st.NoProfile++
			continue
		case "company":
			st.NoCompany++
			continue
		case "recruiter":
			st.Recruiters++
			continue
		case "student":
			st.Students++
			continue
		case "vit":
			st.NotVIT++
			continue
		}
		if len(rows) >= max {
			break
		}
		if key := strings.ToLower(p.LinkedIn); seen[key] {
			st.Duplicates++
			continue
		} else {
			seen[key] = true
		}
		rows = append(rows, "| "+strings.Join([]string{cell(p.Name), p.Batch, cell(p.Company), cell(p.Role), p.Club, p.LinkedIn, "", "not contacted", today.Format("2006-01-02")}, " | ")+" |")
		st.Kept++
	}
	if len(rows) == 0 {
		return "", st
	}
	return AlumniHeader + "\n" + strings.Join(rows, "\n"), st
}

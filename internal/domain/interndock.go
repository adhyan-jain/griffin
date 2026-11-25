package domain

import (
	"html"
	"regexp"
	"strings"
	"time"
)

// InternDockBase is where Griffin reads real, dated internship postings.
const InternDockBase = "https://www.interndock.com"

// InternDockPages are the InternDock collections the daily sync reads.
var InternDockPages = []string{"/internships/india", "/internships/fields/software-engineering", "/internships/fields/data-science-ml", "/internships/remote"}

// InternDockSyncTitle is the routine that syncs InternDock into the Internships list, in code, with no model.
const InternDockSyncTitle = "INTERNDOCK SYNC"

// IsInternDockSync reports whether a task is the InternDock sync.
func IsInternDockSync(title string) bool {
	return strings.Contains(strings.ToUpper(title), InternDockSyncTitle)
}

// Posting is one internship as InternDock lists it.
type Posting struct {
	Title, Company, Location, Pay, Link string
	Posted                              time.Time
	// Source is the site the posting was read from (internshala, unstop, interndock).
	Source string
}

var (
	cardRe = regexp.MustCompile(`(?s)<a[^>]+href="(/internships/[a-z0-9-]+/[a-z0-9-]{20,})"[^>]*>(.*?)</a>`)
	textRe = regexp.MustCompile(`>([^<>]+)<`)
	payRe  = regexp.MustCompile(`[$₹£€]|per hour|/hr|/mo|per month`)
)

// ParseInternDock reads the role cards on an InternDock collection page.
func ParseInternDock(page string) []Posting {
	var out []Posting
	for _, m := range cardRe.FindAllStringSubmatch(page, -1) {
		var nodes []string
		for _, t := range textRe.FindAllStringSubmatch(m[2], -1) {
			if s := strings.TrimSpace(html.UnescapeString(t[1])); s != "" {
				nodes = append(nodes, s)
			}
		}
		if len(nodes) < 3 {
			continue
		}
		// Cards that carry a company-logo placeholder start with its initials ("H" for HP): they are not the title.
		if len(nodes) >= 4 && isLogoInitials(nodes[0], nodes[2]) {
			nodes = nodes[1:]
		}
		p := Posting{Title: nodes[0], Company: nodes[1], Location: nodes[2], Link: InternDockBase + m[1], Source: "interndock"}
		for _, n := range nodes[3:] {
			switch {
			case strings.HasPrefix(n, "Posted "):
				p.Posted, _ = time.Parse("Jan 2, 2006", strings.TrimPrefix(n, "Posted "))
			case payRe.MatchString(n):
				p.Pay = n
			case strings.EqualFold(n, "remote"):
				p.Location += " (Remote)"
			}
		}
		out = append(out, p)
	}
	return out
}

// isLogoInitials reports whether mark is the placeholder InternDock draws instead of a logo: the company's first
// letter or the first letters of its words ("H" or "LM" for "Lockheed Martin"), never a title.
func isLogoInitials(mark, company string) bool {
	if len(mark) == 0 || len(mark) > 3 || mark != strings.ToUpper(mark) {
		return false
	}
	company = strings.ToUpper(company)
	initials := ""
	for _, w := range strings.Fields(company) {
		initials += w[:1]
	}
	return strings.HasPrefix(company, mark) || strings.HasPrefix(initials, mark)
}

var (
	fitRole  = regexp.MustCompile(`(?i)software|backend|back-end|full[- ]?stack|platform|infrastructure|distributed|machine learning|\bml\b|\bai\b|artificial intelligence|\bllm|generative|data scien|deep learning|computer vision|\bnlp\b|developer|\bsde\b|\bswe\b|engineer|\bpython|\bnode\.?js|\bdjango|\bjava\b|\bgolang|\bjavascript|\btypescript|\bflask|\bfastapi|spring boot`)
	notMyJob = regexp.MustCompile(`(?i)trainer|subject matter|\bsme\b|tutor|mentor|teach|front[- ]?end|angular|ui/ux|\bui\b|game|wordpress|shopify|test|\bqa\b|quality|support|sales|marketing|analyst|account|\bhr\b|recruit|design|mechanical|civil|electrical|hardware|embedded|manufactur|supply|operations|finance|legal|apprentice|founder'?s office|content|business`)
)

// FitsOwner reports whether a posting is a role the owner takes (software, backend, AI of any sort) in a place the
// owner can work (India, or remote outside the US and UK, which usually need local work authorization).
func FitsOwner(p Posting) bool {
	if !fitRole.MatchString(p.Title) || notMyJob.MatchString(p.Title) {
		return false
	}
	return InIndiaOrWorldwide(p.Location)
}

var (
	indiaPlace     = regexp.MustCompile(`(?i)\b(india|bengaluru|bangalore|mumbai|delhi|new delhi|ncr|gurgaon|gurugram|noida|hyderabad|pune|chennai|kolkata|ahmedabad|vellore|jaipur|kochi|chandigarh|indore|coimbatore)\b`)
	anywhereRemote = regexp.MustCompile(`(?i)\b(anywhere|worldwide|global|globally|apac|asia)\b`)
)

// InIndiaOrWorldwide reports whether the owner can take a job in this location: somewhere in India, or remote
// open to anyone. Remote roles tied to another country ("New York (Remote)", "Remote - US") need local work
// authorization, so they do not count.
func InIndiaOrWorldwide(loc string) bool {
	if indiaPlace.MatchString(loc) {
		return true
	}
	return strings.Contains(strings.ToLower(loc), "remote") && anywhereRemote.MatchString(loc)
}

// ListingRow writes a posting as a row of a list with the given header, filling the columns it knows.
func ListingRow(header string, p Posting) string {
	var cells []string
	for _, h := range splitRow(header) {
		v := "-"
		switch hl := strings.ToLower(h); {
		case strings.Contains(hl, "tier"):
			v = "Tier A"
		case strings.Contains(hl, "company"):
			v = p.Company
		case strings.Contains(hl, "role") || strings.Contains(hl, "title"):
			v = p.Title
		case strings.Contains(hl, "location") || strings.Contains(hl, "mode"):
			v = p.Location
		case strings.Contains(hl, "stipend") || strings.Contains(hl, "pay"):
			if p.Pay != "" {
				v = p.Pay
			}
		case strings.Contains(hl, "posted"):
			if !p.Posted.IsZero() {
				v = p.Posted.Format("2006-01-02")
			}
		case strings.Contains(hl, "source") || strings.Contains(hl, "hm"):
			v = "InternDock"
		case strings.Contains(hl, "link") || strings.Contains(hl, "apply") || strings.Contains(hl, "url"):
			v = p.Link
		case hl == "status":
			v = "not_applied"
		}
		cells = append(cells, strings.ReplaceAll(v, "|", "/"))
	}
	return "| " + strings.Join(cells, " | ") + " |"
}

// InternshipsHeader is the Internships list head used when there is no list yet.
const InternshipsHeader = "| Tier | Company | Role | Mode & Location | Stipend | Posted | Source | Application Link | Status |\n|---|---|---|---|---|---|---|---|---|"

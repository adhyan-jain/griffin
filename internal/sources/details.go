package sources

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// The aggregators' listing pages carry no description. A listing's own detail page does: the plain text it states
// (what the work is, who may apply, the skills, the perks, the deadline) is read here, by code, so the assessment
// has real words to quote. Nothing is added or guessed: a section the page does not have is left out.

// Detail is what one listing's detail page says.
type Detail struct {
	// Description is plain text with the page's own section headings; never empty in a successful read.
	Description string
	// Deadline is the application deadline when the page states one.
	Deadline *time.Time
}

// ErrNoDetail means the page was read but none of the sections a detail page has could be found in it.
var ErrNoDetail = errors.New("no description found on the detail page")

var ist = time.FixedZone("IST", 5*3600+30*60)

// DetailSource is the source name under which detail reads are recorded for a listing's source.
func DetailSource(source string) string { return source + "_detail" }

// HasDetail reports whether detail pages are read for this source.
func HasDetail(source string) bool { return source == Internshala || source == Unstop }

var unstopID = regexp.MustCompile(`-(\d+)/?$`)

// DetailURL is the address to read a listing's detail from: Internshala's page is the listing's own link; Unstop's is
// the public opportunity JSON for the id at the end of the link. ok is false for any other link.
func DetailURL(source, link string) (string, bool) {
	link = strings.TrimSpace(link)
	switch source {
	case Internshala:
		if strings.HasPrefix(link, internshalaBase+"/internship/detail/") {
			return link, true
		}
	case Unstop:
		path := link
		if i := strings.IndexAny(path, "?#"); i >= 0 {
			path = path[:i]
		}
		if strings.HasPrefix(link, "https://unstop.com/") {
			if m := unstopID.FindStringSubmatch(path); m != nil {
				return "https://unstop.com/api/public/competition/" + m[1], true
			}
		}
	}
	return "", false
}

// FetchDetail reads one listing's detail page and parses it.
func FetchDetail(ctx context.Context, get Get, source, link string) (Detail, error) {
	if get == nil {
		get = HTTPGet
	}
	u, ok := DetailURL(source, link)
	if !ok {
		return Detail{}, fmt.Errorf("no detail page for %q", link)
	}
	body, err := get(ctx, u)
	if err != nil {
		return Detail{}, err
	}
	if source == Unstop {
		return ParseUnstopDetail(body)
	}
	return ParseInternshalaDetail(body)
}

// ---- Internshala ----

var applyBy = regexp.MustCompile(`^(\d{1,2}) ([A-Z][a-z]{2})'? ?(\d{2})$`)

// ParseInternshalaDetail reads an Internshala internship detail page: About the internship, Skill(s) required, Who
// can apply, Perks, Number of openings, start date, duration, stipend, Apply By, Posted, and any certificate or
// pre-placement-offer mention.
func ParseInternshalaDetail(page []byte) (Detail, error) {
	doc, err := html.Parse(bytes.NewReader(page))
	if err != nil {
		return Detail{}, fmt.Errorf("unreadable detail page: %w", err)
	}
	body := findFirst(doc, func(n *html.Node) bool { return hasClass(n, "internship_details") })
	if body == nil {
		return Detail{}, ErrNoDetail
	}
	var sections []string
	add := func(heading, text string) {
		if text = strings.TrimSpace(text); text != "" {
			sections = append(sections, heading+"\n"+text)
		}
	}
	var about, who, perks string
	if h := findFirst(body, func(n *html.Node) bool { return hasClass(n, "about_heading") }); h != nil {
		if t := nextElement(h); t != nil {
			about = blockText(t)
		}
	}
	add("About the internship", about)
	skills := tabs(body, "skills_heading")
	add("Skill(s) required", strings.Join(skills, ", "))
	if w := findFirst(body, func(n *html.Node) bool { return hasClass(n, "who_can_apply") }); w != nil {
		who = blockText(w)
	}
	add("Who can apply", who)
	perkList := tabs(body, "perks_heading")
	perks = strings.Join(perkList, ", ")
	add("Perks", perks)
	if h := findFirst(body, func(n *html.Node) bool {
		return hasClass(n, "section_heading") && strings.EqualFold(collapse(textOf(n)), "Number of openings")
	}); h != nil {
		if t := nextElement(h); t != nil {
			add("Number of openings", collapse(textOf(t)))
		}
	}
	if len(sections) == 0 {
		return Detail{}, ErrNoDetail
	}

	// The facts line by line, in the page's own words.
	var facts []string
	fact := func(label, v string) {
		if v = strings.TrimSpace(v); v != "" {
			facts = append(facts, label+": "+v)
		}
	}
	items := map[string]string{}
	for _, it := range findAll(doc, func(n *html.Node) bool { return hasClass(n, "other_detail_item") }) {
		head := findFirst(it, func(n *html.Node) bool { return hasClass(n, "item_heading") })
		val := findFirst(it, func(n *html.Node) bool { return hasClass(n, "item_body") })
		if head == nil || val == nil {
			continue
		}
		label := strings.ToLower(collapse(textOf(head)))
		if _, seen := items[label]; seen {
			continue // the first is this internship's; later ones belong to cards for other internships
		}
		text := collapse(textOf(val))
		if sp := findFirst(val, func(n *html.Node) bool { return hasClass(n, "start_immediately_desktop") }); sp != nil {
			text = collapse(textOf(sp))
		}
		items[label] = text
	}
	fact("Start date", items["start date"])
	fact("Duration", items["duration"])
	fact("Stipend", items["stipend"])
	fact("Apply by", items["apply by"])
	ld := jobPostingLD(doc)
	posted := ld.DatePosted
	if posted == "" {
		if st := findFirst(doc, func(n *html.Node) bool {
			return hasClass(n, "status") && strings.HasPrefix(collapse(textOf(n)), "Posted ")
		}); st != nil {
			posted = strings.TrimPrefix(collapse(textOf(st)), "Posted ")
		}
	}
	fact("Posted", posted)
	var labels []string
	if l := findFirst(doc, func(n *html.Node) bool { return hasClass(n, "other_label_container") }); l != nil {
		for _, st := range findAll(l, func(n *html.Node) bool { return hasClass(n, "status") }) {
			if t := collapse(textOf(st)); t != "" {
				labels = append(labels, t)
			}
		}
	}
	fact("Listed as", strings.Join(labels, ", "))
	fact("Certificate", mention(perkList, about+"\n"+who, "certificate"))
	fact("Pre-placement offer (PPO)", ppoMention(labels, perkList, about+"\n"+who))
	if len(facts) > 0 {
		sections = append(sections, "Key facts\n"+strings.Join(facts, "\n"))
	}

	d := Detail{Description: strings.Join(sections, "\n\n")}
	switch {
	case ld.ValidThrough != "":
		d.Deadline = parseDeadline(ld.ValidThrough)
	case items["apply by"] != "":
		if m := applyBy.FindStringSubmatch(items["apply by"]); m != nil {
			if t, err := time.ParseInLocation("2 Jan 06 15:04:05", m[1]+" "+m[2]+" "+m[3]+" 23:59:59", ist); err == nil {
				d.Deadline = &t
			}
		}
	}
	return d, nil
}

// jobPosting is the schema.org JobPosting an Internshala detail page embeds.
type jobPosting struct {
	DatePosted   string `json:"datePosted"`
	ValidThrough string `json:"validThrough"`
}

func jobPostingLD(doc *html.Node) jobPosting {
	for _, s := range findAll(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "script" && attr(n, "type") == "application/ld+json"
	}) {
		var p struct {
			Type string `json:"@type"`
			jobPosting
		}
		if json.Unmarshal([]byte(textOf(s)), &p) == nil && p.Type == "JobPosting" {
			return p.jobPosting
		}
	}
	return jobPosting{}
}

// parseDeadline reads a date or date-time. A bare date is the end of that day; no zone means India time.
func parseDeadline(s string) *time.Time {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
		if t, err := time.ParseInLocation(layout, s, ist); err == nil {
			return &t
		}
	}
	if t, err := time.ParseInLocation("2006-01-02", s, ist); err == nil {
		t = t.Add(24*time.Hour - time.Second)
		return &t
	}
	return nil
}

// tabs is the round tabs (skills, perks) under the heading that carries the class.
func tabs(body *html.Node, headingClass string) []string {
	h := findFirst(body, func(n *html.Node) bool { return hasClass(n, headingClass) })
	if h == nil {
		return nil
	}
	box := nextElement(h)
	if box == nil {
		return nil
	}
	var out []string
	for _, t := range findAll(box, func(n *html.Node) bool { return hasClass(n, "round_tabs") }) {
		if s := collapse(textOf(t)); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// mention says where a word is stated: in the perks list, or in the description text (with the sentence it is in).
func mention(perks []string, text, word string) string {
	for _, p := range perks {
		if strings.Contains(strings.ToLower(p), word) {
			return "yes, listed under Perks (" + p + ")"
		}
	}
	if s := sentenceWith(text, regexp.MustCompile(`(?i)\b`+word+`\b`)); s != "" {
		return "mentioned in the description: \"" + s + "\""
	}
	return ""
}

var ppoWord = regexp.MustCompile(`(?i)\bPPO\b|pre[- ]placement|job offer`)

func ppoMention(labels, perks []string, text string) string {
	for _, l := range labels {
		if ppoWord.MatchString(l) {
			return "yes, the listing is labelled \"" + l + "\""
		}
	}
	for _, p := range perks {
		if ppoWord.MatchString(p) {
			return "yes, listed under Perks (" + p + ")"
		}
	}
	if s := sentenceWith(text, ppoWord); s != "" {
		return "mentioned in the description: \"" + s + "\""
	}
	return ""
}

// sentenceWith is the sentence (or line) of text that first matches re, at most 200 characters.
func sentenceWith(text string, re *regexp.Regexp) string {
	loc := re.FindStringIndex(text)
	if loc == nil {
		return ""
	}
	start := strings.LastIndexAny(text[:loc[0]], ".!?\n") + 1
	end := loc[1]
	if i := strings.IndexAny(text[end:], ".!?\n"); i >= 0 {
		end += i + 1
	} else {
		end = len(text)
	}
	return capRunes(collapse(text[start:end]), 200)
}

// ---- Unstop ----

type unstopJob struct {
	Type       string          `json:"type"`
	Timing     string          `json:"timing"`
	PaidUnpaid string          `json:"paid_unpaid"`
	PayIn      string          `json:"pay_in"`
	Min        *float64        `json:"min_salary"`
	Max        *float64        `json:"max_salary"`
	Hidden     bool            `json:"not_disclosed"`
	Openings   json.RawMessage `json:"openings"`
	Duration   string          `json:"internship_duration"`
	Perks      []struct {
		Text  string `json:"text"`
		Label string `json:"label"`
	} `json:"perks"`
	PPOIntern json.RawMessage `json:"pre_placement_internship"`
	PPOOpp    json.RawMessage `json:"pre_placement_opportunity"`
}

type unstopSkill struct {
	Skill     string `json:"skill"`
	SkillName string `json:"skill_name"`
}

type unstopOpp struct {
	Details  string        `json:"details"`
	EndDate  string        `json:"end_date"`
	JobSnake *unstopJob    `json:"job_detail"`
	JobCamel *unstopJob    `json:"jobDetail"`
	Skills   []unstopSkill `json:"skills"`
	Required []unstopSkill `json:"required_skills"`
	Regn     *struct {
		Start       string          `json:"start_regn_dt"`
		End         string          `json:"end_regn_dt"`
		Eligibility json.RawMessage `json:"eligibility"`
	} `json:"regnRequirements"`
}

// ParseUnstopDetail reads Unstop's public opportunity JSON (https://unstop.com/api/public/competition/<id>): the
// details text, the eligibility, the registration window, the job details and the skills.
func ParseUnstopDetail(body []byte) (Detail, error) {
	var r struct {
		Data struct {
			Competition *unstopOpp `json:"competition"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return Detail{}, fmt.Errorf("unreadable Unstop detail: %w", err)
	}
	o := r.Data.Competition
	if o == nil {
		return Detail{}, ErrNoDetail
	}
	var sections []string
	add := func(heading, text string) {
		if text = strings.TrimSpace(text); text != "" {
			sections = append(sections, heading+"\n"+text)
		}
	}
	if o.Details != "" {
		if frag, err := html.Parse(strings.NewReader(o.Details)); err == nil {
			add("Details", blockText(frag))
		}
	}
	if o.Regn != nil {
		add("Eligibility", unstopEligibility(o.Regn.Eligibility))
	}
	if o.Regn != nil {
		var reg []string
		if t := parseDeadline(o.Regn.Start); t != nil {
			reg = append(reg, "Registration opened: "+t.In(ist).Format("2 Jan 2006"))
		}
		if t := parseDeadline(o.Regn.End); t != nil {
			reg = append(reg, "Registration closes: "+t.In(ist).Format("2 Jan 2006"))
		}
		add("Registration", strings.Join(reg, "\n"))
	}
	if j := firstJob(o); j != nil {
		add("Job details", unstopJobText(j))
	}
	skills := o.Skills
	if len(skills) == 0 {
		skills = o.Required
	}
	var names []string
	for _, s := range skills {
		n := s.SkillName
		if n == "" {
			n = s.Skill
		}
		if n = strings.TrimSpace(n); n != "" {
			names = append(names, n)
		}
	}
	add("Skills", strings.Join(names, ", "))
	if len(sections) == 0 {
		return Detail{}, ErrNoDetail
	}
	d := Detail{Description: strings.Join(sections, "\n\n")}
	if d.Deadline = parseDeadline(o.EndDate); d.Deadline == nil && o.Regn != nil {
		d.Deadline = parseDeadline(o.Regn.End)
	}
	return d, nil
}

func firstJob(o *unstopOpp) *unstopJob {
	if o.JobSnake != nil {
		return o.JobSnake
	}
	return o.JobCamel
}

func unstopJobText(j *unstopJob) string {
	var lines []string
	line := func(label, v string) {
		if v = strings.TrimSpace(v); v != "" {
			lines = append(lines, label+": "+v)
		}
	}
	line("Work type", map[string]string{"wfh": "Work from home", "in_office": "In office", "hybrid": "Hybrid"}[j.Type])
	line("Timing", map[string]string{"full_time": "Full time", "part_time": "Part time"}[j.Timing])
	line("Duration", j.Duration)
	var n int
	if json.Unmarshal(j.Openings, &n) == nil && n > 0 {
		line("Openings", strconv.Itoa(n))
	}
	switch {
	case j.PaidUnpaid == "unpaid":
		line("Pay", "Unpaid")
	case j.Hidden || j.Max == nil:
	default:
		per := j.PayIn
		if per == "" || per == "monthly" {
			per = "month"
		}
		if j.Min != nil && *j.Min != *j.Max {
			line("Pay", fmt.Sprintf("₹ %s - %s /%s", rupees(*j.Min), rupees(*j.Max), per))
		} else {
			line("Pay", fmt.Sprintf("₹ %s /%s", rupees(*j.Max), per))
		}
	}
	var perks []string
	for _, p := range j.Perks {
		if t := strings.TrimSpace(p.Text); t != "" {
			perks = append(perks, t)
		}
	}
	line("Perks", strings.Join(perks, ", "))
	for _, raw := range []json.RawMessage{j.PPOIntern, j.PPOOpp} {
		if s := strings.Trim(string(raw), `" `); s == "1" || s == "true" {
			line("Pre-placement offer (PPO)", "yes")
			break
		}
	}
	return strings.Join(lines, "\n")
}

// unstopEligibility reads the eligibility Unstop stores as JSON (often inside a string).
func unstopEligibility(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) != nil || s == "" {
			return ""
		}
		raw = json.RawMessage(s)
	}
	var e map[string]json.RawMessage
	if json.Unmarshal(raw, &e) != nil {
		return ""
	}
	var lines []string
	list := func(label string, key string) {
		var items []any
		if json.Unmarshal(e[key], &items) != nil {
			return
		}
		var out []string
		for _, it := range items {
			switch v := it.(type) {
			case string:
				out = append(out, v)
			case float64:
				out = append(out, strconv.Itoa(int(v)))
			}
		}
		if len(out) > 0 {
			lines = append(lines, label+": "+strings.Join(out, ", "))
		}
	}
	list("Open to", "sector")
	list("Experience", "experience")
	list("Fresher passout years", "fresherPassoutYearsSelected")
	list("Student passout years", "studentPassoutYearsSelected")
	for _, kind := range []string{"engineering", "bSchools", "arts", "medicine", "law"} {
		var items []any
		if json.Unmarshal(e[kind], &items) != nil {
			continue
		}
		var courses []string
		for _, it := range items {
			switch v := it.(type) {
			case string:
				courses = append(courses, v)
			case map[string]any:
				if c, _ := v["course"].(string); c != "" {
					courses = append(courses, c)
				}
			}
		}
		if len(courses) > 0 {
			lines = append(lines, "Courses ("+kind+"): "+strings.Join(courses, ", "))
		}
	}
	return strings.Join(lines, "\n")
}

// ---- html helpers ----

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasClass(n *html.Node, class string) bool {
	if n.Type != html.ElementNode {
		return false
	}
	for _, c := range strings.Fields(attr(n, "class")) {
		if c == class {
			return true
		}
	}
	return false
}

func findAll(n *html.Node, match func(*html.Node) bool) []*html.Node {
	var out []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if match(n) {
			out = append(out, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return out
}

func findFirst(n *html.Node, match func(*html.Node) bool) *html.Node {
	if match(n) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if f := findFirst(c, match); f != nil {
			return f
		}
	}
	return nil
}

func nextElement(n *html.Node) *html.Node {
	for s := n.NextSibling; s != nil; s = s.NextSibling {
		if s.Type == html.ElementNode {
			return s
		}
	}
	return nil
}

// textOf is the text under a node, as the page's source has it.
func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

var blockTags = map[string]bool{"p": true, "div": true, "ul": true, "ol": true, "li": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true, "br": true, "tr": true}

// blockText is the text under a node with its line structure: spaces collapse, <br> and block elements break lines,
// list items start with "- ". Scripts and styles are dropped.
func blockText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			spaced := func() bool {
				return b.Len() > 0 && !strings.HasSuffix(b.String(), " ") && !strings.HasSuffix(b.String(), "\n")
			}
			f := strings.Fields(n.Data)
			if n.Data != "" && (len(f) == 0 || n.Data[0] == ' ' || n.Data[0] == '\n' || n.Data[0] == '\t') && spaced() {
				b.WriteString(" ")
			}
			b.WriteString(strings.Join(f, " "))
			if len(f) > 0 && (strings.HasSuffix(n.Data, " ") || strings.HasSuffix(n.Data, "\n") || strings.HasSuffix(n.Data, "\t")) {
				b.WriteString(" ")
			}
			return
		case html.ElementNode:
			if n.Data == "script" || n.Data == "style" {
				return
			}
			if blockTags[n.Data] {
				b.WriteString("\n")
			}
			if n.Data == "li" {
				b.WriteString("- ")
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if n.Type == html.ElementNode && blockTags[n.Data] && n.Data != "br" {
			b.WriteString("\n")
		}
	}
	walk(n)
	var lines []string
	for _, l := range strings.Split(b.String(), "\n") {
		if l = strings.TrimSpace(l); l != "" && l != "-" {
			lines = append(lines, l)
		}
	}
	return strings.Join(lines, "\n")
}

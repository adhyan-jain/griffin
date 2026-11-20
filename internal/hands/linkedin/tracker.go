package linkedin

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/provider/gemini"
)

// Row is one internship found in a post, as it goes into the Internships list.
type Row struct {
	Company  string `json:"company"`
	Role     string `json:"role"`
	Location string `json:"location"`
	Deadline string `json:"deadline"`
	Link     string `json:"link"`
	Type     string `json:"type"` // internship, full-time or other
	Source   string // who posted it
}

// Extractor turns a post into a row.
type Extractor interface {
	Extract(ctx context.Context, author, text string, urls []string) (Row, error)
}

const extractSystem = `You read one LinkedIn hiring post and reply with ONLY a JSON object, no other text:
{"company": "", "role": "", "location": "", "deadline": "", "link": "", "type": ""}
company and role are the hiring company and the job title. location is city or "Remote". deadline is the apply-by date if the post says one, else "". link is the application link if the post contains one, else "". type is "internship" if the role is an internship (including "internship to full-time"), "full-time" for a permanent job, else "other". Use "" for anything the post does not say; never guess.`

// GeminiExtractor is an Extractor backed by the Gemini API.
type GeminiExtractor struct{ Client *gemini.Client }

var fenceRe = regexp.MustCompile("(?s)^```(?:json)?\\s*(.*?)\\s*```$")

// Extract implements Extractor.
func (g GeminiExtractor) Extract(ctx context.Context, author, text string, urls []string) (Row, error) {
	if len(text) > 4000 {
		text = text[:4000]
	}
	reply, err := g.Client.Chat(ctx, extractSystem, []gemini.Turn{{Role: "user", Text: "Posted by: " + author + "\nLinks in the post: " + strings.Join(urls, " ") + "\n\n" + text}})
	if err != nil {
		return Row{}, err
	}
	reply = strings.TrimSpace(reply)
	if m := fenceRe.FindStringSubmatch(reply); m != nil {
		reply = m[1]
	}
	var r Row
	if err := json.Unmarshal([]byte(reply), &r); err != nil {
		return Row{}, fmt.Errorf("could not read the extracted listing: %w", err)
	}
	r.Source = author
	if r.Link == "" && len(urls) > 0 {
		r.Link = urls[0]
	}
	return r, nil
}

// defaultHeaders is the list's shape when it does not exist yet.
var defaultHeaders = []string{"Company", "Role", "Mode & Location", "Deadline", "Application Link", "Source"}

func cells(line string) []string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func norm(s string) string {
	return strings.TrimSpace(strings.ToLower(regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(s), " ")))
}

func colFor(headers []string, needles ...string) int {
	for i, h := range headers {
		for _, n := range needles {
			if strings.Contains(strings.ToLower(h), n) {
				return i
			}
		}
	}
	return -1
}

// TableInGoal finds the shared list the server showed this task: the first table after its notice.
func TableInGoal(goal string) string {
	i := strings.Index(goal, "tracker. Every task about")
	if i < 0 {
		return ""
	}
	t, _ := domain.FirstTable(goal[i:])
	return t
}

// MergeInto adds the rows to the list's table and returns the complete table. A listing already in the
// list (same company and role) is left as it is; nothing in the list is ever dropped.
func MergeInto(existing string, rows []Row) (table string, added int) {
	var headers []string
	var data [][]string
	if existing != "" {
		lines := strings.Split(existing, "\n")
		headers = cells(lines[0])
		for _, l := range lines[2:] {
			data = append(data, cells(l))
		}
	} else {
		headers = defaultHeaders
	}
	company, role := colFor(headers, "company", "name"), colFor(headers, "role", "title", "position")
	key := func(c, r string) string { return norm(c) + "|" + norm(r) }
	have := map[string]bool{}
	for _, d := range data {
		if company >= 0 && role >= 0 && company < len(d) && role < len(d) {
			have[key(d[company], d[role])] = true
		}
	}
	for _, r := range rows {
		if r.Company == "" || r.Role == "" || have[key(r.Company, r.Role)] {
			continue
		}
		have[key(r.Company, r.Role)] = true
		row := make([]string, len(headers))
		set := func(v string, needles ...string) {
			if i := colFor(headers, needles...); i >= 0 && row[i] == "" {
				row[i] = v
			}
		}
		set(r.Company, "company", "name")
		set(r.Role, "role", "title", "position")
		set(r.Location, "location", "mode")
		set(r.Deadline, "deadline", "due")
		set(r.Link, "link", "apply", "url")
		set(r.Source, "source", "posted")
		data = append(data, row)
		added++
	}
	var b strings.Builder
	b.WriteString("| " + strings.Join(headers, " | ") + " |\n|" + strings.Repeat("---|", len(headers)))
	for _, d := range data {
		for len(d) < len(headers) {
			d = append(d, "")
		}
		b.WriteString("\n| " + strings.Join(d[:len(headers)], " | ") + " |")
	}
	return b.String(), added
}

// Answer is what the scan hands back: a short report, then the complete updated list under "## Tracker", which
// is how the server knows to replace the shared list with it.
func Answer(report, table string) string {
	return report + "\n\n" + domain.TrackerSection + "\n\n" + table
}

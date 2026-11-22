package ats

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
	"time"

	nethtml "golang.org/x/net/html"

	"github.com/griffin/core/internal/jobs"
)

// Adapter reads one board (or page, or search) of one provider.
type Adapter interface {
	Provider() string
	Read(ctx context.Context, token string) jobs.Scan
}

// Detail and page limits. A board larger than the cap is read up to the cap and reported partial, never ok.
const (
	maxPages        = 200 // listing pages per board
	defaultMaxFetch = 50  // description (detail) requests per board for providers that need one per job
)

// run tracks the pages of one scan so that its status can only be ok when every page was read.
type run struct {
	scan jobs.Scan
	ok   int
	bad  int
	errs []string
	f    *Fetcher
}

func (f *Fetcher) begin(source, target string) *run {
	return &run{f: f, scan: jobs.Scan{Source: source, Target: target, StartedAt: f.now().UTC()}}
}

func (r *run) pageOK() { r.ok++ }

func (r *run) pageFailed(err error) {
	r.bad++
	r.errs = append(r.errs, err.Error())
}

// stopped records that reading ended early on purpose (a page cap), which is a partial read.
func (r *run) stopped(msg string) {
	r.bad++
	r.errs = append(r.errs, msg)
}

func (r *run) finish() jobs.Scan {
	s := r.scan
	s.FinishedAt = r.f.now().UTC()
	s.Pages = r.ok
	switch {
	case r.ok > 0 && r.bad == 0:
		s.Status = jobs.ScanOK
	case r.ok > 0:
		s.Status = jobs.ScanPartial
	default:
		s.Status = jobs.ScanFailed
		s.Jobs = nil
		if len(r.errs) == 0 {
			r.errs = []string{"nothing was read"}
		}
	}
	errs := r.errs
	if len(errs) > 3 {
		errs = append(append([]string{}, errs[:3]...), fmt.Sprintf("and %d more", len(r.errs)-3))
	}
	s.Error = strings.Join(errs, "; ")
	return s
}

// failedScan is a scan that read nothing.
func (f *Fetcher) failedScan(source, target string, err error) jobs.Scan {
	r := f.begin(source, target)
	r.pageFailed(err)
	return r.finish()
}

func orDefault(f *Fetcher) *Fetcher {
	if f == nil {
		return NewFetcher()
	}
	return f
}

// ---- text ----------------------------------------------------------------------------------------------------

var (
	blockTags  = map[string]bool{"p": true, "div": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true, "ul": true, "ol": true, "tr": true, "table": true, "section": true, "blockquote": true, "header": true, "footer": true, "article": true}
	spaceRun   = regexp.MustCompile(`[ \t\r\n\f]+`)
	blankLines = regexp.MustCompile(`\n{3,}`)
)

// htmlToText turns a description's HTML into plain text: block tags become line breaks, list items "- " lines,
// entities are decoded and scripts and styles are dropped. Entity-escaped HTML (Greenhouse) is decoded first.
func htmlToText(s string) string {
	if !strings.Contains(s, "<") && strings.Contains(s, "&lt;") {
		s = html.UnescapeString(s)
	}
	z := nethtml.NewTokenizer(strings.NewReader(s))
	var b strings.Builder
	skip := 0
loop:
	for {
		tt := z.Next()
		switch tt {
		case nethtml.ErrorToken:
			break loop
		case nethtml.TextToken:
			if skip == 0 {
				b.WriteString(spaceRun.ReplaceAllString(string(z.Text()), " "))
			}
		case nethtml.StartTagToken, nethtml.SelfClosingTagToken:
			name, _ := z.TagName()
			switch n := string(name); {
			case n == "script" || n == "style":
				if tt == nethtml.StartTagToken {
					skip++
				}
			case n == "br":
				b.WriteString("\n")
			case n == "li":
				b.WriteString("\n- ")
			case blockTags[n]:
				b.WriteString("\n")
			}
		case nethtml.EndTagToken:
			name, _ := z.TagName()
			switch n := string(name); {
			case n == "script" || n == "style":
				if skip > 0 {
					skip--
				}
			case blockTags[n]:
				b.WriteString("\n")
			}
		}
	}
	return cleanPlain(b.String())
}

// cleanPlain normalises plain text: no-break spaces become spaces, each line is trimmed, blank runs are one blank line.
func cleanPlain(s string) string {
	s = strings.NewReplacer(" ", " ", "\r\n", "\n", "\r", "\n", "​", "").Replace(s)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(spaceRun.ReplaceAllString(l, " "))
	}
	return strings.TrimSpace(blankLines.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}

// joinText joins non-empty text blocks with a blank line.
func joinText(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "\n\n")
}

// ---- dates ---------------------------------------------------------------------------------------------------

var timeLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05.000Z0700",
	"2006-01-02T15:04:05Z0700",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05 MST", // Recruitee "2026-11-18 16:12:44 UTC"
	"2006-01-02 15:04:05",
	"2006-01-02",
}

// parseTime reads the timestamp formats the providers use. "" or an unknown format is nil: nothing is guessed.
func parseTime(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	for _, l := range timeLayouts {
		if t, err := time.Parse(l, s); err == nil {
			t = t.UTC()
			return &t
		}
	}
	return nil
}

// millisTime reads a millisecond epoch (Lever createdAt). Zero or negative is nil.
func millisTime(ms int64) *time.Time {
	if ms <= 0 {
		return nil
	}
	t := time.UnixMilli(ms).UTC()
	return &t
}

var workdayPosted = regexp.MustCompile(`(?i)^\s*posted\s+(today|yesterday|(\d+)(\+)?\s+days?\s+ago)\s*$`)

// ParseWorkdayPosted turns a Workday list label ("Posted Today", "Posted 3 Days Ago") into the day it counts back
// to from fetched. "Posted 30+ Days Ago" only says "at least 30", so it gives nil, as does any other text.
func ParseWorkdayPosted(label string, fetched time.Time) *time.Time {
	m := workdayPosted.FindStringSubmatch(label)
	if m == nil {
		return nil
	}
	days := 0
	switch strings.ToLower(m[1]) {
	case "today":
	case "yesterday":
		days = 1
	default:
		if m[3] != "" {
			return nil
		}
		n, err := strconv.Atoi(m[2])
		if err != nil {
			return nil
		}
		days = n
	}
	f := fetched.UTC()
	t := time.Date(f.Year(), f.Month(), f.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -days)
	return &t
}

// ---- small helpers -------------------------------------------------------------------------------------------

// flexString reads a JSON string or number as text.
type flexString string

func (s *flexString) UnmarshalJSON(b []byte) error {
	t := strings.TrimSpace(string(b))
	switch {
	case t == "null" || t == "":
		*s = ""
	case strings.HasPrefix(t, `"`):
		var v string
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		*s = flexString(strings.TrimSpace(v))
	default:
		var n json.Number
		if err := json.Unmarshal(b, &n); err != nil {
			return fmt.Errorf("not a string or number: %s", t)
		}
		*s = flexString(n.String())
	}
	return nil
}

// stringList reads one string or an array of strings (schema.org allows both).
type stringList []string

func (l *stringList) UnmarshalJSON(b []byte) error {
	var one string
	if json.Unmarshal(b, &one) == nil {
		if one = strings.TrimSpace(one); one != "" {
			*l = stringList{one}
		}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return nil // an unexpected shape is "not given", not an error for the whole board
	}
	for _, m := range many {
		if m = strings.TrimSpace(m); m != "" {
			*l = append(*l, m)
		}
	}
	return nil
}

func ptr[T any](v T) *T { return &v }

// addUnique appends non-empty values not already present.
func addUnique(list []string, vals ...string) []string {
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		dup := false
		for _, e := range list {
			if e == v {
				dup = true
				break
			}
		}
		if !dup {
			list = append(list, v)
		}
	}
	return list
}

// moneyText joins the parts of a pay range that the source gave ("USD", "50000", "70000", "per year").
func moneyText(currency, min, max, per string) string {
	rng := min
	if max != "" && max != min {
		if min != "" {
			rng = min + " - " + max
		} else {
			rng = max
		}
	}
	if rng == "" {
		return ""
	}
	return strings.TrimSpace(strings.Join(nonEmpty(currency, rng, per), " "))
}

func nonEmpty(vals ...string) []string {
	var out []string
	for _, v := range vals {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

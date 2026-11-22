package ats

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	nethtml "golang.org/x/net/html"

	"github.com/griffin/core/internal/jobs"
)

// CareersJSONLD reads a careers page (the token is its URL) and returns every schema.org JobPosting in its
// application/ld+json blocks. A page without one is a failed read, not an empty board: the page may simply not
// publish structured data.
type CareersJSONLD struct{ F *Fetcher }

func (CareersJSONLD) Provider() string { return "careers_jsonld" }

func (c CareersJSONLD) Read(ctx context.Context, pageURL string) jobs.Scan {
	f := orDefault(c.F)
	r := f.begin("careers_jsonld", pageURL)
	if u, err := url.Parse(pageURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		r.pageFailed(fmt.Errorf("careers page %q is not an http(s) URL", pageURL))
		return r.finish()
	}
	body, err := f.Get(ctx, pageURL)
	if err != nil {
		r.pageFailed(err)
		return r.finish()
	}
	posts, problems := ParseJobPostings(string(body), pageURL, r.scan.StartedAt)
	switch {
	case len(posts) == 0 && len(problems) > 0:
		r.pageFailed(fmt.Errorf("no readable JobPosting: %s", strings.Join(problems, "; ")))
	case len(posts) == 0:
		r.pageFailed(fmt.Errorf("the page has no schema.org JobPosting JSON-LD"))
	default:
		r.pageOK()
		r.scan.Jobs = posts
		for _, p := range problems {
			r.pageFailed(fmt.Errorf("%s", p)) // a block that could not be read may have held a posting: the read is partial
		}
	}
	return r.finish()
}

// ParseJobPostings extracts the JobPosting objects from a page's JSON-LD blocks. A single object, an array and an
// @graph are all read. Blocks that are not valid JSON are named in problems.
func ParseJobPostings(page, pageURL string, fetched time.Time) (posts []jobs.RawJob, problems []string) {
	for i, block := range jsonLDBlocks(page) {
		var root any
		if err := json.Unmarshal([]byte(block), &root); err != nil {
			problems = append(problems, fmt.Sprintf("JSON-LD block %d is not valid JSON (%v)", i+1, err))
			continue
		}
		for _, node := range jsonLDNodes(root) {
			if hasType(node, "JobPosting") {
				posts = append(posts, jobFromLD(node, pageURL, fetched))
			}
		}
	}
	return posts, problems
}

func jsonLDBlocks(page string) []string {
	var out []string
	z := nethtml.NewTokenizer(strings.NewReader(page))
	inLD := false
	var cur strings.Builder
	for {
		switch z.Next() {
		case nethtml.ErrorToken:
			return out
		case nethtml.StartTagToken:
			name, hasAttr := z.TagName()
			if string(name) != "script" {
				continue
			}
			for hasAttr {
				var k, v []byte
				k, v, hasAttr = z.TagAttr()
				if string(k) == "type" && strings.Contains(strings.ToLower(string(v)), "ld+json") {
					inLD = true
					cur.Reset()
				}
			}
		case nethtml.TextToken:
			if inLD {
				cur.Write(z.Raw())
			}
		case nethtml.EndTagToken:
			if name, _ := z.TagName(); inLD && string(name) == "script" {
				if s := strings.TrimSpace(cur.String()); s != "" {
					out = append(out, s)
				}
				inLD = false
			}
		}
	}
}

// jsonLDNodes flattens a JSON-LD root: an object, an array of objects, or an object with an @graph array.
func jsonLDNodes(v any) []map[string]any {
	switch t := v.(type) {
	case []any:
		var out []map[string]any
		for _, e := range t {
			out = append(out, jsonLDNodes(e)...)
		}
		return out
	case map[string]any:
		out := []map[string]any{t}
		if g, ok := t["@graph"]; ok {
			out = append(out, jsonLDNodes(g)...)
		}
		return out
	}
	return nil
}

func hasType(n map[string]any, want string) bool {
	switch t := n["@type"].(type) {
	case string:
		return t == want || strings.HasSuffix(t, "/"+want)
	case []any:
		for _, e := range t {
			if s, ok := e.(string); ok && (s == want || strings.HasSuffix(s, "/"+want)) {
				return true
			}
		}
	}
	return false
}

// text reads a JSON-LD value that should be text: a string, a number, or an object with a name/value.
func ldText(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case map[string]any:
		for _, k := range []string{"name", "value", "@id"} {
			if s := ldText(t[k]); s != "" {
				return s
			}
		}
	}
	return ""
}

// ldID reads an identifier: a plain value or a PropertyValue whose value is the id (its name is the issuer).
func ldID(v any) string {
	if m, ok := v.(map[string]any); ok {
		for _, k := range []string{"value", "@id", "name"} {
			if s := ldText(m[k]); s != "" {
				return s
			}
		}
		return ""
	}
	return ldText(v)
}

func ldNumber(v any) string {
	switch t := v.(type) {
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case string:
		return strings.TrimSpace(t)
	}
	return ""
}

func ldList(v any) []any {
	switch t := v.(type) {
	case nil:
		return nil
	case []any:
		return t
	}
	return []any{v}
}

func jobFromLD(n map[string]any, pageURL string, fetched time.Time) jobs.RawJob {
	j := jobs.RawJob{
		Source: "careers_jsonld", SourceKind: jobs.EmployerSite,
		ExternalID:  ldID(n["identifier"]),
		Title:       ldText(n["title"]),
		URL:         firstNonEmpty(ldText(n["url"]), pageURL),
		Department:  ldText(n["occupationalCategory"]),
		Description: htmlToText(ldText(n["description"])),
		PostedAt:    parseTime(ldText(n["datePosted"])),
		Deadline:    parseTime(ldText(n["validThrough"])),
		FetchedAt:   fetched,
	}
	if j.Title == "" {
		j.Title = ldText(n["name"])
	}
	if org, ok := n["hiringOrganization"].(map[string]any); ok {
		j.EmployerName = ldText(org["name"])
		j.EmployerDomain = domainOf(firstNonEmpty(ldText(org["sameAs"]), ldText(org["url"])))
	} else {
		j.EmployerName = ldText(n["hiringOrganization"])
	}
	var types []string
	for _, e := range ldList(n["employmentType"]) {
		types = addUnique(types, ldText(e))
	}
	j.EmploymentType = strings.Join(types, ", ")
	for _, e := range ldList(n["jobLocation"]) {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		addr, ok := m["address"].(map[string]any)
		if !ok {
			j.Locations = addUnique(j.Locations, ldText(m["address"]), ldText(m["name"]))
			continue
		}
		j.Locations = addUnique(j.Locations, strings.Join(nonEmpty(ldText(addr["addressLocality"]), ldText(addr["addressRegion"]), ldText(addr["addressCountry"])), ", "))
	}
	if strings.EqualFold(ldText(n["jobLocationType"]), "TELECOMMUTE") {
		j.Remote = ptr(true)
	}
	if bs, ok := n["baseSalary"].(map[string]any); ok {
		cur := ldText(bs["currency"])
		if val, ok := bs["value"].(map[string]any); ok {
			j.Compensation = moneyText(cur, firstNonEmpty(ldNumber(val["minValue"]), ldNumber(val["value"])), ldNumber(val["maxValue"]), ldText(val["unitText"]))
		} else {
			j.Compensation = moneyText(cur, ldNumber(bs["value"]), "", "")
		}
	}
	return j
}

func domainOf(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}

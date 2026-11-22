package ats

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/griffin/core/internal/jobs"
)

// The seed list is the curated set of employers whose own boards Griffin reads from its first run: Indian product
// companies and startups, global AI and infrastructure companies that hire interns in India or remotely, and
// open-source companies. Every entry was read live by its adapter (cmd/verify_seeds) and kept only when the read was
// ok and the board was shown to be that company's. Nothing in seeds.json is a guess: a company whose board could not be
// read is not in the file.

//go:embed seeds.json
var seedsJSON []byte

// SeedKinds are the company kinds a seed may carry (the companies table's own set).
var SeedKinds = map[string]bool{"startup": true, "enterprise": true, "lab": true, "foundation": true, "unknown": true}

// Seed is one verified employer board.
type Seed struct {
	Name       string `json:"name"`
	Domain     string `json:"domain"`
	Kind       string `json:"kind"`
	Region     string `json:"region"` // "india", "global" or "remote": why the company is here
	Provider   string `json:"provider"`
	Token      string `json:"board"`       // the board's slug (not a credential)
	VerifiedOn string `json:"verified_on"` // the day the adapter read the board
	JobsSeen   int    `json:"jobs_seen"`   // postings on the board that day
	Identity   string `json:"identity"`    // how the board was shown to be this company's
}

// SeedFile is the versioned seed list.
type SeedFile struct {
	Version    int    `json:"version"`
	VerifiedOn string `json:"verified_on"`
	Seeds      []Seed `json:"seeds"`
}

// ParseSeeds reads a seed file and checks it: every seed has a name, a known provider and a token, and no board
// appears twice. A broken file is an error, never a silently shorter list.
func ParseSeeds(data []byte) (SeedFile, error) {
	var f SeedFile
	if err := json.Unmarshal(data, &f); err != nil {
		return f, fmt.Errorf("seeds: %w", err)
	}
	boards := map[string]bool{}
	for i, s := range f.Seeds {
		switch {
		case strings.TrimSpace(s.Name) == "":
			return f, fmt.Errorf("seeds: entry %d has no name", i)
		case strings.TrimSpace(s.Token) == "":
			return f, fmt.Errorf("seeds: %s has no token", s.Name)
		case !SeedProviders[s.Provider]:
			return f, fmt.Errorf("seeds: %s has the unknown provider %q", s.Name, s.Provider)
		case !SeedKinds[s.Kind]:
			return f, fmt.Errorf("seeds: %s has the unknown kind %q", s.Name, s.Kind)
		case s.VerifiedOn == "":
			return f, fmt.Errorf("seeds: %s has no verification date", s.Name)
		}
		key := s.Provider + "|" + strings.ToLower(s.Token)
		if boards[key] {
			return f, fmt.Errorf("seeds: the board %s %s is listed twice", s.Provider, s.Token)
		}
		boards[key] = true
	}
	return f, nil
}

// SeedProviders are the board providers a seed may use: the employer ATS adapters, not the aggregators.
var SeedProviders = map[string]bool{
	"greenhouse": true, "lever": true, "lever_eu": true, "ashby": true, "smartrecruiters": true,
	"workday": true, "recruitee": true, "workable": true,
}

// Seeds returns the embedded, verified seed list. It panics on a malformed file, which the seed test catches before
// any release.
func Seeds() SeedFile {
	f, err := ParseSeeds(seedsJSON)
	if err != nil {
		panic(err)
	}
	return f
}

// ---- verification ---------------------------------------------------------------------------------------------

// Try is one provider and token to read for a candidate company.
type Try struct {
	Provider string `json:"provider"`
	Token    string `json:"token"`
}

// Candidate is a company that may be seeded, with the boards to try for it in order. The first board that reads ok and
// is shown to be the company's becomes the seed.
type Candidate struct {
	Name   string `json:"name"`
	Domain string `json:"domain"`
	Kind   string `json:"kind"`
	Region string `json:"region"`
	Tries  []Try  `json:"tries"`
	// Needs are words of the company's own line of work, for a name other companies share. When a board is tied to the
	// company only by its name used in the text, at least one posting must also contain one of them.
	Needs []string `json:"needs,omitempty"`
}

// scanMentionsAny reports whether any posting's title or text contains one of the words, ignoring case.
func scanMentionsAny(scan jobs.Scan, words []string) bool {
	for _, j := range scan.Jobs {
		text := strings.ToLower(j.Title + "\n" + j.Description)
		for _, w := range words {
			if w = strings.ToLower(strings.TrimSpace(w)); w != "" && strings.Contains(text, w) {
				return true
			}
		}
	}
	return false
}

// Outcome says what happened to one try, for the verification record.
type Outcome struct {
	Try
	Status   string // the scan status, or "skipped"
	Jobs     int
	Identity string // how the board was shown to be the company's; "" when it was not
	Note     string
}

// VerifyCandidate reads each of a candidate's boards in order and returns the seed for the first one that read ok and
// belongs to the company, with the outcome of every try. A board is the company's only when its postings say so
// (see BoardIdentity); an empty board cannot be told from another company's, so it is not seeded.
func VerifyCandidate(ctx context.Context, adapters map[string]Adapter, c Candidate, day string) (Seed, bool, []Outcome) {
	var outs []Outcome
	for _, t := range c.Tries {
		o := Outcome{Try: t}
		a := adapters[t.Provider]
		if a == nil || !SeedProviders[t.Provider] {
			o.Status, o.Note = "skipped", "no adapter for "+t.Provider
			outs = append(outs, o)
			continue
		}
		scan := a.Read(ctx, t.Token)
		o.Status, o.Jobs, o.Note = string(scan.Status), len(scan.Jobs), scan.Error
		if scan.Status != jobs.ScanOK {
			outs = append(outs, o)
			continue
		}
		id := BoardIdentity(c.Name, c.Domain, scan)
		if strings.HasPrefix(id, "posting text says") && len(c.Needs) > 0 && !scanMentionsAny(scan, c.Needs) {
			// the name alone is shared with another company (Navi, Neon): the postings must also show this one's line of work
			o.Note, id = "the name matches but no posting mentions "+strings.Join(c.Needs, " or ")+", so it may be another company", ""
		} else if id == "" {
			o.Note = "no posting names this company"
			if len(scan.Jobs) == 0 {
				o.Note = "the board is empty, so it cannot be shown to be this company's"
			}
		}
		o.Identity = id
		outs = append(outs, o)
		if id == "" {
			continue
		}
		return Seed{Name: c.Name, Domain: c.Domain, Kind: c.Kind, Region: c.Region, Provider: t.Provider, Token: t.Token, VerifiedOn: day, JobsSeen: len(scan.Jobs), Identity: id}, true, outs
	}
	return Seed{}, false, outs
}

// BoardIdentity says how a read board is shown to be the named company's: the employer name its postings carry, a
// posting link on the company's own domain, or the company's domain or proper name used as a company in a posting's
// text ("At Linear we build ...", "About Render", "linear.app/careers"). "" means nothing ties the board to the
// company. The token itself is never evidence: it is the guess being checked. A bare word is not either, because many
// company names are ordinary words ("render", "linear algebra").
func BoardIdentity(name, domain string, scan jobs.Scan) string {
	terms := identityTerms(name, domain)
	dom := strings.ToLower(strings.TrimSpace(domain))
	pats := namePatterns(name, domain)
	for _, j := range scan.Jobs {
		if e := strings.ToLower(j.EmployerName); e != "" {
			for _, t := range terms {
				if containsWord(e, t) {
					return "employer name " + fmt.Sprintf("%q", j.EmployerName)
				}
			}
		}
		for _, link := range []string{j.URL, j.ApplyURL} {
			if h := hostOf(link); h != "" && dom != "" && (h == dom || strings.HasSuffix(h, "."+dom)) {
				return "posting link on " + h
			}
		}
		text := j.Title + "\n" + j.Description
		if dom != "" && containsWord(strings.ToLower(text), dom) {
			return "posting text names " + dom
		}
		for _, re := range pats {
			if m := re.FindString(text); m != "" {
				return "posting text says " + fmt.Sprintf("%q", strings.TrimSpace(m))
			}
		}
	}
	return ""
}

// namePatterns match the company's proper name used as a company: after "at", "about", "join" or "welcome to", or as
// the subject of a sentence ("Linear is", "Linear's"). They are case-sensitive, so the verb "render" and the
// adjective "linear" do not match.
func namePatterns(name, domain string) []*regexp.Regexp {
	var spellings []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if len(s) < 3 {
			return
		}
		for _, x := range spellings {
			if x == s {
				return
			}
		}
		spellings = append(spellings, s)
	}
	add(name)
	if l := domainLabel(domain); l != "" {
		add(strings.ToUpper(l[:1]) + l[1:])
		add(strings.ToUpper(l))
	}
	var out []*regexp.Regexp
	for _, sp := range spellings {
		q := regexp.QuoteMeta(sp)
		out = append(out,
			regexp.MustCompile(`(?:^|[^A-Za-z0-9])(?:At|at|About|about|Join|join|Joining|joining|Welcome to|welcome to) `+q+`(?:[^A-Za-z0-9]|$)`),
			regexp.MustCompile(`(?:^|[^A-Za-z0-9])`+q+`(?:'s| is | was | are | has | builds | helps | team )`),
		)
	}
	return out
}

func identityTerms(name, domain string) []string {
	var terms []string
	add := func(s string) {
		s = strings.ToLower(strings.TrimSpace(s))
		if s != "" {
			for _, t := range terms {
				if t == s {
					return
				}
			}
			terms = append(terms, s)
		}
	}
	add(name)
	add(domainLabel(domain))
	return terms
}

// domainLabel is the name part of a domain: "razorpay.com" gives "razorpay".
func domainLabel(domain string) string {
	d := strings.ToLower(strings.TrimSpace(domain))
	d = strings.TrimPrefix(d, "www.")
	if i := strings.Index(d, "."); i > 0 {
		return d[:i]
	}
	return d
}

func hostOf(link string) string {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}

// containsWord reports whether text holds term with non-letters or the text's edges around it.
func containsWord(text, term string) bool {
	for from := 0; ; {
		i := strings.Index(text[from:], term)
		if i < 0 {
			return false
		}
		i += from
		end := i + len(term)
		before := i == 0 || !isLetter(text[i-1])
		after := end >= len(text) || !isLetter(text[end])
		if before && after {
			return true
		}
		from = i + 1
	}
}

func isLetter(b byte) bool { return b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b >= 0x80 }

// MarshalSeeds writes a seed file in the stable form the repository keeps.
func MarshalSeeds(f SeedFile) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(f); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

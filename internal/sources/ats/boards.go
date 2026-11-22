package ats

import (
	"regexp"
	"strings"
)

// BoardRef names one ATS board: the provider (a key of Adapters) and the token its adapter reads.
type BoardRef struct {
	Provider string
	Token    string
}

type boardPattern struct {
	provider string
	re       *regexp.Regexp
	// reject names tokens that are routes, not boards
	reject map[string]bool
}

var tok = `([A-Za-z0-9][A-Za-z0-9_-]*)`

var boardPatterns = []boardPattern{
	{"greenhouse", regexp.MustCompile(`(?i)(?:job-)?boards\.(?:eu\.)?greenhouse\.io/` + tok), map[string]bool{"embed": true, "v1": true}},
	{"greenhouse", regexp.MustCompile(`(?i)boards-api\.greenhouse\.io/v1/boards/` + tok), nil},
	{"greenhouse", regexp.MustCompile(`(?i)greenhouse\.io/embed/job_board(?:/js)?\?(?:[^"'\s<>]*&)?for=` + tok), nil},
	{"lever_eu", regexp.MustCompile(`(?i)(?:jobs|api)\.eu\.lever\.co/(?:v0/postings/)?` + tok), map[string]bool{"v0": true}},
	{"lever", regexp.MustCompile(`(?i)jobs\.lever\.co/` + tok), nil},
	{"lever", regexp.MustCompile(`(?i)api\.lever\.co/v0/postings/` + tok), nil},
	{"ashby", regexp.MustCompile(`(?i)jobs\.ashbyhq\.com/` + tok), map[string]bool{"api": true}},
	{"ashby", regexp.MustCompile(`(?i)api\.ashbyhq\.com/posting-api/job-board/` + tok), nil},
	{"smartrecruiters", regexp.MustCompile(`(?i)(?:jobs|careers)\.smartrecruiters\.com/` + tok), map[string]bool{"oneclick-ui": true, "external-referrals": true}},
	{"smartrecruiters", regexp.MustCompile(`(?i)api\.smartrecruiters\.com/v1/companies/` + tok), nil},
	{"workable", regexp.MustCompile(`(?i)apply\.workable\.com/` + tok), map[string]bool{"api": true, "j": true}},
	{"workable", regexp.MustCompile(`(?i)apply\.workable\.com/api/v\d/accounts/` + tok), nil},
}

var (
	recruiteeRe = regexp.MustCompile(`(?i)\b([a-z0-9][a-z0-9-]*)\.recruitee\.com\b`)
	workdayRe   = regexp.MustCompile(`(?i)\b([a-z0-9][a-z0-9-]*)\.(wd\d+)\.myworkdayjobs\.com/(?:wday/cxs/[^/\s"'<>]+/|(?:[a-z]{2}-[A-Z]{2}/)?)([A-Za-z0-9_.-]+)`)
	localeRe    = regexp.MustCompile(`^[a-z]{2}-[A-Za-z]{2}$`)
	// not boards: shared hosts of the provider itself
	recruiteeReject = map[string]bool{"www": true, "api": true, "app": true, "support": true, "help": true, "careers": true, "blog": true, "docs": true, "status": true}
	workdayReject   = map[string]bool{"job": true, "jobs": true, "wday": true, "login": true, "details": true}
)

// DetectBoards finds the ATS boards a careers page links to or embeds: Greenhouse, Lever (and EU), Ashby,
// SmartRecruiters, Workable, Recruitee and Workday. Each board appears once, in the order it first appears.
// It reads URLs wherever they occur in the HTML (links, iframes, script sources, inline JSON).
func DetectBoards(page string) []BoardRef {
	page = strings.NewReplacer(`\/`, `/`, `&amp;`, `&`, `/`, `/`, `/`, `/`).Replace(page)
	type hit struct {
		pos int
		ref BoardRef
	}
	var hits []hit
	for _, p := range boardPatterns {
		for _, m := range p.re.FindAllStringSubmatchIndex(page, -1) {
			t := page[m[2]:m[3]]
			if p.reject[strings.ToLower(t)] {
				continue
			}
			hits = append(hits, hit{m[0], BoardRef{p.provider, t}})
		}
	}
	for _, m := range recruiteeRe.FindAllStringSubmatchIndex(page, -1) {
		if t := page[m[2]:m[3]]; !recruiteeReject[strings.ToLower(t)] {
			hits = append(hits, hit{m[0], BoardRef{"recruitee", strings.ToLower(t)}})
		}
	}
	for _, m := range workdayRe.FindAllStringSubmatchIndex(page, -1) {
		site := page[m[6]:m[7]]
		if workdayReject[strings.ToLower(site)] || localeRe.MatchString(site) {
			continue
		}
		hits = append(hits, hit{m[0], BoardRef{"workday", strings.ToLower(page[m[2]:m[3]]) + "." + strings.ToLower(page[m[4]:m[5]]) + "/" + site}})
	}
	// order by where each hit first appears in the page
	for i := 1; i < len(hits); i++ {
		for k := i; k > 0 && hits[k].pos < hits[k-1].pos; k-- {
			hits[k], hits[k-1] = hits[k-1], hits[k]
		}
	}
	var out []BoardRef
	seen := map[BoardRef]bool{}
	for _, h := range hits {
		key := BoardRef{h.ref.Provider, strings.ToLower(h.ref.Token)}
		if !seen[key] {
			seen[key] = true
			out = append(out, h.ref)
		}
	}
	return out
}

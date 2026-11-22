package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	githubAPI = "https://api.github.com"
	// HygieneRequestCap is the most GitHub requests one hygiene run makes: unauthenticated GitHub allows 60 an hour.
	HygieneRequestCap = 55
	// StaleAfter is how long a repo may go without a push before the rules suggest archiving or updating it.
	StaleAfter = 183 * 24 * time.Hour
	// TriageIssues is the open-issue count from which the rules suggest triage.
	TriageIssues = 10
)

// Repo is one public repository of the owner, with the checks made against it.
type Repo struct {
	Name, Link, Stack, License string
	Description                string
	Pushed                     time.Time
	OpenIssues                 int
	README, CI                 bool
	Checked                    bool // false when the request budget ran out before its files were looked at
}

// ParseRepos reads a user's repository list. Forks, archived and private repos are skipped.
func ParseRepos(body []byte) ([]Repo, error) {
	var r []struct {
		Name        string  `json:"name"`
		HTMLURL     string  `json:"html_url"`
		Description *string `json:"description"`
		Language    *string `json:"language"`
		Fork        bool    `json:"fork"`
		Archived    bool    `json:"archived"`
		Private     bool    `json:"private"`
		PushedAt    string  `json:"pushed_at"`
		OpenIssues  int     `json:"open_issues_count"`
		License     *struct {
			SPDX string `json:"spdx_id"`
			Name string `json:"name"`
		} `json:"license"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("sources: unreadable GitHub repository list: %w", err)
	}
	var out []Repo
	for _, x := range r {
		if x.Fork || x.Archived || x.Private || x.Name == "" {
			continue
		}
		repo := Repo{Name: x.Name, Link: x.HTMLURL, OpenIssues: x.OpenIssues}
		if x.Description != nil {
			repo.Description = strings.TrimSpace(*x.Description)
		}
		if x.Language != nil {
			repo.Stack = *x.Language
		}
		if x.License != nil {
			repo.License = x.License.SPDX
			if repo.License == "" || repo.License == "NOASSERTION" {
				repo.License = x.License.Name
			}
		}
		repo.Pushed, _ = time.Parse(time.RFC3339, x.PushedAt)
		out = append(out, repo)
	}
	return out, nil
}

// RootFiles are what a repository's root listing shows.
type RootFiles struct {
	README, License, GitHubDir bool
}

// ParseRootListing reads the contents listing of a repository's root.
func ParseRootListing(body []byte) (RootFiles, error) {
	var items []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(body, &items); err != nil {
		return RootFiles{}, fmt.Errorf("sources: unreadable GitHub contents listing: %w", err)
	}
	var f RootFiles
	for _, it := range items {
		n := strings.ToLower(it.Name)
		switch {
		case it.Type == "file" && strings.HasPrefix(n, "readme"):
			f.README = true
		case it.Type == "file" && (strings.HasPrefix(n, "license") || strings.HasPrefix(n, "licence") || strings.HasPrefix(n, "copying")):
			f.License = true
		case it.Type == "dir" && n == ".github":
			f.GitHubDir = true
		}
	}
	return f, nil
}

// HasWorkflows reports whether a .github listing holds a workflows directory.
func HasWorkflows(body []byte) bool {
	var items []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if json.Unmarshal(body, &items) != nil {
		return false
	}
	for _, it := range items {
		if it.Type == "dir" && strings.EqualFold(it.Name, "workflows") {
			return true
		}
	}
	return false
}

// ReadRepos lists the public repositories of a GitHub user and checks each one's root files, newest push first,
// within HygieneRequestCap requests. A repository the budget did not reach is left with Checked false. When GitHub
// answers 403 or 429 (its rate limit) no more requests are sent.
func ReadRepos(ctx context.Context, get Get, login string) (repos []Repo, problems []string) {
	if get == nil {
		get = HTTPGet
	}
	used := 0
	fetch := func(u string) ([]byte, error) {
		used++
		return get(ctx, u)
	}
	limited := func(err error) bool {
		return strings.Contains(err.Error(), "HTTP 403") || strings.Contains(err.Error(), "HTTP 429")
	}
	body, err := fetch(githubAPI + "/users/" + url.PathEscape(login) + "/repos?per_page=100&type=owner&sort=pushed")
	if err != nil {
		return nil, []string{"the GitHub repository list"}
	}
	repos, err = ParseRepos(body)
	if err != nil {
		return nil, []string{"the GitHub repository list"}
	}
	sort.SliceStable(repos, func(i, j int) bool { return repos[i].Pushed.After(repos[j].Pushed) })
	for i := range repos {
		r := &repos[i]
		root := githubAPI + "/repos/" + url.PathEscape(login) + "/" + url.PathEscape(r.Name) + "/contents/"
		if used >= HygieneRequestCap {
			break
		}
		body, err := fetch(root)
		if err != nil {
			if limited(err) {
				problems = append(problems, "GitHub rate limit reached, so the remaining repos were not checked")
				break
			}
			problems = append(problems, r.Name)
			continue
		}
		files, err := ParseRootListing(body)
		if err != nil {
			problems = append(problems, r.Name)
			continue
		}
		r.README = files.README
		if r.License == "" && files.License {
			r.License = "yes"
		}
		r.Checked = true
		if files.GitHubDir && used < HygieneRequestCap {
			gh, err := fetch(root + ".github")
			if err != nil {
				r.Checked = false // CI is unknown, so the row would claim something unread
				if limited(err) {
					problems = append(problems, "GitHub rate limit reached, so the remaining repos were not checked")
					break
				}
				problems = append(problems, r.Name)
				continue
			}
			r.CI = HasWorkflows(gh)
		}
	}
	return repos, problems
}

// FixNext is what to fix on a repo, generated by rules from its failed checks (most important first, at most two).
func (r Repo) FixNext(now time.Time) string {
	var fixes []string
	if !r.README {
		fixes = append(fixes, "Add a README")
	}
	if r.License == "" {
		fixes = append(fixes, "Add a LICENSE")
	}
	if r.Description == "" {
		fixes = append(fixes, "Add a description")
	}
	if !r.CI {
		fixes = append(fixes, "Add CI: no .github/workflows")
	}
	if !r.Pushed.IsZero() && now.Sub(r.Pushed) > StaleAfter {
		fixes = append(fixes, fmt.Sprintf("No push in %d months: archive or update", int(now.Sub(r.Pushed).Hours()/24/30)))
	}
	if r.OpenIssues >= TriageIssues {
		fixes = append(fixes, fmt.Sprintf("Triage %d open issues", r.OpenIssues))
	}
	switch {
	case len(fixes) == 0:
		return "Nothing: all checks pass"
	case len(fixes) > 2:
		fixes = fixes[:2]
	}
	return strings.Join(fixes, "; ")
}

// failed counts the repo's failed checks, so the repos that need the most come first.
func (r Repo) failed(now time.Time) int {
	n := 0
	for _, bad := range []bool{!r.README, r.License == "", r.Description == "", !r.CI, !r.Pushed.IsZero() && now.Sub(r.Pushed) > StaleAfter, r.OpenIssues >= TriageIssues} {
		if bad {
			n++
		}
	}
	return n
}

// HygieneListText is the Repo Hygiene tracker written from the checked repos, the ones needing the most first.
func HygieneListText(repos []Repo, now time.Time) string {
	cell := func(s string) string {
		s = strings.TrimSpace(strings.Join(strings.Fields(strings.ReplaceAll(s, "|", "/")), " "))
		if s == "" {
			return "-"
		}
		return s
	}
	yes := func(ok bool) string {
		if ok {
			return "yes"
		}
		return "no"
	}
	var checked []Repo
	for _, r := range repos {
		if r.Checked {
			checked = append(checked, r)
		}
	}
	sort.SliceStable(checked, func(i, j int) bool { return checked[i].failed(now) > checked[j].failed(now) })
	var b strings.Builder
	b.WriteString("# Repo Hygiene tracker\n\n")
	b.WriteString("| Repo | Stack | Last push | README | License | CI | Open issues | Fix next | Link |\n|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range checked {
		lic := r.License
		if lic == "" {
			lic = "no"
		}
		push := "-"
		if !r.Pushed.IsZero() {
			push = r.Pushed.Format("2006-01-02")
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %d | %s | %s |\n", cell(r.Name), cell(r.Stack), push, yes(r.README), cell(lic), yes(r.CI),
			r.OpenIssues, cell(r.FixNext(now)), cell(r.Link))
	}
	return b.String()
}

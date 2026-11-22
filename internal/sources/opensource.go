package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

// OpenSourceRepos are the projects the owner's interests point at (Go infrastructure and observability, LLM serving,
// AI tooling, vector stores). Checked live on 2026-11-22 for open, unassigned "good first issue" / "help wanted"
// issues from the last year: projects with none (grpc-go, etcd, temporal, asynq, go-redis, ...) are left out.
var OpenSourceRepos = []string{
	"open-telemetry/opentelemetry-go",
	"open-telemetry/opentelemetry-collector-contrib",
	"jaegertracing/jaeger",
	"prometheus/prometheus",
	"vllm-project/vllm",
	"sgl-project/sglang",
	"ray-project/ray",
	"run-llama/llama_index",
	"ollama/ollama",
	"qdrant/qdrant",
	"langfuse/langfuse",
}

const (
	githubSearch = "https://api.github.com/search/issues?"
	// IssueMaxAge is how old an open issue may be and still be listed.
	IssueMaxAge = 365 * 24 * time.Hour
	// IssueRows is how many issues the tracker lists.
	IssueRows = 25
	// reposPerQuery keeps a search query under GitHub's 256-character limit.
	reposPerQuery = 3
)

// Issue is one open, unassigned issue a newcomer can pick up.
type Issue struct {
	Repo, Title, Link string
	Number, Comments  int
	Labels            []string
	Opened            time.Time
}

// IssueQueries are the search URLs for the repos, a few repos to a query: open issues with no assignee and no linked
// pull request, opened since the age limit, labelled "good first issue" or "help wanted" (a comma means either).
func IssueQueries(repos []string, now time.Time) []string {
	var out []string
	for i := 0; i < len(repos); i += reposPerQuery {
		end := i + reposPerQuery
		if end > len(repos) {
			end = len(repos)
		}
		var q []string
		for _, r := range repos[i:end] {
			q = append(q, "repo:"+r)
		}
		q = append(q, "is:issue", "is:open", "no:assignee", "-linked:pr", "created:>="+now.Add(-IssueMaxAge).Format("2006-01-02"),
			`label:"good first issue","help wanted"`)
		out = append(out, githubSearch+"sort=created&order=desc&per_page=100&q="+url.QueryEscape(strings.Join(q, " ")))
	}
	return out
}

// metaIssue is a planning thread (an RFC, roadmap or tracking issue), not a piece of work a newcomer can pick up.
var metaIssue = regexp.MustCompile(`(?i)^\s*\[(rfc|roadmap|tracker|tracking|meta|discussion)\]|\b(roadmap|tracking issue|tracking .* (feature )?requests)\b|\(tracker\)`)

// ParseIssueSearch reads a GitHub issue search result. Pull requests, assigned issues and ones past the age
// limit are skipped.
func ParseIssueSearch(body []byte, now time.Time) ([]Issue, error) {
	var r struct {
		Message string `json:"message"`
		Items   []struct {
			Title    string          `json:"title"`
			Number   int             `json:"number"`
			HTMLURL  string          `json:"html_url"`
			RepoURL  string          `json:"repository_url"`
			Comments int             `json:"comments"`
			Created  string          `json:"created_at"`
			State    string          `json:"state"`
			Assignee json.RawMessage `json:"assignee"`
			Assigns  []any           `json:"assignees"`
			PR       json.RawMessage `json:"pull_request"`
			Labels   []struct {
				Name string `json:"name"`
			} `json:"labels"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("sources: unreadable GitHub search result: %w", err)
	}
	var out []Issue
	for _, it := range r.Items {
		opened, err := time.Parse(time.RFC3339, it.Created)
		hasAssignee := len(it.Assignee) > 0 && string(it.Assignee) != "null"
		isPR := len(it.PR) > 0 && string(it.PR) != "null"
		i := strings.Index(it.RepoURL, "/repos/")
		if err != nil || i < 0 || it.HTMLURL == "" || it.State != "open" || hasAssignee || len(it.Assigns) > 0 || isPR || now.Sub(opened) > IssueMaxAge || metaIssue.MatchString(it.Title) {
			continue
		}
		is := Issue{Repo: it.RepoURL[i+len("/repos/"):], Title: clean(it.Title), Link: it.HTMLURL, Number: it.Number, Comments: it.Comments, Opened: opened.UTC()}
		for _, l := range it.Labels {
			is.Labels = append(is.Labels, l.Name)
		}
		out = append(out, is)
	}
	return out, nil
}

// ReadIssues runs the issue searches. A search that fails is named in problems; when GitHub answers 403 or 429 (its
// rate limit) the remaining searches are not sent. The result is the newest issues first, at most IssueRows.
func ReadIssues(ctx context.Context, get Get, now time.Time) (issues []Issue, problems []string) {
	if get == nil {
		get = HTTPGet
	}
	seen := map[string]bool{}
	for _, u := range IssueQueries(OpenSourceRepos, now) {
		body, err := get(ctx, u)
		if err != nil {
			if strings.Contains(err.Error(), "HTTP 403") || strings.Contains(err.Error(), "HTTP 429") {
				problems = append(problems, "GitHub rate limit reached, so the rest of the repos were not searched")
				break
			}
			problems = append(problems, "a GitHub search")
			continue
		}
		page, err := ParseIssueSearch(body, now)
		if err != nil {
			problems = append(problems, "a GitHub search")
			continue
		}
		for _, is := range page {
			if !seen[is.Link] {
				seen[is.Link] = true
				issues = append(issues, is)
			}
		}
	}
	sort.SliceStable(issues, func(i, j int) bool { return issues[i].Opened.After(issues[j].Opened) })
	if len(issues) > IssueRows {
		issues = issues[:IssueRows]
	}
	return issues, problems
}

// IssuesListText is the Open Source tracker written from the issues.
func IssuesListText(issues []Issue) string {
	cell := func(s string) string {
		s = strings.TrimSpace(strings.Join(strings.Fields(strings.ReplaceAll(s, "|", "/")), " "))
		if s == "" {
			return "-"
		}
		return s
	}
	var b strings.Builder
	b.WriteString("# Open Source tracker\n\n")
	b.WriteString("| Repo | Issue | Labels | Comments | Opened | Link | Status |\n|---|---|---|---|---|---|---|\n")
	for _, is := range issues {
		fmt.Fprintf(&b, "| %s | #%d %s | %s | %d | %s | %s | open, unassigned |\n", cell(is.Repo), is.Number, cell(is.Title),
			cell(strings.Join(is.Labels, ", ")), is.Comments, is.Opened.Format("2006-01-02"), cell(is.Link))
	}
	return b.String()
}

package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
)

// TargetRepo is a repository to watch for good-first or help-wanted issues.
type TargetRepo struct {
	Repo     string `json:"repo"`
	Language string `json:"language,omitempty"`
}

// Issue is an open issue in a target repository meeting the search criteria.
type Issue struct {
	Repo      string    `json:"repo"`
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"created_at"`
	Labels    []string  `json:"labels"`
	Language  string    `json:"language"`
	Summary   string    `json:"summary"`
}

// ParseTargets parses target repos from JSON, accepting either a list of strings
// (e.g. ["golang/go", "tiangolo/fastapi"]) or a list of TargetRepo objects.
func ParseTargets(data []byte) ([]TargetRepo, error) {
	var asObjs []TargetRepo
	if err := json.Unmarshal(data, &asObjs); err == nil && len(asObjs) > 0 && asObjs[0].Repo != "" {
		return asObjs, nil
	}

	var asStrings []string
	if err := json.Unmarshal(data, &asStrings); err == nil {
		out := make([]TargetRepo, 0, len(asStrings))
		for _, s := range asStrings {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, TargetRepo{Repo: s})
			}
		}
		return out, nil
	}

	return nil, fmt.Errorf("targets JSON must be a list of repo strings or objects with 'repo' field")
}

// GitHubToken returns a token from GITHUB_TOKEN / GH_TOKEN env vars or `gh auth token` CLI.
func GitHubToken() string {
	if tok := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); tok != "" {
		return tok
	}
	if tok := strings.TrimSpace(os.Getenv("GH_TOKEN")); tok != "" {
		return tok
	}
	out, err := exec.Command("gh", "auth", "token").Output()
	if err == nil {
		if tok := strings.TrimSpace(string(out)); tok != "" {
			return tok
		}
	}
	return ""
}

var (
	goRe     = regexp.MustCompile(`(?i)\b(golang|go)\b`)
	pythonRe = regexp.MustCompile(`(?i)\b(python|py|pip|pytest)\b`)
)

// DetectLanguage inspects repo name, issue title, and body for Go or Python keywords.
func DetectLanguage(repoLang, title, body, repoName string) string {
	rl := strings.ToLower(repoLang)
	if rl == "go" || rl == "golang" {
		return "Go"
	}
	if rl == "python" || rl == "py" {
		return "Python"
	}

	combined := title + " " + body + " " + repoName
	if goRe.MatchString(combined) {
		return "Go"
	}
	if pythonRe.MatchString(combined) {
		return "Python"
	}
	return ""
}

// FetchRepoIssues searches GitHub for open, unassigned issues in target repos opened in the last 30 days.
func FetchRepoIssues(ctx context.Context, hc *http.Client, baseAPI string, repo TargetRepo, since time.Time, token string) ([]Issue, error) {
	if hc == nil {
		hc = http.DefaultClient
	}
	if baseAPI == "" {
		baseAPI = "https://api.github.com"
	}

	dateStr := since.Format("2006-01-02")
	// Query: repo:X is:open is:issue no:assignee created:>=YYYY-MM-DD
	// We check for good-first-issue or help-wanted labels.
	q := fmt.Sprintf("repo:%s is:open is:issue no:assignee created:>=%s label:\"good first issue\",\"help wanted\",\"good-first-issue\",\"help-wanted\"", repo.Repo, dateStr)
	u := fmt.Sprintf("%s/search/issues?q=%s&sort=created&order=desc&per_page=30", strings.TrimRight(baseAPI, "/"), url.QueryEscape(q))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "griffin-watch/1.0")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("github api error (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}

	var r struct {
		Items []struct {
			Number      int    `json:"number"`
			Title       string `json:"title"`
			HTMLURL     string `json:"html_url"`
			CreatedAt   string `json:"created_at"`
			Body        string `json:"body"`
			PullRequest *any   `json:"pull_request,omitempty"`
			Labels      []struct {
				Name string `json:"name"`
			} `json:"labels"`
		} `json:"items"`
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, 10<<20)).Decode(&r); err != nil {
		return nil, fmt.Errorf("decode github search: %w", err)
	}

	var issues []Issue
	for _, it := range r.Items {
		// Skip if it is actually a pull request (Search API returns PRs unless filtered)
		if it.PullRequest != nil {
			continue
		}

		t, _ := time.Parse(time.RFC3339, it.CreatedAt)
		var labels []string
		for _, l := range it.Labels {
			labels = append(labels, l.Name)
		}

		lang := DetectLanguage(repo.Language, it.Title, it.Body, repo.Repo)
		summary := fmt.Sprintf("[%s#%d] %s", repo.Repo, it.Number, clip(it.Title, 100))

		issues = append(issues, Issue{
			Repo:      repo.Repo,
			Number:    it.Number,
			Title:     it.Title,
			URL:       it.HTMLURL,
			CreatedAt: t,
			Labels:    labels,
			Language:  lang,
			Summary:   summary,
		})
	}

	return issues, nil
}

// RankAndFilterIssues selects up to limit best issues matching Go or Python, sorted by recency and label match.
func RankAndFilterIssues(issues []Issue, limit int) []Issue {
	var filtered []Issue
	for _, it := range issues {
		if it.Language == "Go" || it.Language == "Python" {
			filtered = append(filtered, it)
		}
	}

	// Sort: newest first
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})

	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered
}

package sources

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

var issueNow = time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)

func TestIssueQueriesStayShortAndAskForUnassignedUnlinkedLabelledIssues(t *testing.T) {
	qs := IssueQueries(OpenSourceRepos, issueNow)
	if len(qs) != 4 {
		t.Fatalf("eleven repos, three to a query: %d", len(qs))
	}
	for _, u := range qs {
		raw, _ := url.Parse(u)
		q := raw.Query().Get("q")
		if len(q) > 256 {
			t.Errorf("GitHub refuses queries over 256 characters: %d %s", len(q), q)
		}
		for _, want := range []string{"is:issue", "is:open", "no:assignee", "-linked:pr", `label:"good first issue","help wanted"`, "created:>=2025-04-10"} {
			if !strings.Contains(q, want) {
				t.Errorf("query lacks %s: %s", want, q)
			}
		}
	}
}

func TestOpenIssuesAreReadFromTheRealSearchAnswer(t *testing.T) {
	is, err := ParseIssueSearch(fixture(t, "github_issues.json"), issueNow)
	// #39001 is hand-built (the captured answer had no ordinary issue under a year old); RFC, roadmap and tracking
	// threads are planning, not work a newcomer can pick up, so they are skipped.
	if err != nil || len(is) != 1 {
		t.Fatalf("only the one ordinary issue under a year old: %d %v", len(is), err)
	}
	v := is[0]
	if v.Repo != "vllm-project/vllm" || v.Number != 39001 || v.Comments != 2 || v.Opened.Format("2006-01-02") != "2026-03-20" ||
		v.Link != "https://github.com/vllm-project/vllm/issues/39001" || !strings.Contains(strings.Join(v.Labels, ","), "good first issue") {
		t.Fatalf("%+v", v)
	}
}

func TestAssignedIssuesAndPullRequestsAreSkipped(t *testing.T) {
	body := `{"items":[
	 {"title":"taken","number":1,"html_url":"https://github.com/a/b/issues/1","repository_url":"https://api.github.com/repos/a/b","created_at":"2026-04-01T00:00:00Z","state":"open","assignee":{"login":"x"},"labels":[]},
	 {"title":"a pr","number":2,"html_url":"https://github.com/a/b/pull/2","repository_url":"https://api.github.com/repos/a/b","created_at":"2026-04-01T00:00:00Z","state":"open","pull_request":{"url":"u"},"labels":[]},
	 {"title":"closed","number":3,"html_url":"https://github.com/a/b/issues/3","repository_url":"https://api.github.com/repos/a/b","created_at":"2026-04-01T00:00:00Z","state":"closed","labels":[]},
	 {"title":"free","number":4,"html_url":"https://github.com/a/b/issues/4","repository_url":"https://api.github.com/repos/a/b","created_at":"2026-04-01T00:00:00Z","state":"open","assignee":null,"assignees":[],"labels":[{"name":"good first issue"}]}]}`
	is, err := ParseIssueSearch([]byte(body), issueNow)
	if err != nil || len(is) != 1 || is[0].Number != 4 {
		t.Fatalf("%v %+v", err, is)
	}
}

func TestIssuesAreNewestFirstAndCapped(t *testing.T) {
	var items []string
	for i := 1; i <= 40; i++ {
		day := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i%28)
		items = append(items, `{"title":"t`+strconv.Itoa(i)+`","number":`+strconv.Itoa(i)+`,"html_url":"https://github.com/a/b/issues/`+strconv.Itoa(i)+`","repository_url":"https://api.github.com/repos/a/b","created_at":"`+day.Format(time.RFC3339)+`","state":"open","labels":[]}`)
	}
	get := func(context.Context, string) ([]byte, error) {
		return []byte(`{"items":[` + strings.Join(items, ",") + `]}`), nil
	}
	is, problems := ReadIssues(context.Background(), get, issueNow)
	if len(is) != IssueRows || len(problems) != 0 {
		t.Fatalf("%d %v", len(is), problems)
	}
	for i := 1; i < len(is); i++ {
		if is[i].Opened.After(is[i-1].Opened) {
			t.Fatalf("newest first: %v then %v", is[i-1].Opened, is[i].Opened)
		}
	}
}

func TestAGitHubRateLimitStopsTheSearchesAndIsRecorded(t *testing.T) {
	calls := 0
	get := func(context.Context, string) ([]byte, error) {
		calls++
		return nil, errors.New("https://api.github.com/search/issues answered HTTP 403")
	}
	is, problems := ReadIssues(context.Background(), get, issueNow)
	if calls != 1 || len(is) != 0 || len(problems) != 1 || !strings.Contains(problems[0], "rate limit") {
		t.Fatalf("calls %d, issues %d, problems %v", calls, len(is), problems)
	}
}

func TestIssuesListIsATable(t *testing.T) {
	is, _ := ParseIssueSearch(fixture(t, "github_issues.json"), issueNow)
	text := IssuesListText(is)
	if !strings.Contains(text, "| Repo | Issue | Labels | Comments | Opened | Link | Status |") ||
		!strings.Contains(text, "| vllm-project/vllm | #39001 [Bug]: Streaming usage stats report the wrong completion token count | bug, good first issue | 2 | 2026-03-20 | https://github.com/vllm-project/vllm/issues/39001 | open, unassigned |") {
		t.Fatalf("\n%s", text)
	}
}

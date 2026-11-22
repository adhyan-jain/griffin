package sources

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

var hygieneNow = time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)

func TestRepoListSkipsForksAndKeepsTheFacts(t *testing.T) {
	repos, err := ParseRepos(fixture(t, "github_repos.json"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range repos {
		names = append(names, r.Name)
	}
	if strings.Join(names, ",") != "Spoon-Knife,Hello-World,octocat.github.io,git-consortium" {
		t.Fatalf("the two forks are skipped: %v", names)
	}
	if repos[3].License != "MIT" || repos[0].License != "" || repos[1].Pushed.IsZero() {
		t.Fatalf("%+v %+v", repos[3], repos[1])
	}
}

func TestRootListingShowsReadmeLicenseAndTheDotFolder(t *testing.T) {
	f, err := ParseRootListing(fixture(t, "github_root.json"))
	if err != nil || !f.README || f.License || f.GitHubDir {
		t.Fatalf("Hello-World has only a README: %+v %v", f, err)
	}
	f, _ = ParseRootListing([]byte(`[{"name":"LICENSE.md","type":"file"},{"name":"readme.rst","type":"file"},{"name":".github","type":"dir"},{"name":"README","type":"dir"}]`))
	if !f.README || !f.License || !f.GitHubDir {
		t.Fatalf("%+v", f)
	}
	if !HasWorkflows(fixture(t, "github_dotgithub.json")) || HasWorkflows([]byte(`[{"name":"CODEOWNERS","type":"file"}]`)) {
		t.Fatal("workflows is a directory inside .github")
	}
}

func TestFixNextIsGeneratedByRulesFromTheFailedChecks(t *testing.T) {
	stale := hygieneNow.AddDate(0, -8, 0)
	for _, c := range []struct {
		name string
		r    Repo
		want string
	}{
		{"all good", Repo{README: true, License: "MIT", Description: "d", CI: true, Pushed: hygieneNow.AddDate(0, 0, -3)}, "Nothing: all checks pass"},
		{"no license", Repo{README: true, Description: "d", CI: true, Pushed: hygieneNow}, "Add a LICENSE"},
		{"no CI", Repo{README: true, License: "MIT", Description: "d", Pushed: hygieneNow}, "Add CI: no .github/workflows"},
		{"stale", Repo{README: true, License: "MIT", Description: "d", CI: true, Pushed: stale}, "No push in 8 months: archive or update"},
		{"two shown at most", Repo{Pushed: hygieneNow}, "Add a README; Add a LICENSE"},
		{"issues", Repo{README: true, License: "MIT", Description: "d", CI: true, Pushed: hygieneNow, OpenIssues: 14}, "Triage 14 open issues"},
	} {
		if got := c.r.FixNext(hygieneNow); got != c.want {
			t.Errorf("%s: %q want %q", c.name, got, c.want)
		}
	}
}

// api answers the three kinds of request a hygiene run makes and records them.
func api(t *testing.T, calls *[]string) Get {
	return func(_ context.Context, u string) ([]byte, error) {
		*calls = append(*calls, u)
		switch {
		case strings.Contains(u, "/users/someone/repos"):
			return fixture(t, "github_repos.json"), nil
		case strings.HasSuffix(u, "/Spoon-Knife/contents/"):
			return []byte(`[{"name":"README.md","type":"file"},{"name":"LICENSE","type":"file"},{"name":".github","type":"dir"}]`), nil
		case strings.HasSuffix(u, "/contents/.github"):
			return fixture(t, "github_dotgithub.json"), nil
		case strings.HasSuffix(u, "/Hello-World/contents/"):
			return fixture(t, "github_root.json"), nil
		}
		return nil, errors.New(u + " answered HTTP 404")
	}
}

func TestEveryRepoIsCheckedWithAFewRequests(t *testing.T) {
	var calls []string
	repos, problems := ReadRepos(context.Background(), api(t, &calls), "someone")
	if len(repos) != 4 {
		t.Fatalf("%d repos %v", len(repos), problems)
	}
	// one list, one root listing a repo, one .github listing for the repo that has the folder; two repos 404 and are named.
	if len(calls) != 1+4+1 || len(problems) != 2 {
		t.Fatalf("calls %d %v, problems %v", len(calls), calls, problems)
	}
	var spoon *Repo
	for i := range repos {
		if repos[i].Name == "Spoon-Knife" {
			spoon = &repos[i]
		}
	}
	if spoon == nil || !spoon.Checked || !spoon.README || spoon.License != "yes" || !spoon.CI {
		t.Fatalf("%+v", spoon)
	}
	text := HygieneListText(repos, hygieneNow)
	if !strings.Contains(text, "| Repo | Stack | Last push | README | License | CI | Open issues | Fix next | Link |") ||
		!strings.Contains(text, "| Hello-World |") || strings.Contains(text, "| git-consortium |") {
		t.Fatalf("only repos that were read are listed:\n%s", text)
	}
	if strings.Index(text, "| Hello-World |") > strings.Index(text, "| Spoon-Knife |") {
		t.Fatalf("the repo with more failed checks comes first:\n%s", text)
	}
}

func TestTheRequestBudgetIsNeverExceeded(t *testing.T) {
	var list []string
	for i := 0; i < 80; i++ {
		list = append(list, `{"name":"r`+strings.Repeat("x", i)+`","html_url":"https://example.test/r","pushed_at":"2026-11-01T00:00:00Z","open_issues_count":0}`)
	}
	n := 0
	get := func(_ context.Context, u string) ([]byte, error) {
		n++
		if strings.Contains(u, "/users/") {
			return []byte("[" + strings.Join(list, ",") + "]"), nil
		}
		return []byte(`[{"name":"README.md","type":"file"}]`), nil
	}
	repos, _ := ReadRepos(context.Background(), get, "someone")
	checked := 0
	for _, r := range repos {
		if r.Checked {
			checked++
		}
	}
	if n > HygieneRequestCap || checked != HygieneRequestCap-1 {
		t.Fatalf("%d requests, %d repos checked", n, checked)
	}
}

func TestARateLimitStopsTheChecks(t *testing.T) {
	calls := 0
	get := func(_ context.Context, u string) ([]byte, error) {
		calls++
		if strings.Contains(u, "/users/") {
			return fixture(t, "github_repos.json"), nil
		}
		return nil, errors.New(u + " answered HTTP 403")
	}
	repos, problems := ReadRepos(context.Background(), get, "someone")
	if calls != 2 || len(problems) != 1 || !strings.Contains(problems[0], "rate limit") {
		t.Fatalf("calls %d problems %v", calls, problems)
	}
	for _, r := range repos {
		if r.Checked {
			t.Fatalf("nothing was read: %+v", r)
		}
	}
}

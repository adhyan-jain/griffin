package watch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTargets(t *testing.T) {
	// JSON as string array
	rawStrings := `["kubernetes/kubernetes", "golang/go"]`
	targets, err := ParseTargets([]byte(rawStrings))
	require.NoError(t, err)
	assert.Len(t, targets, 2)
	assert.Equal(t, "kubernetes/kubernetes", targets[0].Repo)

	// JSON as object array
	rawObjs := `[{"repo": "tiangolo/fastapi", "language": "python"}, {"repo": "gin-gonic/gin", "language": "go"}]`
	targets, err = ParseTargets([]byte(rawObjs))
	require.NoError(t, err)
	assert.Len(t, targets, 2)
	assert.Equal(t, "tiangolo/fastapi", targets[0].Repo)
	assert.Equal(t, "python", targets[0].Language)

	// Invalid JSON
	_, err = ParseTargets([]byte(`{"invalid": true}`))
	assert.Error(t, err)
}

func TestDetectLanguage(t *testing.T) {
	assert.Equal(t, "Go", DetectLanguage("go", "Add feature", "body", "some/repo"))
	assert.Equal(t, "Python", DetectLanguage("python", "Fix bug", "body", "some/repo"))
	assert.Equal(t, "Go", DetectLanguage("", "Refactor in golang router", "use go routines", "some/repo"))
	assert.Equal(t, "Python", DetectLanguage("", "Fix pytest failure", "pip install fails", "some/repo"))
	assert.Equal(t, "", DetectLanguage("", "Update README for CSS styles", "formatting", "some/repo"))
}

func TestFetchRepoIssues(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/search/issues", r.URL.Path)
		assert.Contains(t, r.URL.Query().Get("q"), "repo:golang/go")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"total_count": 1,
			"items": [
				{
					"number": 42,
					"title": "Clean up error string in net/http",
					"html_url": "https://github.com/golang/go/issues/42",
					"created_at": "2026-11-10T12:00:00Z",
					"body": "This is a good first issue for Go contributors",
					"labels": [{"name": "good-first-issue"}]
				}
			]
		}`))
	}))
	defer ts.Close()

	repo := TargetRepo{Repo: "golang/go", Language: "Go"}
	since := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	issues, err := FetchRepoIssues(context.Background(), ts.Client(), ts.URL, repo, since, "")
	require.NoError(t, err)
	require.Len(t, issues, 1)
	assert.Equal(t, 42, issues[0].Number)
	assert.Equal(t, "golang/go", issues[0].Repo)
	assert.Equal(t, "Go", issues[0].Language)
	assert.Contains(t, issues[0].Summary, "[golang/go#42]")
}

func TestRankAndFilterIssues(t *testing.T) {
	now := time.Now()
	issues := []Issue{
		{Number: 1, Language: "JavaScript", CreatedAt: now},
		{Number: 2, Language: "Go", CreatedAt: now.Add(-2 * time.Hour)},
		{Number: 3, Language: "Python", CreatedAt: now.Add(-1 * time.Hour)},
		{Number: 4, Language: "Ruby", CreatedAt: now},
	}

	best := RankAndFilterIssues(issues, 5)
	require.Len(t, best, 2)
	assert.Equal(t, 3, best[0].Number, "Python issue is newer than Go issue")
	assert.Equal(t, 2, best[1].Number)
}

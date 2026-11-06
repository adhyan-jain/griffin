package worker

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

func TestWithDependencyOutputs_NoDepsReturnsSameTask(t *testing.T) {
	task := &domain.Task{Goal: "do it"}
	assert.Same(t, task, withDependencyOutputs(task, nil, 0))
}

func TestWithDependencyOutputs_AppendsSectionAndLeavesOriginalUntouched(t *testing.T) {
	task := &domain.Task{Goal: "synthesize", InputPayload: json.RawMessage(`{"model":"m"}`)}
	deps := []repository.DependencyOutput{
		{Title: "Gather", Output: json.RawMessage(`{"response":"found X"}`)},
		{Title: "Other", Output: json.RawMessage(`{"k":1}`)},
	}
	got := withDependencyOutputs(task, deps, 0)

	assert.Equal(t, "synthesize", task.Goal)
	assert.JSONEq(t, `{"model":"m"}`, string(task.InputPayload))
	assert.True(t, strings.HasPrefix(got.Goal, "synthesize"))
	assert.Contains(t, got.Goal, "Results from earlier steps")
	assert.Contains(t, got.Goal, "Gather")
	assert.Contains(t, got.Goal, "found X")
	assert.NotContains(t, got.Goal, `"response"`)
	assert.Contains(t, got.Goal, `{"k":1}`)
}

func TestWithDependencyOutputs_RewritesInputPromptToo(t *testing.T) {
	task := &domain.Task{Goal: "g", InputPayload: json.RawMessage(`{"prompt":"custom","model":"m"}`)}
	got := withDependencyOutputs(task, []repository.DependencyOutput{{Title: "A", Output: json.RawMessage(`{"response":"up"}`)}}, 0)

	var in map[string]any
	require.NoError(t, json.Unmarshal(got.InputPayload, &in))
	assert.Equal(t, "m", in["model"])
	assert.Contains(t, in["prompt"], "custom")
	assert.Contains(t, in["prompt"], "up")
	assert.JSONEq(t, `{"prompt":"custom","model":"m"}`, string(task.InputPayload))
}

func TestWithDependencyOutputs_Truncates(t *testing.T) {
	big := strings.Repeat("a", 5000)
	out, _ := json.Marshal(map[string]string{"response": big})
	deps := []repository.DependencyOutput{{Title: "Big", Output: out}}
	got := withDependencyOutputs(&domain.Task{Goal: "g"}, deps, 1000)

	assert.Contains(t, got.Goal, "[truncated]")
	assert.Less(t, len(got.Goal), 1000+500)
}

func TestResultFile_PrefersTextThenJSON(t *testing.T) {
	body, ext, typ, ct, ok := resultFile(&ExecutionResult{TextOutput: "  **Done**\n| a | b |\n"})
	assert.True(t, ok)
	assert.Equal(t, "md", ext)
	assert.Equal(t, "MARKDOWN", typ)
	assert.Contains(t, ct, "markdown")
	assert.Equal(t, "**Done**\n| a | b |\n", string(body))

	body, ext, _, _, ok = resultFile(&ExecutionResult{Output: json.RawMessage(`{"response":"from payload"}`)})
	assert.True(t, ok)
	assert.Equal(t, "md", ext)
	assert.Equal(t, "from payload\n", string(body))

	body, ext, typ, _, ok = resultFile(&ExecutionResult{Output: json.RawMessage(`{"status":"ok","n":1}`)})
	assert.True(t, ok)
	assert.Equal(t, "json", ext)
	assert.Equal(t, "JSON", typ)
	assert.Contains(t, string(body), `"status": "ok"`)

	_, _, _, _, ok = resultFile(&ExecutionResult{})
	assert.False(t, ok, "an empty run saves nothing")
}

func TestFileSlug(t *testing.T) {
	assert.Equal(t, "check-if-invideo-has-any-interns-opening", fileSlug("Check if inVideo has any interns opening!"))
	assert.Equal(t, "result", fileSlug("???"))
	assert.LessOrEqual(t, len(fileSlug(strings.Repeat("word ", 40))), 60)
}

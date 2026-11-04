package worker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

// maxDependencyContextBytes caps the upstream-results section added to a prompt.
const maxDependencyContextBytes = 20 * 1024

const truncationMarker = "\n...[truncated]"

// withDependencyOutputs returns a copy of task whose prompt (Goal, and input_payload.prompt when
// set) also carries the outputs of its completed prerequisites. The original task is not modified.
// With no dependencies it returns task itself. limit <= 0 selects the default cap.
func withDependencyOutputs(task *domain.Task, deps []repository.DependencyOutput, limit int) *domain.Task {
	if len(deps) == 0 {
		return task
	}
	if limit <= 0 {
		limit = maxDependencyContextBytes
	}

	var b strings.Builder
	b.WriteString("\n\n--- Results from earlier steps ---\n")
	for i, d := range deps {
		fmt.Fprintf(&b, "\n[%d] %s\n%s\n", i+1, d.Title, outputText(d.Output))
	}
	b.WriteString("--- End of results from earlier steps ---")

	section := b.String()
	if len(section) > limit {
		cut := limit - len(truncationMarker)
		if cut < 0 {
			cut = 0
		}
		// avoid splitting a multi-byte rune
		for cut > 0 && cut < len(section) && (section[cut]&0xC0) == 0x80 {
			cut--
		}
		section = section[:cut] + truncationMarker
	}

	cp := *task
	cp.Goal = task.Goal + section

	var in map[string]json.RawMessage
	if len(task.InputPayload) > 0 && json.Unmarshal(task.InputPayload, &in) == nil {
		var prompt string
		if raw, ok := in["prompt"]; ok && json.Unmarshal(raw, &prompt) == nil && strings.TrimSpace(prompt) != "" {
			in["prompt"], _ = json.Marshal(prompt + section)
			if rewritten, err := json.Marshal(in); err == nil {
				cp.InputPayload = rewritten
			}
		}
	}
	return &cp
}

// outputText extracts the `response` string from {"response": "..."} outputs and falls back to compact JSON.
func outputText(out json.RawMessage) string {
	var env struct {
		Response *string `json:"response"`
	}
	if json.Unmarshal(out, &env) == nil && env.Response != nil {
		return *env.Response
	}
	var buf bytes.Buffer
	if json.Compact(&buf, out) == nil {
		return buf.String()
	}
	return string(out)
}

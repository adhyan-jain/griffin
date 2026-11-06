package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	maxStepSummaryChars  = 1500
	maxFinalSummaryChars = 6000
)

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "... [truncated]"
}

// stepOutputText returns the human-readable text of a step's output_payload:
// output.response when it is non-blank text, otherwise the compact JSON.
// It returns "" when the output carries no content (empty, null, {}, [] or blank response).
func stepOutputText(payload json.RawMessage) string {
	trimmed := bytes.TrimSpace(payload)
	switch string(trimmed) {
	case "", "null", "{}", "[]", `""`:
		return ""
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(trimmed, &obj) == nil {
		if raw, ok := obj["response"]; ok {
			var text string
			if json.Unmarshal(raw, &text) == nil {
				if strings.TrimSpace(text) != "" {
					return strings.TrimSpace(text)
				}
				if len(obj) == 1 {
					return "" // only an empty response
				}
			}
		}
	}
	var compact bytes.Buffer
	if json.Compact(&compact, trimmed) == nil {
		return compact.String()
	}
	return string(trimmed)
}

// summaryPayload wraps the final response as valid JSON ({"summary": ...}).
func summaryPayload(s string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"summary": s})
	return b
}

// buildFinalResponse renders the orchestration's final response from per-step texts.
func buildFinalResponse(managerID string, titles, texts []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Objective achieved by Manager %s.\n\nStep results:\n", managerID)
	for i, text := range texts {
		fmt.Fprintf(&b, "\n### %s\n%s\n", titles[i], truncateRunes(text, maxStepSummaryChars))
	}
	return truncateRunes(strings.TrimRight(b.String(), "\n"), maxFinalSummaryChars)
}

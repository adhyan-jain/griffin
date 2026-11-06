package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStepOutputText(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"response text", `{"response":"  Varkala has cliffs.  ","provider":{"name":"antigravity"}}`, "Varkala has cliffs."},
		{"no response key uses compact json", `{ "status": "success",  "n": 1 }`, `{"status":"success","n":1}`},
		{"empty object", `{}`, ""},
		{"null", `null`, ""},
		{"empty", ``, ""},
		{"blank response only", `{"response":"   "}`, ""},
		{"blank response with other fields", `{"response":"","x":1}`, `{"response":"","x":1}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, stepOutputText(json.RawMessage(tc.in)))
		})
	}
}

func TestBuildFinalResponse_CapsStepsAndTotal(t *testing.T) {
	long := strings.Repeat("a", 5000)
	got := buildFinalResponse("research", []string{"Step one"}, []string{long})
	assert.Contains(t, got, "Step one")
	assert.Less(t, strings.Count(got, "a"), 1600)
	assert.Contains(t, got, "[truncated]")

	var titles, texts []string
	for i := 0; i < 10; i++ {
		titles = append(titles, "S")
		texts = append(texts, long)
	}
	assert.LessOrEqual(t, len([]rune(buildFinalResponse("m", titles, texts))), maxFinalSummaryChars+len("... [truncated]"))
}

func TestBuildFinalResponse_ContainsActualText(t *testing.T) {
	got := buildFinalResponse("research", []string{"Collect facts"}, []string{"Varkala is in Kerala."})
	assert.Contains(t, got, "Varkala is in Kerala.")
	assert.NotContains(t, got, "{}")
}

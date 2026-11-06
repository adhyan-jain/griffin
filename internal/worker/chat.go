package worker

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/griffin/core/internal/domain"
)

// ErrEmptyChatResponse is raised when a chat task "succeeds" without any assistant text.
var ErrEmptyChatResponse = errors.New("chat task produced an empty response")

// isChatTask reports whether a task is a JJ chat turn:
// manager_domain JJ, no parent, input_payload.kind == "chat".
func isChatTask(task *domain.Task) bool {
	if task.ManagerDomain != "JJ" || task.ParentID != nil || len(task.InputPayload) == 0 {
		return false
	}
	var env struct {
		Kind string `json:"kind"`
	}
	return json.Unmarshal(task.InputPayload, &env) == nil && env.Kind == "chat"
}

// chatResponseText extracts the assistant text from a result (TextOutput, else output.response).
func chatResponseText(res *ExecutionResult) string {
	if t := strings.TrimSpace(res.TextOutput); t != "" {
		return res.TextOutput
	}
	var out struct {
		Response string `json:"response"`
	}
	if len(res.Output) > 0 && json.Unmarshal(res.Output, &out) == nil && strings.TrimSpace(out.Response) != "" {
		return out.Response
	}
	return ""
}

// buildChatOutput renders the contract output_payload for a chat task.
func buildChatOutput(text, providerName string, facts RuntimeFacts) json.RawMessage {
	if facts.Version == "" {
		facts.Version = "unknown"
	}
	if facts.Model == "" {
		facts.Model = "default"
	}
	b, _ := json.Marshal(map[string]any{
		"response": text,
		"provider": map[string]string{"name": providerName, "version": facts.Version, "model": facts.Model},
	})
	return b
}

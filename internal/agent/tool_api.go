package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/provider/gemini"
)

// ToolInfo describes one tool for an outside caller (the MCP server).
type ToolInfo struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

// ToolList lists the registry's tools with their argument schemas.
func (a *Agent) ToolList() []ToolInfo {
	out := make([]ToolInfo, 0)
	for _, name := range a.Registry.Names() {
		t := a.Registry.Get(name)
		schema := t.Schema
		if props, ok := schema["properties"].(map[string]any); !ok || len(props) == 0 {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, ToolInfo{Name: t.Name, Description: t.Description, InputSchema: schema})
	}
	return out
}

// TaskConversationID is the store conversation that holds what one task's tool calls have seen, so ids shown by a
// list tool stay usable by later calls of the same task and never leak to another task.
func TaskConversationID(taskID uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("agent-tools-task:"+taskID.String()))
}

// RunTool runs one tool for a task's owner on behalf of a model run elsewhere (a laptop or EC2 model task). The origin
// is always OriginScheduled: an outward tool only makes an Approval. The reply is what the model should see:
// {"result": ...} or {"error": ...}.
func (a *Agent) RunTool(ctx context.Context, userID, taskID uuid.UUID, name string, args json.RawMessage) (map[string]any, error) {
	if a.Registry.Get(name) == nil {
		return nil, fmt.Errorf("unknown tool %q", name)
	}
	var m map[string]any
	if len(args) > 0 && string(args) != "null" {
		if err := json.Unmarshal(args, &m); err != nil {
			return map[string]any{"error": "bad arguments: " + err.Error()}, nil
		}
	}
	convID := TaskConversationID(taskID)
	conv, err := a.Store.GetConversation(ctx, userID, convID)
	if err != nil {
		return nil, err
	}
	if conv == nil {
		conv = &Conversation{ID: convID, UserID: userID}
	}
	rs := &run{call: &Call{UserID: userID, Origin: OriginScheduled, Now: a.now(), TurnID: uuid.New(), conv: conv}}
	resp := a.execute(ctx, rs, gemini.FunctionCall{Name: name, Args: m})
	if err := a.Store.SaveConversation(context.WithoutCancel(ctx), conv); err != nil {
		return nil, err
	}
	return resp, nil
}

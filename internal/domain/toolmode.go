package domain

import "encoding/json"

// ToolMode says how much a step may do on the machine, as opposed to only writing text.
//
//	none  the default and today's behaviour: text only, no tools.
//	read  read-only research: search, open pages, read files. Nothing is changed, sent or submitted.
//	act   may act on the user's behalf (log in, fill forms, send). Never runs without an approved
//	      sign-off for that exact task, and is never chosen automatically.
type ToolMode string

const (
	ToolNone ToolMode = "none"
	ToolRead ToolMode = "read"
	ToolAct  ToolMode = "act"

	// ToolModeKey is where the mode lives inside a task's input_payload JSON (no schema change).
	ToolModeKey = "tool_mode"

	// ToolApprovalAction is the approval action type that grants a task its act tier.
	ToolApprovalAction = "tool_act"
)

type toolPayloadView struct {
	Kind string   `json:"kind"`
	Mode ToolMode `json:"tool_mode"`
}

// ToolModeFor returns the tier a task asked for. Anything unrecognised, and every chat task, is none,
// so a malformed payload can only ever make a task less capable, never more.
func ToolModeFor(task *Task) ToolMode {
	var v toolPayloadView
	if len(task.InputPayload) > 0 {
		_ = json.Unmarshal(task.InputPayload, &v)
	}
	if v.Kind == "chat" {
		return ToolNone
	}
	if v.Mode == ToolRead || v.Mode == ToolAct {
		return v.Mode
	}
	return ToolNone
}

// SuggestToolMode is what the planner records for a step: read-only tools for work that touches the
// outside world, otherwise none. It never suggests act; that is an explicit, approved choice.
func SuggestToolMode(title, goal string) ToolMode {
	if needsMachine.MatchString(title + " " + goal) {
		return ToolRead
	}
	return ToolNone
}

// WithToolMode records the suggested tier in a step's payload, respecting an existing value and
// leaving a payload that is not a JSON object untouched.
func WithToolMode(payload json.RawMessage, title, goal string) json.RawMessage {
	obj := map[string]json.RawMessage{}
	if len(payload) > 0 && string(payload) != "null" {
		if err := json.Unmarshal(payload, &obj); err != nil {
			return payload
		}
	}
	if _, ok := obj[ToolModeKey]; ok {
		return payload
	}
	m, _ := json.Marshal(SuggestToolMode(title, goal))
	obj[ToolModeKey] = m
	out, err := json.Marshal(obj)
	if err != nil {
		return payload
	}
	return out
}

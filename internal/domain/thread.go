package domain

import (
	"encoding/json"
	"strings"

	"github.com/google/uuid"
)

// Threads. A task that is continued ("also add X", "make it shorter", "now send it") is not a new,
// unrelated task: it joins the thread of the task it continues. A thread has a root (the first task) and a
// task knows its thread through two payload fields, so no schema change is needed.
const (
	ThreadIDKey   = "thread_id"    // the root task's id
	FollowUpOfKey = "follow_up_of" // the task this one continues
)

// ThreadOf returns a task's thread root id and, when it is a follow-up, the task it continues.
// A task that is not a follow-up is the root of its own thread.
func ThreadOf(t *Task) (root uuid.UUID, followUpOf *uuid.UUID) {
	var p struct {
		Thread   string `json:"thread_id"`
		FollowUp string `json:"follow_up_of"`
	}
	if len(t.InputPayload) > 0 {
		_ = json.Unmarshal(t.InputPayload, &p)
	}
	if id, err := uuid.Parse(p.Thread); err == nil {
		root = id
	} else {
		root = t.ID
	}
	if id, err := uuid.Parse(p.FollowUp); err == nil {
		followUpOf = &id
	}
	return root, followUpOf
}

// IsFollowUp reports whether the task continues another and so belongs inside that task's thread rather
// than on its own row.
func IsFollowUp(t *Task) bool {
	_, f := ThreadOf(t)
	return f != nil
}

var resultTextKeys = []string{"response", "result", "output", "answer", "summary", "text", "message", "content"}

// ResultText is the readable answer in a task's output payload (the same keys the web app reads), or ""
// when there is none.
func ResultText(output json.RawMessage) string {
	if len(output) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(output, &s) == nil {
		return strings.TrimSpace(s)
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(output, &m) != nil {
		return ""
	}
	for _, k := range resultTextKeys {
		var v string
		if raw, ok := m[k]; ok && json.Unmarshal(raw, &v) == nil && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

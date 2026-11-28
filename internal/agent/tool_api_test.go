package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
)

func TestRunToolIsScheduledOriginAndScopedToTheTask(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	taskA, taskB := uuid.New(), uuid.New()

	// an outward tool only makes an Approval, whoever calls it
	resp, err := e.agent.RunTool(ctx, e.user, taskA, "send_test_message", json.RawMessage(`{"to":"Asha","text":"hi"}`))
	if err != nil || len(e.sent) != 0 || len(e.apps.pending) != 1 || !strings.Contains(resp["result"].(string), "Nothing has been sent") {
		t.Fatalf("resp=%v err=%v sent=%v pending=%d", resp, err, e.sent, len(e.apps.pending))
	}

	// ids a list tool showed task A are usable by task A only
	rt := mkTask(e.user, "Chase Asha", domain.TaskStatusQueued)
	e.tasks.tasks = []*domain.Task{rt}
	if r, _ := e.agent.RunTool(ctx, e.user, taskA, "list_tasks", nil); r["error"] != nil {
		t.Fatalf("list_tasks: %v", r)
	}
	if r, _ := e.agent.RunTool(ctx, e.user, taskA, "cancel_tasks", json.RawMessage(`{"ids":["`+rt.ID.String()+`"]}`)); r["error"] != nil && strings.Contains(r["error"].(string), "not returned") {
		t.Fatalf("task A should have seen the id: %v", r)
	}
	r, _ := e.agent.RunTool(ctx, e.user, taskB, "cancel_tasks", json.RawMessage(`{"ids":["`+rt.ID.String()+`"]}`))
	if r["error"] == nil || !strings.Contains(r["error"].(string), "not returned") {
		t.Fatalf("task B must not use task A's ids: %v", r)
	}
	if _, err := e.agent.RunTool(ctx, e.user, taskA, "no_such_tool", nil); err == nil {
		t.Fatal("an unknown tool must be refused")
	}
	if len(e.agent.ToolList()) == 0 || e.agent.ToolList()[0].InputSchema == nil {
		t.Fatal("ToolList must carry schemas")
	}
}

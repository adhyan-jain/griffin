package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/agent"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
)

type fakeToolRunner struct{ calls []string }

func (f *fakeToolRunner) ToolList() []agent.ToolInfo {
	return []agent.ToolInfo{{Name: "list_tasks", Description: "d", InputSchema: map[string]any{"type": "object"}}}
}
func (f *fakeToolRunner) RunTool(_ context.Context, user, task uuid.UUID, name string, _ json.RawMessage) (map[string]any, error) {
	f.calls = append(f.calls, user.String()+"/"+task.String()+"/"+name)
	return map[string]any{"result": "ok"}, nil
}

type fakeTaskGetter map[uuid.UUID]*domain.Task

func (f fakeTaskGetter) GetTask(_ context.Context, id uuid.UUID) (*domain.Task, error) {
	if t, ok := f[id]; ok {
		return t, nil
	}
	return nil, domain.ErrNotFound
}

func TestHostAgentToolsRequireTheHostsLease(t *testing.T) {
	host := &domain.Host{ID: uuid.New(), UserID: uuid.New()}
	other := host.WorkerID() + "-other"
	mine, theirs, notRunning, foreignUser := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	w := host.WorkerID()
	tasks := fakeTaskGetter{
		mine:        {ID: mine, UserID: host.UserID, Status: domain.TaskStatusRunning, LeasedBy: &w},
		theirs:      {ID: theirs, UserID: host.UserID, Status: domain.TaskStatusRunning, LeasedBy: &other},
		notRunning:  {ID: notRunning, UserID: host.UserID, Status: domain.TaskStatusCompleted, LeasedBy: &w},
		foreignUser: {ID: foreignUser, UserID: uuid.New(), Status: domain.TaskStatusRunning, LeasedBy: &w},
	}
	runner := &fakeToolRunner{}
	h := HostAgentHandler{Tools: runner, Tasks: tasks}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /tools", h.ListTools)
	mux.HandleFunc("POST /tools/{name}", h.CallTool)

	do := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(middleware.WithHost(context.Background(), host))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	if rec := do("GET", "/tools", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), "list_tasks") {
		t.Fatalf("list = %d %s", rec.Code, rec.Body)
	}
	for name, id := range map[string]uuid.UUID{"another host's lease": theirs, "a finished task": notRunning, "another owner's task": foreignUser, "no such task": uuid.New()} {
		rec := do("POST", "/tools/list_tasks", `{"task_id":"`+id.String()+`","args":{}}`)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s: status %d, want 403", name, rec.Code)
		}
	}
	if rec := do("POST", "/tools/list_tasks", `{"args":{}}`); rec.Code == 200 {
		t.Fatal("a missing task_id must be refused")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("a refused call ran a tool: %v", runner.calls)
	}
	rec := do("POST", "/tools/list_tasks", `{"task_id":"`+mine.String()+`","args":{}}`)
	if rec.Code != 200 || len(runner.calls) != 1 || runner.calls[0] != host.UserID.String()+"/"+mine.String()+"/list_tasks" {
		t.Fatalf("held task: %d %s calls=%v", rec.Code, rec.Body, runner.calls)
	}
}

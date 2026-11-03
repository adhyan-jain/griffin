package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api/middleware"
)

type apiError struct {
	Success bool `json:"success"`
	Error   struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func postJSON(t *testing.T, handler http.HandlerFunc, path, body string) (int, apiError) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, uuid.New()))
	rec := httptest.NewRecorder()
	handler(rec, req)
	var out apiError
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("response is not the API error envelope: %q (%v)", rec.Body.String(), err)
	}
	return rec.Code, out
}

// Every entry point that accepts a priority must reject bad values with a 400 that tells the
// caller what is allowed. The engine is nil on purpose: validation must happen before it is used.
func TestPriorityValidationOnEntryPoints(t *testing.T) {
	task := NewTaskHandler(nil, nil, nil, false)
	orch := NewOrchestratorHandler(nil, nil, nil, false)

	entry := []struct {
		name    string
		handler http.HandlerFunc
		path    string
		body    func(priority string) string
	}{
		{"create task", task.CreateTask, "/api/v1/tasks",
			func(p string) string { return `{"title":"t","goal":"g","priority":` + p + `}` }},
		{"submit request", orch.SubmitRequest, "/api/v1/orchestrator/requests",
			func(p string) string { return `{"request_text":"do a thing","priority":` + p + `}` }},
	}
	bad := []struct {
		name, priority, wantInMessage string
	}{
		{"number", `1`, "priority must be a string"},
		{"boolean", `true`, "priority must be a string"},
		{"unknown string", `"urgent"`, `priority "urgent" is not valid`},
		{"wrong case", `"high_p1"`, `priority "high_p1" is not valid`},
	}
	for _, e := range entry {
		for _, b := range bad {
			t.Run(e.name+"/"+b.name, func(t *testing.T) {
				status, out := postJSON(t, e.handler, e.path, e.body(b.priority))
				if status != http.StatusBadRequest {
					t.Fatalf("status = %d, want 400", status)
				}
				if out.Error.Code != "VALIDATION_ERROR" {
					t.Fatalf("code = %q, want VALIDATION_ERROR", out.Error.Code)
				}
				if !strings.Contains(out.Error.Message, b.wantInMessage) {
					t.Fatalf("message %q does not contain %q", out.Error.Message, b.wantInMessage)
				}
				for _, allowed := range []string{"CRITICAL_P0", "HIGH_P1", "NORMAL_P2", "BACKGROUND_P3"} {
					if !strings.Contains(out.Error.Message, allowed) {
						t.Fatalf("message %q should list %s", out.Error.Message, allowed)
					}
				}
			})
		}
	}
}

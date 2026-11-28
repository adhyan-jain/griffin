package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMCPListAndCallRoundTrip(t *testing.T) {
	var gotAuth, gotBody, gotPath string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/host/agent/tools":
			_, _ = w.Write([]byte(`{"success":true,"data":{"tools":[{"name":"list_tasks","description":"List tasks","input_schema":{"type":"object","properties":{}}}]}}`))
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/v1/host/agent/tools/"):
			var b bytes.Buffer
			_, _ = b.ReadFrom(r.Body)
			if strings.HasSuffix(r.URL.Path, "/list_tasks") {
				gotPath, gotBody = r.URL.Path, b.String()
			}
			if strings.HasSuffix(r.URL.Path, "/boom") {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"success":false,"error":{"code":"FORBIDDEN","message":"nope"}}`))
				return
			}
			if strings.HasSuffix(r.URL.Path, "/bad_args") {
				_, _ = w.Write([]byte(`{"success":true,"data":{"error":"bad arguments"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"result":"3 tasks"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer api.Close()

	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_tasks","arguments":{"status":"queued"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"boom"}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"bad_args"}}`,
		`{"jsonrpc":"2.0","id":6,"method":"nope"}`,
	}, "\n") + "\n"
	var out bytes.Buffer
	if err := newServer(api.URL, "ghk_tok", "task-9").serve(context.Background(), strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	var lines []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("bad line %q", l)
		}
		lines = append(lines, m)
	}
	if len(lines) != 6 { // the notification got no reply
		t.Fatalf("got %d replies: %s", len(lines), out.String())
	}
	if lines[0]["result"].(map[string]any)["protocolVersion"] != "2025-03-26" {
		t.Fatalf("initialize = %v", lines[0])
	}
	tools := lines[1]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != 1 || tools[0].(map[string]any)["name"] != "list_tasks" || tools[0].(map[string]any)["inputSchema"] == nil {
		t.Fatalf("tools/list = %v", lines[1])
	}
	call := lines[2]["result"].(map[string]any)
	if call["isError"] != false || call["content"].([]any)[0].(map[string]any)["text"] != "3 tasks" {
		t.Fatalf("tools/call = %v", call)
	}
	if gotAuth != "Bearer ghk_tok" || !strings.Contains(gotBody, `"task_id":"task-9"`) || !strings.Contains(gotBody, `"status":"queued"`) {
		t.Fatalf("auth=%q body=%s path=%s", gotAuth, gotBody, gotPath)
	}
	for i, want := range []string{"nope", "bad arguments"} {
		r := lines[3+i]["result"].(map[string]any)
		if r["isError"] != true || !strings.Contains(r["content"].([]any)[0].(map[string]any)["text"].(string), want) {
			t.Fatalf("error result %d = %v", i, r)
		}
	}
	if lines[5]["error"].(map[string]any)["code"].(float64) != -32601 {
		t.Fatalf("unknown method = %v", lines[5])
	}
}

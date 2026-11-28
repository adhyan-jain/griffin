package gemini

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestChatWithToolsParsesFunctionCallAndSendsDeclarations(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		if r.Header.Get("x-goog-api-key") != "k" {
			t.Error("key header missing")
		}
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"list_tasks","args":{"limit":3}},"thoughtSignature":"sig"}]}}]}`))
	}))
	defer srv.Close()
	c := &Client{APIKey: "k", Model: "m", BaseURL: srv.URL}
	rep, err := c.ChatWithTools(context.Background(), "sys", []Content{{Role: "user", Parts: []Part{{Text: "hi"}}}},
		[]FunctionDecl{{Name: "list_tasks", Description: "d", Parameters: map[string]any{"type": "object"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Calls) != 1 || rep.Calls[0].Name != "list_tasks" || rep.Calls[0].Args["limit"].(float64) != 3 {
		t.Fatalf("calls = %+v", rep.Calls)
	}
	if rep.Raw.Parts[0].ThoughtSignature != "sig" {
		t.Fatal("thought signature must be kept for the next turn")
	}
	tools := got["tools"].([]any)[0].(map[string]any)["functionDeclarations"].([]any)
	if tools[0].(map[string]any)["name"] != "list_tasks" {
		t.Fatalf("tools = %v", got["tools"])
	}
}

func TestChatWithToolsSendsFunctionResponseAndReadsText(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"All set."}]}}]}`))
	}))
	defer srv.Close()
	c := &Client{APIKey: "k", BaseURL: srv.URL}
	rep, err := c.ChatWithTools(context.Background(), "sys", []Content{
		{Role: "user", Parts: []Part{{Text: "hi"}}},
		{Role: "model", Parts: []Part{{FunctionCall: &FunctionCall{Name: "x"}}}},
		{Role: "user", Parts: []Part{{FunctionResponse: &FunctionResponse{Name: "x", Response: map[string]any{"result": "ok"}}}}},
	}, nil)
	if err != nil || rep.Text != "All set." || len(rep.Calls) != 0 {
		t.Fatalf("rep=%+v err=%v", rep, err)
	}
	if !strings.Contains(body, `"functionResponse"`) || strings.Contains(body, `"tools"`) {
		t.Fatalf("body = %s", body)
	}
}

func TestChatWithToolsFallsBackOnceWhenModelOverloaded(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if strings.Contains(r.URL.Path, "main") {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"message":"gone"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"hi"}]}}]}`))
	}))
	defer srv.Close()
	c := &Client{APIKey: "k", Model: "main", Fallback: "alt", BaseURL: srv.URL}
	rep, err := c.ChatWithTools(context.Background(), "s", []Content{{Role: "user", Parts: []Part{{Text: "x"}}}}, nil)
	if err != nil || rep.Text != "hi" {
		t.Fatalf("rep=%+v err=%v", rep, err)
	}
}

func TestChatWithToolsNotConfigured(t *testing.T) {
	_, err := (&Client{}).ChatWithTools(context.Background(), "s", []Content{{Role: "user"}}, nil)
	if err != ErrNotConfigured {
		t.Fatalf("err = %v", err)
	}
}

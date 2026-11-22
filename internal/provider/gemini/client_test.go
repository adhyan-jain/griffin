package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func standIn(t *testing.T, status int, reply string, seen *struct {
	path, key, body string
}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if seen != nil {
			b, _ := io.ReadAll(r.Body)
			seen.path, seen.key, seen.body = r.URL.Path, r.Header.Get("x-goog-api-key"), string(b)
			if r.URL.RawQuery != "" {
				t.Errorf("the key or anything else must not be in the URL query: %q", r.URL.RawQuery)
			}
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}))
}

const okReply = `{"candidates":[{"content":{"parts":[{"text":"Not much, "},{"text":"what's up?"}]},"finishReason":"STOP"}]}`

func TestChatSendsConversationAndReadsReply(t *testing.T) {
	var seen struct{ path, key, body string }
	srv := standIn(t, 200, okReply, &seen)
	defer srv.Close()
	c := &Client{APIKey: "k-123", Model: "m1", BaseURL: srv.URL}

	got, err := c.Chat(context.Background(), "You are Jarvis.", []Turn{
		{Role: "user", Text: "hi"}, {Role: "assistant", Text: "hello"}, {Role: "user", Text: "wassup"},
	})
	if err != nil || got != "Not much, what's up?" {
		t.Fatalf("got %q, %v", got, err)
	}
	if seen.path != "/v1beta/models/m1:generateContent" || seen.key != "k-123" {
		t.Errorf("path %q key %q", seen.path, seen.key)
	}
	var sent struct {
		SystemInstruction struct{ Parts []struct{ Text string } } `json:"systemInstruction"`
		Contents          []struct {
			Role  string
			Parts []struct{ Text string }
		}
	}
	if err := json.Unmarshal([]byte(seen.body), &sent); err != nil {
		t.Fatal(err)
	}
	if sent.SystemInstruction.Parts[0].Text != "You are Jarvis." || len(sent.Contents) != 3 {
		t.Fatalf("bad request body: %s", seen.body)
	}
	if sent.Contents[1].Role != "model" || sent.Contents[0].Role != "user" {
		t.Errorf("assistant turns must be sent as role model: %s", seen.body)
	}
}

func TestChatFailuresAreTyped(t *testing.T) {
	cases := []struct {
		name   string
		status int
		reply  string
		want   error
	}{
		{"rate limit", 429, `{"error":{"message":"quota"}}`, ErrRateLimited},
		{"blocked prompt", 200, `{"promptFeedback":{"blockReason":"SAFETY"}}`, ErrBlocked},
		{"blocked answer", 200, `{"candidates":[{"content":{"parts":[]},"finishReason":"SAFETY"}]}`, ErrBlocked},
	}
	for _, tc := range cases {
		srv := standIn(t, tc.status, tc.reply, nil)
		c := &Client{APIKey: "k", BaseURL: srv.URL}
		_, err := c.Chat(context.Background(), "s", []Turn{{Role: "user", Text: "x"}})
		srv.Close()
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, err, tc.want)
		}
	}
	srv := standIn(t, 500, `{"error":{"message":"boom"}}`, nil)
	defer srv.Close()
	_, err := (&Client{APIKey: "k", BaseURL: srv.URL}).Chat(context.Background(), "s", []Turn{{Role: "user", Text: "x"}})
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") || !strings.Contains(err.Error(), "boom") {
		t.Errorf("server error should say what happened, got %v", err)
	}
}

func TestChatWithoutKeyOrUserTurn(t *testing.T) {
	if _, err := (&Client{}).Chat(context.Background(), "s", []Turn{{Role: "user", Text: "x"}}); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("no key: got %v", err)
	}
	c := &Client{APIKey: "k"}
	if _, err := c.Chat(context.Background(), "s", nil); err == nil {
		t.Error("an empty conversation must be refused")
	}
	if _, err := c.Chat(context.Background(), "s", []Turn{{Role: "assistant", Text: "x"}}); err == nil {
		t.Error("a conversation ending with the assistant must be refused")
	}
}

func TestErrorsNeverContainTheKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // connection refused: the transport error includes the URL
	c := &Client{APIKey: "SECRET-KEY-VALUE", BaseURL: url + "/SECRET-KEY-VALUE"}
	_, err := c.Chat(context.Background(), "s", []Turn{{Role: "user", Text: "x"}})
	if err == nil || strings.Contains(err.Error(), "SECRET-KEY-VALUE") {
		t.Fatalf("error must exist and must not leak the key: %v", err)
	}
}

func TestChatWithImages(t *testing.T) {
	var seen struct{ path, key, body string }
	srv := standIn(t, 200, okReply, &seen)
	defer srv.Close()
	c := &Client{APIKey: "k-vision", Model: "gemini-flash", BaseURL: srv.URL}

	turns := []Turn{
		{
			Role: "user",
			Text: "What is this image?",
			Images: []Image{
				{
					MimeType: "image/png",
					Data:     "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==",
				},
			},
		},
	}
	got, err := c.Chat(context.Background(), "You analyze images.", turns)
	if err != nil {
		t.Fatalf("Chat with image failed: %v", err)
	}
	if got != "Not much, what's up?" {
		t.Errorf("got %q", got)
	}

	var sent struct {
		Contents []struct {
			Role  string `json:"role"`
			Parts []struct {
				Text       string `json:"text,omitempty"`
				InlineData *struct {
					MimeType string `json:"mimeType"`
					Data     string `json:"data"`
				} `json:"inlineData,omitempty"`
			} `json:"parts"`
		} `json:"contents"`
	}
	if err := json.Unmarshal([]byte(seen.body), &sent); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if len(sent.Contents) != 1 || len(sent.Contents[0].Parts) != 2 {
		t.Fatalf("expected 1 turn with 2 parts (text + image), got: %s", seen.body)
	}
	p0 := sent.Contents[0].Parts[0]
	p1 := sent.Contents[0].Parts[1]
	if p0.Text != "What is this image?" {
		t.Errorf("part 0 text mismatch: %v", p0.Text)
	}
	if p1.InlineData == nil || p1.InlineData.MimeType != "image/png" || p1.InlineData.Data == "" {
		t.Errorf("part 1 inlineData mismatch: %+v", p1.InlineData)
	}
}

func TestChatCanAskForJSONAtTemperatureZero(t *testing.T) {
	var seen struct{ path, key, body string }
	srv := standIn(t, 200, okReply, &seen)
	defer srv.Close()
	zero := 0.0
	c := &Client{APIKey: "k", Model: "m1", BaseURL: srv.URL, Temperature: &zero, JSONOutput: true}
	if _, err := c.Chat(context.Background(), "classify", []Turn{{Role: "user", Text: "x"}}); err != nil {
		t.Fatal(err)
	}
	var sent struct {
		GenerationConfig struct {
			Temperature      float64 `json:"temperature"`
			ResponseMIMEType string  `json:"responseMimeType"`
		} `json:"generationConfig"`
	}
	if err := json.Unmarshal([]byte(seen.body), &sent); err != nil {
		t.Fatal(err)
	}
	if sent.GenerationConfig.Temperature != 0 || sent.GenerationConfig.ResponseMIMEType != "application/json" {
		t.Fatalf("the classifier setting did not reach the request: %s", seen.body)
	}
	// The conversational default is untouched.
	c2 := &Client{APIKey: "k", Model: "m1", BaseURL: srv.URL}
	if _, err := c2.Chat(context.Background(), "chat", []Turn{{Role: "user", Text: "x"}}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(seen.body, "responseMimeType") || !strings.Contains(seen.body, `"temperature":0.7`) {
		t.Fatalf("default request changed: %s", seen.body)
	}
}

func TestAnOverloadedModelFallsBackToTheLighterOneOnce(t *testing.T) {
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path)
		if strings.Contains(r.URL.Path, DefaultFallbackModel) {
			_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"from the fallback"}]}}]}`))
			return
		}
		w.WriteHeader(503)
		_, _ = w.Write([]byte(`{"error":{"message":"high demand"}}`))
	}))
	defer srv.Close()
	c := &Client{APIKey: "k", BaseURL: srv.URL, Model: "gemini-flash-latest"}
	out, err := c.Chat(context.Background(), "s", []Turn{{Role: "user", Text: "x"}})
	if err != nil || out != "from the fallback" {
		t.Fatalf("%q %v", out, err)
	}
	if !strings.Contains(asked[len(asked)-1], DefaultFallbackModel) {
		t.Fatalf("the fallback is asked last: %v", asked)
	}
	asked = nil
	off := &Client{APIKey: "k", BaseURL: srv.URL, Model: "gemini-flash-latest", Fallback: "none"}
	if _, err := off.Chat(context.Background(), "s", []Turn{{Role: "user", Text: "x"}}); err == nil {
		t.Fatal("with the fallback off, the overload is reported")
	}
	for _, p := range asked {
		if strings.Contains(p, DefaultFallbackModel) {
			t.Fatal("no fallback when it is off")
		}
	}
}

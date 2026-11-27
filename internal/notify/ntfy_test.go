package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNtfyPublishesAJSONMessageWithTapAndListenAction(t *testing.T) {
	var got map[string]any
	var auth, ctype, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		auth, ctype, path = r.Header.Get("Authorization"), r.Header.Get("Content-Type"), r.URL.Path
	}))
	defer srv.Close()
	n := &Ntfy{Server: srv.URL + "/", Topic: "griffin-secret-topic", Token: "tk_abc"}
	err := n.Send(context.Background(), Message{Title: "JJ: done ✓", Body: "Your cover letter is ready.", ClickURL: "https://app.example/tasks/1", ListenURL: "https://app.example/listen/1", Priority: 4, Tags: []string{"white_check_mark"}})
	if err != nil {
		t.Fatal(err)
	}
	if path != "/" || ctype != "application/json" || auth != "Bearer tk_abc" {
		t.Errorf("path %q type %q auth %q", path, ctype, auth)
	}
	if got["topic"] != "griffin-secret-topic" || got["title"] != "JJ: done ✓" || got["click"] != "https://app.example/tasks/1" || got["priority"] != float64(4) {
		t.Errorf("a tap opens the task: %v", got)
	}
	actions, _ := got["actions"].([]any)
	if len(actions) != 1 {
		t.Fatalf("want one Listen action, got %v", got["actions"])
	}
	if a := actions[0].(map[string]any); a["label"] != "Listen" || a["url"] != "https://app.example/listen/1" {
		t.Errorf("the Listen action opens the listen page: %v", actions)
	}
	got = nil
	if err := n.Send(context.Background(), Message{Title: "t", Body: "b", ClickURL: "https://app.example/approvals?id=1"}); err != nil {
		t.Fatal(err)
	}
	if _, has := got["actions"]; has {
		t.Errorf("no Listen button without a listen page: %v", got)
	}
}

func TestNtfyLimitsLengthRefusesWithoutATopicAndNeverLeaksSecrets(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
	}))
	n := &Ntfy{Server: srv.URL, Topic: "t"}
	_ = n.Send(context.Background(), Message{Title: strings.Repeat("a", 500), Body: strings.Repeat("b", 2000)})
	srv.Close()
	if len([]rune(got["title"].(string))) > 120 || len([]rune(got["message"].(string))) > 500 {
		t.Error("a notification must stay short")
	}
	if err := (&Ntfy{}).Send(context.Background(), Message{Body: "x"}); err == nil {
		t.Error("no topic, no send")
	}
	// a server that is down: the error must not contain the topic or the token
	err := (&Ntfy{Server: srv.URL + "/SECRET-TOPIC", Topic: "SECRET-TOPIC", Token: "SECRET-TOKEN"}).Send(context.Background(), Message{Body: "x"})
	if err == nil || strings.Contains(err.Error(), "SECRET-TOPIC") || strings.Contains(err.Error(), "SECRET-TOKEN") {
		t.Errorf("errors must not leak secrets: %v", err)
	}
	// a rejected publish is reported
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403); _, _ = w.Write([]byte("forbidden")) }))
	defer bad.Close()
	if err := (&Ntfy{Server: bad.URL, Topic: "t"}).Send(context.Background(), Message{Body: "x"}); err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("a refusal must be an error: %v", err)
	}
}

func TestNtfyDelaysAFutureMessageButNotANearOne(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = nil
		_ = json.Unmarshal(b, &got)
	}))
	defer srv.Close()
	n := &Ntfy{Server: srv.URL, Topic: "t"}
	at := time.Now().Add(2 * time.Hour)
	if err := n.Send(context.Background(), Message{Title: "Reminder", Body: "b", At: at}); err != nil {
		t.Fatal(err)
	}
	if got["delay"] != strconv.FormatInt(at.Unix(), 10) {
		t.Errorf("a future message carries its delivery time: %v", got)
	}
	if err := n.Send(context.Background(), Message{Title: "Reminder", Body: "b", At: time.Now().Add(5 * time.Second)}); err != nil {
		t.Fatal(err)
	}
	if _, has := got["delay"]; has {
		t.Errorf("a message due now is not delayed: %v", got)
	}
}

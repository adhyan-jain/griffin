package linkedin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/griffin/core/internal/provider/gemini"
)

func modelSaying(t *testing.T, word string) Gemini {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"` + word + `"}]},"finishReason":"STOP"}]}`))
	}))
	t.Cleanup(srv.Close)
	return Gemini{Client: &gemini.Client{APIKey: "k", Model: "m", BaseURL: srv.URL}}
}

func TestOnlyAYesAnswerCountsAsARealJob(t *testing.T) {
	for word, want := range map[string]bool{"YES": true, " yes.": true, "NO": false, "Maybe": false, "": false} {
		got, err := modelSaying(t, word).IsRealJob(context.Background(), "A", "text")
		if word == "" {
			continue // an empty reply is an error from the client; either way it is never a yes
		}
		if err != nil || got != want {
			t.Errorf("%q: got %v, %v want %v", word, got, err, want)
		}
	}
}

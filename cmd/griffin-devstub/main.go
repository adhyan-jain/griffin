// griffin-devstub is a tiny local stand-in for Griffin's API, for trying the Android app on a real phone
// before anything is deployed: `adb reverse tcp:8787 tcp:8787`, then point the app at http://localhost:8787.
// It runs the real voice handler (so the request and reply shapes are the real ones) with Gemini when
// GEMINI_API_KEY is set, and a plain echo otherwise. It accepts any sign-in and keeps nothing.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/api/handlers"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/provider/gemini"
)

type name struct{ v string }

func (n *name) ManagerName(context.Context, uuid.UUID) (string, error) { return n.v, nil }
func (n *name) SetManagerName(_ context.Context, _ uuid.UUID, v string) error {
	n.v = v
	return nil
}

type echo struct{}

func (echo) Enabled() bool { return true }
func (echo) Chat(_ context.Context, _ string, turns []gemini.Turn) (string, error) {
	return "I heard: " + turns[len(turns)-1].Text, nil
}

func main() {
	addr := "127.0.0.1:8787"
	profiles := &name{v: envOr("STUB_NAME", "JJ")}
	var brain handlers.VoiceBrain = echo{}
	if key := os.Getenv("GEMINI_API_KEY"); key != "" {
		brain = &gemini.Client{APIKey: key}
		log.Println("voice answers come from Gemini")
	} else {
		log.Println("voice answers are echoed (set GEMINI_API_KEY for real ones)")
	}
	user := uuid.New()
	withUser := func(h http.HandlerFunc) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.Printf("%s %s auth=%v", r.Method, r.URL.Path, r.Header.Get("Authorization") != "")
			h(w, r.WithContext(context.WithValue(r.Context(), middleware.UserIDKey, user)))
		})
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/v1/token", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("sign-in (%s)", r.URL.Query().Get("grant_type"))
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "stub-access", "refresh_token": "stub-refresh", "expires_in": 3600})
	})
	mux.Handle("GET /api/v1/settings/manager-name", withUser(handlers.NewSettingsHandler(profiles).GetManagerName))
	mux.Handle("POST /api/v1/voice/turn", withUser(handlers.NewVoiceHandler(brain, profiles).PostTurn))
	mux.Handle("POST /api/v1/jj/messages", withUser(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		log.Printf("handed to the manager: %q (history %d turns)", body["message"], len(asSlice(body["history"])))
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"kind": "objective", "conversation_id": uuid.NewString()}})
	}))
	fmt.Println("griffin-devstub listening on", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func asSlice(v any) []any { s, _ := v.([]any); return s }

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

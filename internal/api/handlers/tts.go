package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/griffin/core/internal/service"
)

// TTSHandler handles neural voice synthesis and voice profile discovery.
type TTSHandler struct {
	tts *service.TTSService
}

// NewTTSHandler constructs a TTSHandler.
func NewTTSHandler(tts *service.TTSService) *TTSHandler {
	return &TTSHandler{tts: tts}
}

// Synthesize streams generated neural voice MP3 audio.
func (h *TTSHandler) Synthesize(w http.ResponseWriter, r *http.Request) {
	if h.tts == nil {
		http.Error(w, "tts service unavailable", http.StatusServiceUnavailable)
		return
	}

	text := strings.TrimSpace(r.URL.Query().Get("text"))
	if text == "" {
		http.Error(w, "missing 'text' query parameter", http.StatusBadRequest)
		return
	}

	voice := r.URL.Query().Get("voice")
	if voice == "" {
		voice = "ryan" // Default JARVIS voice persona
	}

	audio, err := h.tts.Synthesize(r.Context(), text, voice)
	if err != nil {
		http.Error(w, "synthesis error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(audio)
}

// Voices returns the list of auditioned neural voices.
func (h *TTSHandler) Voices(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"voices": service.AvailableAuditionVoices,
	})
}

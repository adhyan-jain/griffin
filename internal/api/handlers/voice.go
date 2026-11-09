package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/provider/gemini"
	"github.com/griffin/core/internal/service"
)

// VoiceBrain is the fast model that answers chat while the worker is still waking.
type VoiceBrain interface {
	Enabled() bool
	Chat(ctx context.Context, system string, turns []gemini.Turn) (string, error)
}

// VoiceHandler serves POST /api/v1/voice/turn. It never creates a task and never acts: it answers small
// talk at once and tells the caller when the request is real work, so the caller can post it to
// /api/v1/jj/messages with the conversation so far and the worker continues from there.
type VoiceHandler struct {
	brain    VoiceBrain
	profiles ProfileStore
}

// NewVoiceHandler constructs a VoiceHandler. brain may be nil: every turn then hands off.
func NewVoiceHandler(brain VoiceBrain, profiles ProfileStore) *VoiceHandler {
	return &VoiceHandler{brain: brain, profiles: profiles}
}

// VoiceTurnRequest is the POST body.
type VoiceTurnRequest struct {
	Text    string     `json:"text"`
	History []chatTurn `json:"history,omitempty"`
}

// VoiceTurnResponse says what to speak and whether the worker must take over.
type VoiceTurnResponse struct {
	// Kind is "wake" (only the name was said), "conversation" or "objective".
	Kind string `json:"kind"`
	// Reply is what to speak now. It can be empty when Handoff is true and nothing quick could be said.
	Reply string `json:"reply"`
	// Handoff means the caller must post Text and the conversation to /api/v1/jj/messages.
	Handoff     bool   `json:"handoff"`
	ManagerName string `json:"manager_name"`
	// Fallback explains why the fast path was skipped, for logs and the app's status line.
	Fallback string `json:"fallback,omitempty"`
}

const (
	voiceMaxChars   = 2000
	voiceBrainLimit = 12 * time.Second
)

// voiceSystem is the fast model's instruction. The name has been validated to letters, digits, spaces,
// hyphens and apostrophes, so it cannot carry an instruction.
func voiceSystem(name string) string {
	return "You are " + name + ", the owner's voice assistant inside Griffin, a personal AI workforce. " +
		"Reply in one or two short spoken sentences of plain text: no markdown, no lists, no emoji. " +
		"In this reply you cannot take actions, look anything up or open anything, so never say you have done something. " +
		"If the owner asks for work to be done, say you are getting started. Do not make up facts about the owner."
}

// PostTurn handles POST /api/v1/voice/turn.
func (h *VoiceHandler) PostTurn(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}
	var req VoiceTurnRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		httputil.ErrorJSON(w, badRequestFromDecode(err))
		return
	}
	text := strings.TrimSpace(req.Text)
	switch {
	case text == "":
		httputil.ErrorJSON(w, chatValidation("text is required"))
		return
	case len([]rune(text)) > voiceMaxChars:
		httputil.ErrorJSON(w, chatValidation("text must be at most %d characters", voiceMaxChars))
		return
	}
	history, err := normalizeHistory(req.History)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}

	name := ResolveManagerName(r.Context(), h.profiles, userID)
	out := VoiceTurnResponse{ManagerName: name}

	message := service.StripLeadingName(text, name)
	if message == "" {
		out.Kind, out.Reply = "wake", "Yes?"
		httputil.JSON(w, http.StatusOK, out)
		return
	}

	if service.ClassifyMessage(message) == service.MessageKindObjective {
		out.Kind, out.Reply, out.Handoff = "objective", "On it.", true
		httputil.JSON(w, http.StatusOK, out)
		return
	}

	out.Kind = "conversation"
	if h.brain == nil || !h.brain.Enabled() {
		out.Handoff, out.Fallback = true, "fast voice model is not configured"
		httputil.JSON(w, http.StatusOK, out)
		return
	}
	turns := make([]gemini.Turn, 0, len(history)+1)
	for _, t := range history {
		turns = append(turns, gemini.Turn{Role: t.Role, Text: t.Text})
	}
	turns = append(turns, gemini.Turn{Role: "user", Text: message})

	ctx, cancel := context.WithTimeout(r.Context(), voiceBrainLimit)
	defer cancel()
	reply, err := h.brain.Chat(ctx, voiceSystem(name), turns)
	if err != nil {
		// Never fail the owner's turn because the fast path did: the worker answers it instead.
		reason := "fast voice model failed"
		switch {
		case errors.Is(err, gemini.ErrRateLimited):
			reason = "fast voice model is rate limited"
		case errors.Is(err, gemini.ErrBlocked):
			reason = "fast voice model declined"
		}
		slog.Warn("voice: fast model unavailable; handing off to the worker", "user_id", userID, "reason", reason, "error", err)
		out.Handoff, out.Fallback = true, reason
		httputil.JSON(w, http.StatusOK, out)
		return
	}
	out.Reply = reply
	httputil.JSON(w, http.StatusOK, out)
}

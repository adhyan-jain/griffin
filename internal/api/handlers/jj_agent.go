package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/agent"
	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/service"
)

// ChatAgent is the tool-using agent the chat endpoint tries first. *agent.Agent implements it.
type ChatAgent interface {
	Run(ctx context.Context, in agent.Input) (*agent.Result, error)
	Undo(ctx context.Context, userID uuid.UUID, actionID string) (string, error)
}

// WithAgent lets chat answer with the tool-using agent; nil keeps the old path for every message.
func (h *JJChatHandler) WithAgent(a ChatAgent) *JJChatHandler {
	h.agent = a
	return h
}

// agentWants says whether the agent should take this message. Image messages, continuing a task thread and a
// "let's discuss first" / "go ahead" exchange keep their existing handling.
func agentWants(req JJMessageRequest, message string) bool {
	return len(req.Images) == 0 && req.ContinueTaskID == "" && !req.NewTask &&
		!service.DiscussFirst(message) && !service.GoAhead(message)
}

// runAgent answers the message with the agent and reports whether it did. On an agent error it logs and returns
// false so the old path handles the message unchanged. The origin is OwnerChat: it is the owner's own message.
func (h *JJChatHandler) runAgent(w http.ResponseWriter, r *http.Request, userID, conversationID uuid.UUID, message, managerName string, req JJMessageRequest, history []chatTurn) bool {
	var client []agent.HistoryTurn
	for _, t := range history {
		client = append(client, agent.HistoryTurn{Role: t.Role, Text: t.Text})
	}
	res, err := h.agent.Run(r.Context(), agent.Input{UserID: userID, ConversationID: conversationID, Message: message, Origin: agent.OriginOwnerChat, ClientHistory: client})
	if err != nil {
		slog.Warn("jj chat: the agent failed; using the normal path", "user_id", userID, "conversation_id", conversationID, "error", err)
		return false
	}
	if res.Wake && h.wakeup != nil {
		wakeCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		_ = h.wakeup.TriggerWakeup(wakeCtx) // best-effort: the work is durably queued and the tick retries
		cancel()
	}
	if res.Handoff != nil {
		h.postObjectiveIn(w, r, userID, conversationID, res.Handoff.Goal, req, history, managerName, nil, nil)
		return true
	}
	httputil.JSON(w, http.StatusOK, map[string]any{
		"kind":            string(service.MessageKindConversation),
		"conversation_id": conversationID.String(),
		"response":        res.Reply,
		"actions":         res.Actions,
	})
	return true
}

// AgentUndoHandler serves POST /api/v1/jj/undo/{action_id}: it reverses one agent action, once.
type AgentUndoHandler struct{ Agent ChatAgent }

// Undo handles the request.
func (h AgentUndoHandler) Undo(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}
	msg, err := h.Agent.Undo(r.Context(), userID, strings.TrimSpace(r.PathValue("action_id")))
	if err != nil {
		if err == agent.ErrNoUndo {
			httputil.ErrorJSON(w, domain.ErrNotFound)
			return
		}
		httputil.ErrorJSON(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"ok": true, "message": msg})
}

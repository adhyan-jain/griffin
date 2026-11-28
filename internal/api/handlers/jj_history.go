package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/agent"
	"github.com/griffin/core/internal/api/httputil"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
)

const (
	jjHistoryLimit = 30
	jjPreviewRunes = 120
)

// JJHistoryStore reads the owner's stored chats. *agent.PGStore implements it.
type JJHistoryStore interface {
	ListConversations(ctx context.Context, user uuid.UUID, limit int) ([]agent.ConversationSummary, error)
	ConversationTurns(ctx context.Context, user, conv uuid.UUID) ([]agent.Turn, bool, error)
}

// JJHistoryHandler serves the owner's past JJ conversations.
type JJHistoryHandler struct{ Store JJHistoryStore }

type jjConversationItem struct {
	ID        string    `json:"id"`
	UpdatedAt time.Time `json:"updated_at"`
	Preview   string    `json:"preview"`
}

type jjTurnItem struct {
	Role      string             `json:"role"`
	Text      string             `json:"text"`
	Actions   []agent.ActionView `json:"actions"`
	CreatedAt time.Time          `json:"created_at"`
}

func previewOf(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= jjPreviewRunes {
		return s
	}
	return string([]rune(s)[:jjPreviewRunes]) + "…"
}

// ListConversations handles GET /api/v1/jj/conversations.
func (h JJHistoryHandler) ListConversations(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}
	rows, err := h.Store.ListConversations(r.Context(), userID, jjHistoryLimit)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	items := make([]jjConversationItem, 0, len(rows))
	for _, c := range rows {
		items = append(items, jjConversationItem{ID: c.ID.String(), UpdatedAt: c.UpdatedAt, Preview: previewOf(c.Preview)})
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"conversations": items})
}

// ConversationTurns handles GET /api/v1/jj/conversations/{id}/turns.
func (h JJHistoryHandler) ConversationTurns(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrUnauthorized)
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httputil.ErrorJSON(w, domain.ErrNotFound)
		return
	}
	turns, found, err := h.Store.ConversationTurns(r.Context(), userID, id)
	if err != nil {
		httputil.ErrorJSON(w, err)
		return
	}
	if !found {
		httputil.ErrorJSON(w, domain.ErrNotFound)
		return
	}
	items := make([]jjTurnItem, 0, len(turns))
	for _, t := range turns {
		acts := make([]agent.ActionView, 0, len(t.Actions))
		for _, a := range t.Actions {
			acts = append(acts, a.View())
		}
		items = append(items, jjTurnItem{Role: t.Role, Text: t.Text, Actions: acts, CreatedAt: t.CreatedAt})
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"conversation_id": id.String(), "turns": items})
}

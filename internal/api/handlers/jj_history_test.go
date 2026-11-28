package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/agent"
	"github.com/griffin/core/internal/api/middleware"
)

type fakeHistory struct {
	owner uuid.UUID
	conv  uuid.UUID
}

func (f fakeHistory) ListConversations(_ context.Context, u uuid.UUID, limit int) ([]agent.ConversationSummary, error) {
	if u != f.owner {
		return nil, nil
	}
	return []agent.ConversationSummary{{ID: f.conv, UpdatedAt: time.Unix(1, 0), Preview: strings.Repeat("a ", 200)}}, nil
}

func (f fakeHistory) ConversationTurns(_ context.Context, u, c uuid.UUID) ([]agent.Turn, bool, error) {
	if u != f.owner || c != f.conv {
		return nil, false, nil
	}
	return []agent.Turn{
		{Role: "user", Text: "pause it"},
		{Role: "assistant", Text: "Paused.", Actions: []agent.Action{{ID: "a1", Tool: "pause_routine", Summary: "Paused", Undo: &agent.UndoRecord{Tool: "x"}}}},
	}, true, nil
}

func historyGet(t *testing.T, fn http.HandlerFunc, user uuid.UUID, path, id string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.SetPathValue("id", id)
	if user != uuid.Nil {
		req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, user))
	}
	rec := httptest.NewRecorder()
	fn(rec, req)
	return rec
}

func TestJJHistory_ListAndTurns(t *testing.T) {
	owner, conv := uuid.New(), uuid.New()
	h := JJHistoryHandler{Store: fakeHistory{owner, conv}}

	rec := historyGet(t, h.ListConversations, owner, "/api/v1/jj/conversations", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var list struct {
		Data struct {
			Conversations []jjConversationItem `json:"conversations"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	require.Len(t, list.Data.Conversations, 1)
	assert.Equal(t, conv.String(), list.Data.Conversations[0].ID)
	assert.LessOrEqual(t, len([]rune(list.Data.Conversations[0].Preview)), jjPreviewRunes+1)

	rec = historyGet(t, h.ConversationTurns, owner, "/x", conv.String())
	require.Equal(t, http.StatusOK, rec.Code)
	var turns struct {
		Data struct {
			Turns []jjTurnItem `json:"turns"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &turns))
	require.Len(t, turns.Data.Turns, 2)
	assert.Equal(t, "a1", turns.Data.Turns[1].Actions[0].UndoID)
	assert.Empty(t, turns.Data.Turns[0].Actions)
}

func TestJJHistory_OtherUserAndBadIDAre404_NoUserIs401(t *testing.T) {
	owner, conv := uuid.New(), uuid.New()
	h := JJHistoryHandler{Store: fakeHistory{owner, conv}}
	assert.Equal(t, http.StatusNotFound, historyGet(t, h.ConversationTurns, uuid.New(), "/x", conv.String()).Code)
	assert.Equal(t, http.StatusNotFound, historyGet(t, h.ConversationTurns, owner, "/x", "nope").Code)
	assert.Equal(t, http.StatusUnauthorized, historyGet(t, h.ConversationTurns, uuid.Nil, "/x", conv.String()).Code)
	assert.Equal(t, http.StatusUnauthorized, historyGet(t, h.ListConversations, uuid.Nil, "/x", "").Code)
}

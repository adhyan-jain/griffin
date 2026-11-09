package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/service"
)

func TestSpokenSummaryEndpoint(t *testing.T) {
	user := uuid.New()
	task := mk(user, "Draft a cover letter", "Dear team, I would love to join.", domain.TaskStatusCompleted, `{}`, 0)
	h := NewSpokenHandler(&fakeThreads{tasks: []*domain.Task{task}}, &service.Summarizer{}, &fakeProfiles{name: "Jarvis"})
	get := func(u uuid.UUID, id string) (int, map[string]any) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.SetPathValue("id", id)
		if u != uuid.Nil {
			req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, u))
		}
		rec := httptest.NewRecorder()
		h.Get(rec, req)
		var env struct{ Data map[string]any }
		_ = json.Unmarshal(rec.Body.Bytes(), &env)
		return rec.Code, env.Data
	}
	code, data := get(user, task.ID.String())
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "Dear team, I would love to join.", data["text"])
	assert.Equal(t, "answer", data["case"])
	assert.Equal(t, task.ID.String(), data["task_id"])

	code, _ = get(uuid.New(), task.ID.String())
	assert.Equal(t, http.StatusNotFound, code, "someone else's task is not found")
	code, _ = get(user, "nope")
	assert.Equal(t, http.StatusBadRequest, code)
	code, _ = get(uuid.Nil, task.ID.String())
	assert.Equal(t, http.StatusUnauthorized, code)
}

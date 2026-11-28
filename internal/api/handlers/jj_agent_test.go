package handlers

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/agent"
)

type fakeAgent struct {
	res *agent.Result
	err error
	got agent.Input
}

func (f *fakeAgent) Run(_ context.Context, in agent.Input) (*agent.Result, error) {
	f.got = in
	return f.res, f.err
}
func (f *fakeAgent) Undo(context.Context, uuid.UUID, string) (string, error) { return "", nil }

func TestJJChat_AgentAnswersInlineAsOwnerChat(t *testing.T) {
	h, store, _ := newChat(nil, false)
	fa := &fakeAgent{res: &agent.Result{Reply: "Paused.", Actions: []agent.ActionView{{Tool: "pause_routine", Summary: "Paused routine: Brief", UndoID: "u1"}}}}
	h.WithAgent(fa)
	code, env := sendChat(t, h, uuid.New(), `{"message":"pause the brief"}`, nil)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "conversation", env.Data.Kind)
	assert.Empty(t, store.tasks, "an inline agent answer queues no chat task")
	assert.Equal(t, agent.OriginOwnerChat, fa.got.Origin)
}

func TestJJChat_AgentErrorFallsBackToOldPath(t *testing.T) {
	h, store, _ := newChat(nil, false)
	h.WithAgent(&fakeAgent{err: errors.New("model down")})
	code, env := sendChat(t, h, uuid.New(), `{"message":"Hi JJ, how does leasing work?"}`, nil)
	require.Equal(t, http.StatusCreated, code)
	assert.Equal(t, "conversation", env.Data.Kind)
	require.Len(t, store.tasks, 1, "the old path still queues the chat task")
}

func TestJJChat_AgentHandoffUsesObjectivePath(t *testing.T) {
	h, _, obj := newChat(nil, false)
	h.WithAgent(&fakeAgent{res: &agent.Result{Handoff: &agent.Handoff{Goal: "check every listing link"}}})
	code, env := sendChat(t, h, uuid.New(), `{"message":"check all the links"}`, nil)
	require.Equal(t, http.StatusCreated, code)
	assert.Equal(t, "objective", env.Data.Kind)
	require.Equal(t, 1, obj.calls)
	assert.Equal(t, "check every listing link", obj.last.RequestText)
}

func TestJJChat_AgentSkippedForGoAhead(t *testing.T) {
	h, store, _ := newChat(nil, false)
	fa := &fakeAgent{res: &agent.Result{Reply: "x"}}
	h.WithAgent(fa)
	_, _ = sendChat(t, h, uuid.New(), `{"message":"go ahead"}`, nil)
	assert.Empty(t, fa.got.Message, "go ahead keeps the discussion path")
	_ = store
}

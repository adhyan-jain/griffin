package tests

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/agent"
	"github.com/griffin/core/internal/domain"
)

// The agent's store keeps a conversation, its turns and an undo that can be claimed exactly once.
func TestAgentStoreRoundTripAndUndoOnce(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	owner := CreateTestUser(t, env.DB, "agent-store@example.com")
	other := CreateTestUser(t, env.DB, "agent-store-other@example.com")
	st := agent.NewPGStore(env.DB.Pool)

	conv := &agent.Conversation{ID: uuid.New(), UserID: owner, Summary: "s"}
	conv.WorkingSet.IDs = []string{"a"}
	require.NoError(t, st.SaveConversation(ctx, conv))
	conv.Summary = "s2"
	require.NoError(t, st.SaveConversation(ctx, conv))
	got, err := st.GetConversation(ctx, owner, conv.ID)
	require.NoError(t, err)
	require.Equal(t, "s2", got.Summary)
	require.Equal(t, []string{"a"}, got.WorkingSet.IDs)
	none, err := st.GetConversation(ctx, other, conv.ID)
	require.NoError(t, err)
	require.Nil(t, none)

	actionID := uuid.NewString()
	require.NoError(t, st.AppendTurn(ctx, &agent.Turn{ID: uuid.New(), ConversationID: conv.ID, UserID: owner, Role: "user", Text: "hi"}))
	require.NoError(t, st.AppendTurn(ctx, &agent.Turn{ID: uuid.New(), ConversationID: conv.ID, UserID: owner, Role: "assistant", Text: "done",
		Actions: []agent.Action{{ID: actionID, Tool: "pause_routine", Effect: agent.EffectInternal, Summary: "Paused",
			Undo: &agent.UndoRecord{Tool: "pause_routine", Data: json.RawMessage(`{"x":1}`)}}}}))
	turns, err := st.RecentTurns(ctx, owner, conv.ID, 12)
	require.NoError(t, err)
	require.Len(t, turns, 2)
	require.Equal(t, "hi", turns[0].Text)
	after, err := st.TurnsAfter(ctx, owner, conv.ID, turns[0].Seq)
	require.NoError(t, err)
	require.Len(t, after, 1)

	_, ok, err := st.ClaimUndo(ctx, other, actionID)
	require.NoError(t, err)
	require.False(t, ok, "another user must not claim it")
	rec, ok, err := st.ClaimUndo(ctx, owner, actionID)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "pause_routine", rec.Tool)
	_, ok, err = st.ClaimUndo(ctx, owner, actionID)
	require.NoError(t, err)
	require.False(t, ok, "second claim must fail")
	require.NoError(t, st.ReleaseUndo(ctx, owner, actionID))
	_, ok, _ = st.ClaimUndo(ctx, owner, actionID)
	require.True(t, ok)

	id, err := st.AddMemory(ctx, owner, "likes tea", uuid.Nil)
	require.NoError(t, err)
	mem, _ := st.ListMemory(ctx, owner, 10)
	require.Len(t, mem, 1)
	require.NoError(t, st.SetMemoryDeleted(ctx, owner, id, true))
	mem, _ = st.ListMemory(ctx, owner, 10)
	require.Empty(t, mem)
	require.NoError(t, st.AddGap(ctx, owner, "book a flight"))
}

// An approved agent action runs its stored payload once and its holder task is finished, never queued.
func TestAgentActionApprovalRunsPayloadOnce(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	owner := CreateTestUser(t, env.DB, "agent-approval@example.com")

	var ran []string
	tool := &agent.Tool{Name: "send_probe", Description: "probe", Effect: agent.EffectExternal,
		Prepare: func(raw json.RawMessage) (json.RawMessage, string, error) { return raw, "probe", nil },
		Exec: func(_ context.Context, _ uuid.UUID, p json.RawMessage) (string, error) {
			ran = append(ran, string(p))
			return "ok", nil
		}}
	reg, err := agent.NewRegistry(tool)
	require.NoError(t, err)
	env.Engine.OnActionDecided(reg.Decided)

	ap, err := env.Engine.HoldAgentAction(ctx, owner, "send_probe", "Send probe", []byte(`{"to":"x"}`))
	require.NoError(t, err)
	require.Empty(t, ran)
	_, err = env.Engine.RespondApproval(ctx, ap.ID, owner, true, "ok")
	require.NoError(t, err)
	require.Len(t, ran, 1)
	require.JSONEq(t, `{"to":"x"}`, ran[0])
	task, err := env.Engine.GetTaskForUser(ctx, ap.TaskID, owner)
	require.NoError(t, err)
	require.Equal(t, domain.TaskStatusCompleted, task.Status)
	_, err = env.Engine.RespondApproval(ctx, ap.ID, owner, true, "again")
	require.Error(t, err)
	require.Len(t, ran, 1)
}

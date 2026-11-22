package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
)

type selfChatResp struct {
	Data struct {
		Kind     string `json:"kind"`
		Response string `json:"response"`
		TaskID   string `json:"task_id"`
	} `json:"data"`
}

func (e *chatEnv) say(t *testing.T, auth, msg string) (int, selfChatResp) {
	t.Helper()
	code, b := e.do(t, http.MethodPost, "/api/v1/jj/messages", auth, chatBody(msg, ""), nil)
	var r selfChatResp
	require.NoError(t, json.Unmarshal(b, &r), string(b))
	return code, r
}

// pauseForApproval creates an act-tier task and runs the worker once so it stops at its approval gate.
func pauseForApproval(t *testing.T, e *chatEnv, uid uuid.UUID, title string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	exec := &toolExecutor{fakeExecutor: fakeExecutor{output: json.RawMessage(`{"response":"done"}`)}, tools: true}
	task := actTask(uid)
	task.Title = title
	created, err := e.Engine.CreateTask(ctx, task, nil)
	require.NoError(t, err)
	processed, err := newTestWorker(e.TestEnv, "w-self-"+created.ID.String(), exec).ProcessNext(ctx)
	require.NoError(t, err)
	require.True(t, processed)
	got, err := e.Engine.GetTask(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, domain.TaskStatusAwaitingApproval, got.Status)
	return created.ID
}

func TestSelfCommand_ApproveAllPendingFromChat(t *testing.T) {
	e := newChatEnv(t, nil)
	ctx := context.Background()
	owner := CreateTestUser(t, e.DB, "self_owner@example.com")
	other := CreateTestUser(t, e.DB, "self_other@example.com")
	auth := GenerateTestJWT(owner, "self_owner@example.com")

	t1 := pauseForApproval(t, e, owner, "Apply to Acme internship")
	t2 := pauseForApproval(t, e, owner, "Message Coy on LinkedIn")
	t3 := pauseForApproval(t, e, other, "Apply to Zed internship")

	pending := domain.ApprovalStatusPending
	before, err := e.ApprovalRepo.ListApprovals(ctx, owner, &pending)
	require.NoError(t, err)
	require.Len(t, before, 2)

	code, r := e.say(t, auth, "approve all pending approvals")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "conversation", r.Data.Kind)
	assert.Empty(t, r.Data.TaskID, "no chat task is queued for a self command")
	assert.Contains(t, r.Data.Response, "Approved 2 pending approvals")

	left, err := e.ApprovalRepo.ListApprovals(ctx, owner, &pending)
	require.NoError(t, err)
	assert.Empty(t, left, "every pending approval of the owner is decided")
	approved := domain.ApprovalStatusApproved
	done, err := e.ApprovalRepo.ListApprovals(ctx, owner, &approved)
	require.NoError(t, err)
	assert.Len(t, done, 2)

	// the other user's approval is untouched
	theirs, err := e.ApprovalRepo.ListApprovals(ctx, other, &pending)
	require.NoError(t, err)
	assert.Len(t, theirs, 1)
	gotOther, _ := e.Engine.GetTask(ctx, t3)
	assert.Equal(t, domain.TaskStatusAwaitingApproval, gotOther.Status)

	// the owner's tasks were released, and each has an audit line
	for _, id := range []uuid.UUID{t1, t2} {
		got, _ := e.Engine.GetTask(ctx, id)
		assert.NotEqual(t, domain.TaskStatusAwaitingApproval, got.Status)
		assert.Equal(t, 1, e.count(t, `SELECT count(*) FROM task_events WHERE task_id = $1 AND message LIKE 'Owner chat command (approve_all_pending)%'`, id))
	}
	assert.Equal(t, 0, e.count(t, `SELECT count(*) FROM tasks WHERE title LIKE 'Chat:%'`), "no model task was queued")
	assert.Equal(t, int32(1), e.wake.calls.Load(), "the runner is woken once so approved work continues")

	// nothing left: say so
	_, r = e.say(t, auth, "approve everything")
	assert.Contains(t, r.Data.Response, "Nothing is waiting")
}

func TestSelfCommand_NamedApprovalAndAmbiguity(t *testing.T) {
	e := newChatEnv(t, nil)
	ctx := context.Background()
	owner := CreateTestUser(t, e.DB, "self_named@example.com")
	auth := GenerateTestJWT(owner, "self_named@example.com")
	pauseForApproval(t, e, owner, "Apply to Acme internship")
	pauseForApproval(t, e, owner, "Apply to Initech internship")

	pending := domain.ApprovalStatusPending
	_, r := e.say(t, auth, "approve the internship one")
	assert.Contains(t, r.Data.Response, "changed nothing")
	list, _ := e.ApprovalRepo.ListApprovals(ctx, owner, &pending)
	assert.Len(t, list, 2, "an ambiguous name changes nothing")

	_, r = e.say(t, auth, "reject the Initech one")
	assert.Contains(t, r.Data.Response, "Rejected")
	list, _ = e.ApprovalRepo.ListApprovals(ctx, owner, &pending)
	require.Len(t, list, 1)
	assert.Contains(t, list[0].Summary, "Acme")

	// a question is never an action
	code, r := e.say(t, auth, "should I approve all?")
	require.Equal(t, http.StatusCreated, code)
	assert.NotEmpty(t, r.Data.TaskID, "a question goes to the normal chat path")
	list, _ = e.ApprovalRepo.ListApprovals(ctx, owner, &pending)
	assert.Len(t, list, 1)
}

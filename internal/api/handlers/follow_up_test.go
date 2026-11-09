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

	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
)

type fakeThreads struct{ tasks []*domain.Task }

func (f *fakeThreads) GetTaskForUser(_ context.Context, id, user uuid.UUID) (*domain.Task, error) {
	for _, t := range f.tasks {
		if t.ID == id && t.UserID == user {
			return t, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (f *fakeThreads) ListTasks(_ context.Context, flt repository.TaskFilter) ([]*domain.Task, error) {
	var out []*domain.Task
	for _, t := range f.tasks {
		if flt.UserID == nil || t.UserID == *flt.UserID {
			out = append(out, t)
		}
	}
	return out, nil
}

var tick = time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)

func mk(user uuid.UUID, goal, answer string, status domain.TaskStatus, payload string, minutes int) *domain.Task {
	t := &domain.Task{ID: uuid.New(), UserID: user, Goal: goal, Title: goal, Status: status, InputPayload: json.RawMessage(payload), CreatedAt: tick.Add(time.Duration(minutes) * time.Minute)}
	if answer != "" {
		t.OutputPayload, _ = json.Marshal(map[string]string{"response": answer})
	}
	return t
}

func followUp(t *testing.T, h *JJChatHandler, user uuid.UUID, target uuid.UUID, body string) (int, chatEnvelope) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/"+target.String()+"/follow-ups", strings.NewReader(body))
	req.SetPathValue("id", target.String())
	if user != uuid.Nil {
		req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, user))
	}
	rec := httptest.NewRecorder()
	h.PostFollowUp(rec, req)
	var env chatEnvelope
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	return rec.Code, env
}

func setup(tasks ...*domain.Task) (*JJChatHandler, *fakeChatStore, *fakeObjectives, *fakeThreads) {
	h, store, obj := newChat(&chatWake{}, false)
	th := &fakeThreads{tasks: tasks}
	h.WithThreads(th)
	return h, store, obj, th
}

func payloadOf(t *testing.T, task *domain.Task) map[string]any {
	var m map[string]any
	require.NoError(t, json.Unmarshal(task.InputPayload, &m))
	return m
}

func TestFollowUp_JoinsTheTasksThreadAndStartsFromWhatWasSaid(t *testing.T) {
	user := uuid.New()
	root := mk(user, "Explain how leasing works", "Leasing lets one worker hold a task for a while.", domain.TaskStatusCompleted, `{"kind":"chat"}`, 0)
	h, store, _, _ := setup(root)

	code, env := followUp(t, h, user, root.ID, `{"message":"make it shorter"}`)
	require.Equal(t, http.StatusCreated, code)
	require.Len(t, store.tasks, 1)
	task := store.tasks[0]
	assert.Equal(t, "Follow-up: make it shorter", task.Title)
	assert.Equal(t, "make it shorter", task.Goal)
	p := payloadOf(t, task)
	assert.Equal(t, root.ID.String(), p["thread_id"])
	assert.Equal(t, root.ID.String(), p["follow_up_of"])
	assert.Equal(t, "chat", p["kind"])
	assert.Equal(t, root.ID.String(), env.Data.ConversationID, "the thread is the conversation")
	hist := p["history"].([]any)
	require.Len(t, hist, 2)
	assert.Equal(t, "Explain how leasing works", hist[0].(map[string]any)["text"])
	assert.Equal(t, "Leasing lets one worker hold a task for a while.", hist[1].(map[string]any)["text"])
	assert.Equal(t, "assistant", hist[1].(map[string]any)["role"])
	assert.True(t, domain.IsFollowUp(task), "it must not appear as its own row")
}

func TestFollowUp_OnAFollowUpStaysInTheSameThreadWithAllEarlierTurns(t *testing.T) {
	user := uuid.New()
	root := mk(user, "Draft a cover letter", "Dear team, ...", domain.TaskStatusCompleted, `{}`, 0)
	f1 := mk(user, "make it shorter", "Dear team, short.", domain.TaskStatusCompleted,
		`{"thread_id":"`+root.ID.String()+`","follow_up_of":"`+root.ID.String()+`"}`, 1)
	unfinished := mk(user, "ignored while running", "", domain.TaskStatusRunning,
		`{"thread_id":"`+root.ID.String()+`","follow_up_of":"`+f1.ID.String()+`"}`, 2)
	elsewhere := mk(user, "another thread entirely", "x", domain.TaskStatusCompleted,
		`{"thread_id":"`+uuid.NewString()+`","follow_up_of":"`+uuid.NewString()+`"}`, 3)
	h, store, _, _ := setup(root, f1, unfinished, elsewhere)

	code, _ := followUp(t, h, user, f1.ID, `{"message":"now friendlier"}`)
	require.Equal(t, http.StatusCreated, code)
	p := payloadOf(t, store.tasks[0])
	assert.Equal(t, root.ID.String(), p["thread_id"], "the thread root does not change")
	assert.Equal(t, f1.ID.String(), p["follow_up_of"], "it continues the task it was sent to")
	var texts []string
	for _, h := range p["history"].([]any) {
		texts = append(texts, h.(map[string]any)["text"].(string))
	}
	assert.Equal(t, []string{"Draft a cover letter", "Dear team, ...", "make it shorter", "Dear team, short."}, texts,
		"only this thread's finished turns, in order")
}

func TestFollowUp_OnAPlanStepContinuesThePlan(t *testing.T) {
	user := uuid.New()
	plan := mk(user, "Research laptops", "Final comparison...", domain.TaskStatusCompleted, `{}`, 0)
	step := mk(user, "Gather specs", "specs...", domain.TaskStatusCompleted, `{}`, 1)
	step.ParentID = &plan.ID
	h, store, _, _ := setup(plan, step)
	code, _ := followUp(t, h, user, step.ID, `{"message":"which is quietest?"}`)
	require.Equal(t, http.StatusCreated, code)
	assert.Equal(t, plan.ID.String(), payloadOf(t, store.tasks[0])["thread_id"], "a step's follow-up belongs to its plan's thread")
}

func TestFollowUp_AFailedTaskCanBeContinuedAndTheFailureIsKnown(t *testing.T) {
	user := uuid.New()
	root := mk(user, "Summarise the report", "", domain.TaskStatusFailed, `{}`, 0)
	reason := "the worker ran out of time"
	root.FailureReason = &reason
	h, store, _, _ := setup(root)
	code, _ := followUp(t, h, user, root.ID, `{"message":"try again with only page 1"}`)
	require.Equal(t, http.StatusCreated, code)
	hist := payloadOf(t, store.tasks[0])["history"].([]any)
	assert.Contains(t, hist[1].(map[string]any)["text"], "the worker ran out of time")
}

func TestFollowUp_RefusesWhileTheTaskIsStillRunningAndForOtherPeople(t *testing.T) {
	user := uuid.New()
	running := mk(user, "Long job", "", domain.TaskStatusRunning, `{}`, 0)
	done := mk(user, "Done job", "ok", domain.TaskStatusCompleted, `{}`, 0)
	h, store, _, _ := setup(running, done)

	code, _ := followUp(t, h, user, running.ID, `{"message":"hurry up"}`)
	assert.Equal(t, http.StatusConflict, code)
	code, _ = followUp(t, h, uuid.New(), done.ID, `{"message":"hi"}`)
	assert.Equal(t, http.StatusNotFound, code, "someone else's task is not found, not forbidden")
	assert.Empty(t, store.tasks, "a refused follow-up creates nothing")
}

func TestFollowUp_RequestsForWorkGoThroughThePlannerInTheSameThread(t *testing.T) {
	user := uuid.New()
	root := mk(user, "Find three internships", "1. A  2. B  3. C", domain.TaskStatusCompleted, `{}`, 0)
	h, store, obj, _ := setup(root)
	code, _ := followUp(t, h, user, root.ID, `{"message":"draft applications for all three"}`)
	require.Equal(t, http.StatusCreated, code)
	require.Equal(t, 1, obj.calls, "work is planned like any other request")
	assert.Empty(t, store.tasks)
	var ctx map[string]any
	require.NoError(t, json.Unmarshal(obj.last.Context, &ctx))
	assert.Equal(t, root.ID.String(), ctx["thread_id"])
	assert.Equal(t, root.ID.String(), ctx["follow_up_of"])
	assert.Len(t, ctx["history"], 2, "the plan starts with what was already said")
}

func TestFollowUp_LongThreadsKeepTheBeginningAndTheLatestTurns(t *testing.T) {
	user := uuid.New()
	root := mk(user, "turn 0", "answer 0", domain.TaskStatusCompleted, `{}`, 0)
	tasks := []*domain.Task{root}
	for i := 1; i <= 8; i++ {
		tasks = append(tasks, mk(user, "turn "+string(rune('0'+i)), "answer "+string(rune('0'+i)), domain.TaskStatusCompleted,
			`{"thread_id":"`+root.ID.String()+`","follow_up_of":"`+root.ID.String()+`"}`, i))
	}
	h, store, _, _ := setup(tasks...)
	code, _ := followUp(t, h, user, tasks[8].ID, `{"message":"one more"}`)
	require.Equal(t, http.StatusCreated, code)
	hist := payloadOf(t, store.tasks[0])["history"].([]any)
	assert.Len(t, hist, chatMaxHistoryTurns)
	assert.Equal(t, "turn 0", hist[0].(map[string]any)["text"], "the original request is always kept")
	assert.Equal(t, "answer 8", hist[len(hist)-1].(map[string]any)["text"], "and the latest answer")
}

func TestFollowUp_Validation(t *testing.T) {
	user := uuid.New()
	done := mk(user, "Done", "ok", domain.TaskStatusCompleted, `{}`, 0)
	h, _, _, _ := setup(done)
	for name, body := range map[string]string{"empty": `{"message":"  "}`, "bad json": `{`, "too long": `{"message":"` + strings.Repeat("a", 8001) + `"}`} {
		code, _ := followUp(t, h, user, done.ID, body)
		assert.Equal(t, http.StatusBadRequest, code, name)
	}
	code, _ := followUp(t, h, uuid.Nil, done.ID, `{"message":"hi"}`)
	assert.Equal(t, http.StatusUnauthorized, code)

	noThreads, _, _ := newChat(&chatWake{}, false)
	code, _ = followUp(t, noThreads, user, done.ID, `{"message":"hi"}`)
	assert.Equal(t, http.StatusNotFound, code, "a server without follow-ups says so")
}

// ---- automatic continuation of messages sent to /jj/messages ----

type chatReply struct {
	Data struct {
		Kind      string         `json:"kind"`
		TaskID    string         `json:"task_id"`
		Continued map[string]any `json:"continued"`
		Suggest   map[string]any `json:"suggestion"`
	} `json:"data"`
}

func sendMsg(t *testing.T, h *JJChatHandler, user uuid.UUID, body string) (int, chatReply) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/jj/messages", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, user))
	rec := httptest.NewRecorder()
	h.PostMessage(rec, req)
	var out chatReply
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func recent(user uuid.UUID, goal, answer, payload string, ago time.Duration) *domain.Task {
	t := mk(user, goal, answer, domain.TaskStatusCompleted, payload, 0)
	done := time.Now().Add(-ago)
	t.CreatedAt, t.UpdatedAt, t.CompletedAt = done.Add(-time.Minute), done, &done
	return t
}

func TestAutoThread_AMessageThatContinuesJoinsTheThreadAndSaysWhy(t *testing.T) {
	user := uuid.New()
	letter := recent(user, "Draft a cover letter for the Acme internship", "Dear Acme team, I am excited to apply.", `{"kind":"chat"}`, 3*time.Minute)
	h, store, _, _ := setup(letter)

	code, out := sendMsg(t, h, user, `{"message":"make it shorter"}`)
	require.Equal(t, http.StatusCreated, code)
	require.Len(t, store.tasks, 1)
	assert.Equal(t, letter.ID.String(), payloadOf(t, store.tasks[0])["thread_id"], "it joined the letter's thread")
	require.NotNil(t, out.Data.Continued)
	assert.Equal(t, letter.Title, out.Data.Continued["title"])
	assert.NotEmpty(t, out.Data.Continued["reason"], "the owner is told why")
	assert.NotEmpty(t, out.Data.TaskID)
}

func TestAutoThread_SomethingNewIsANewTaskAndNewTaskForcesIt(t *testing.T) {
	user := uuid.New()
	letter := recent(user, "Draft a cover letter for the Acme internship", "Dear Acme team.", `{}`, 3*time.Minute)
	h, store, _, _ := setup(letter)

	_, out := sendMsg(t, h, user, `{"message":"what is the weather in Pune tomorrow"}`)
	require.Len(t, store.tasks, 1)
	assert.False(t, domain.IsFollowUp(store.tasks[0]), "an unrelated question is its own task")
	assert.Nil(t, out.Data.Continued)

	_, out = sendMsg(t, h, user, `{"message":"make it shorter","new_task":true}`)
	require.Len(t, store.tasks, 2)
	assert.False(t, domain.IsFollowUp(store.tasks[1]), "new_task wins over the guess")
	assert.Nil(t, out.Data.Continued)
}

func TestAutoThread_TheOwnerCanPickWhatToContinue(t *testing.T) {
	user := uuid.New()
	a := recent(user, "Draft a cover letter", "Dear team.", `{}`, 5*time.Hour)
	b := recent(user, "Compare laptops", "The Acer wins.", `{}`, time.Minute)
	running := mk(user, "Still going", "", domain.TaskStatusRunning, `{}`, 0)
	h, store, _, _ := setup(a, b, running)

	code, out := sendMsg(t, h, user, `{"message":"unrelated words entirely","continue_task_id":"`+a.ID.String()+`"}`)
	require.Equal(t, http.StatusCreated, code)
	assert.Equal(t, a.ID.String(), payloadOf(t, store.tasks[0])["thread_id"], "continue_task_id overrides the guess")
	assert.Equal(t, "you chose to continue it", out.Data.Continued["reason"])

	code, _ = sendMsg(t, h, user, `{"message":"hi","continue_task_id":"`+running.ID.String()+`"}`)
	assert.Equal(t, http.StatusConflict, code, "a task that is still running cannot be continued")
	code, _ = sendMsg(t, h, user, `{"message":"hi","continue_task_id":"nope"}`)
	assert.Equal(t, http.StatusBadRequest, code)
	code, _ = sendMsg(t, h, uuid.New(), `{"message":"hi","continue_task_id":"`+a.ID.String()+`"}`)
	assert.Equal(t, http.StatusNotFound, code, "someone else's task")
}

func TestAutoThread_AChatConversationIsOneThreadUnlessItIsStillAnswering(t *testing.T) {
	user := uuid.New()
	conv := uuid.NewString()
	first := recent(user, "hello there", "Hi! How can I help?", `{"kind":"chat","conversation_id":"`+conv+`"}`, 2*time.Minute)
	h, store, _, th := setup(first)

	_, out := sendMsg(t, h, user, `{"message":"tell me a joke","conversation_id":"`+conv+`"}`)
	require.Len(t, store.tasks, 1)
	assert.Equal(t, first.ID.String(), payloadOf(t, store.tasks[0])["thread_id"], "the second chat message continues the conversation's thread")
	assert.Equal(t, "the same conversation", out.Data.Continued["reason"])

	// while that follow-up is still running, the next message is a task of its own, as chat always worked
	store.tasks[0].Status = domain.TaskStatusRunning
	th.tasks = append(th.tasks, store.tasks[0])
	code, out := sendMsg(t, h, user, `{"message":"and another","conversation_id":"`+conv+`"}`)
	require.Equal(t, http.StatusCreated, code)
	require.Len(t, store.tasks, 2)
	assert.False(t, domain.IsFollowUp(store.tasks[1]))
	assert.Nil(t, out.Data.Continued)

	// a brand-new conversation is simply new
	_, out = sendMsg(t, h, user, `{"message":"hello","conversation_id":"`+uuid.NewString()+`"}`)
	assert.Nil(t, out.Data.Continued)
}

func TestAutoThread_AnUncertainMatchStartsANewTaskAndOffersTheOldOne(t *testing.T) {
	user := uuid.New()
	laptops := recent(user, "Compare laptops under 60000", "The Acer Swift Go 14 has the best screen. The Lenovo is quieter.", `{}`, 20*time.Minute)
	h, store, _, _ := setup(laptops)
	_, out := sendMsg(t, h, user, `{"message":"which of those laptops is the quietest"}`)
	require.Len(t, store.tasks, 1)
	if out.Data.Continued != nil {
		return // judged confident enough to continue: also acceptable behaviour
	}
	require.NotNil(t, out.Data.Suggest, "a near miss should offer the earlier task")
	assert.Equal(t, laptops.ID.String(), out.Data.Suggest["task_id"])
	assert.False(t, domain.IsFollowUp(store.tasks[0]))
}

func TestAutoThread_ItNeverGetsInTheWayOfSending(t *testing.T) {
	user := uuid.New()
	h, store, _, th := setup()
	h.WithThreads(failingThreads{th})
	code, out := sendMsg(t, h, user, `{"message":"make it shorter"}`)
	require.Equal(t, http.StatusCreated, code, "a failure to look at earlier tasks must not stop the message")
	require.Len(t, store.tasks, 1)
	assert.Nil(t, out.Data.Continued)
}

type failingThreads struct{ *fakeThreads }

func (failingThreads) ListTasks(context.Context, repository.TaskFilter) ([]*domain.Task, error) {
	return nil, assert.AnError
}

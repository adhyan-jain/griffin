package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/runner"
)

type fakeChatStore struct {
	mu      sync.Mutex
	tasks   []*domain.Task
	byKey   map[string]*domain.Task
	failErr error
}

func (f *fakeChatStore) CreateTask(_ context.Context, t *domain.Task, _ []uuid.UUID) (*domain.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failErr != nil {
		return nil, f.failErr
	}
	if err := t.Validate(); err != nil {
		return nil, err
	}
	if t.IdempotencyKey != nil {
		if _, ok := f.byKey[*t.IdempotencyKey]; ok {
			return nil, domain.ErrIdempotentConflict
		}
	}
	c := *t
	c.ID = uuid.New()
	c.Status = domain.TaskStatusQueued
	f.tasks = append(f.tasks, &c)
	if c.IdempotencyKey != nil {
		if f.byKey == nil {
			f.byKey = map[string]*domain.Task{}
		}
		f.byKey[*c.IdempotencyKey] = &c
	}
	return &c, nil
}

func (f *fakeChatStore) GetTaskByIdempotencyKey(_ context.Context, _ uuid.UUID, key string) (*domain.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t, ok := f.byKey[key]; ok {
		return t, nil
	}
	return nil, domain.ErrNotFound
}

type fakeObjectives struct {
	calls    int
	runnable int
	err      error
	last     *domain.UserRequest
}

func (f *fakeObjectives) SubmitRequest(_ context.Context, req *domain.UserRequest) (*domain.OrchestrationRecord, *domain.OrchestrationPlan, error) {
	f.calls++
	f.last = req
	if f.err != nil {
		return nil, nil, f.err
	}
	return &domain.OrchestrationRecord{ID: uuid.New(), UserID: req.UserID}, &domain.OrchestrationPlan{ID: uuid.New()}, nil
}

func (f *fakeObjectives) RunnableChildCount(context.Context, uuid.UUID) (int, error) {
	return f.runnable, nil
}

type chatWake struct {
	calls int
	err   error
}

func (w *chatWake) TriggerWakeup(context.Context) error { w.calls++; return w.err }

type chatEnvelope struct {
	Success bool `json:"success"`
	Data    struct {
		Kind           string          `json:"kind"`
		ConversationID string          `json:"conversation_id"`
		TaskID         string          `json:"task_id"`
		Orchestration  json.RawMessage `json:"orchestration"`
		Plan           json.RawMessage `json:"plan"`
		Runner         struct {
			Wake   string `json:"wake"`
			Detail string `json:"detail"`
		} `json:"runner"`
	} `json:"data"`
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func sendChat(t *testing.T, h *JJChatHandler, user uuid.UUID, body string, hdr map[string]string) (int, chatEnvelope) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/jj/messages", strings.NewReader(body))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	if user != uuid.Nil {
		req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, user))
	}
	rec := httptest.NewRecorder()
	h.PostMessage(rec, req)
	var env chatEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env), rec.Body.String())
	return rec.Code, env
}

func newChat(wake RunnerWakeup, stz bool) (*JJChatHandler, *fakeChatStore, *fakeObjectives) {
	s, o := &fakeChatStore{}, &fakeObjectives{runnable: 2}
	return NewJJChatHandler(s, o, wake, stz), s, o
}

func TestJJChat_Unauthenticated(t *testing.T) {
	h, _, _ := newChat(nil, false)
	code, env := sendChat(t, h, uuid.Nil, `{"message":"hi"}`, nil)
	assert.Equal(t, http.StatusUnauthorized, code)
	assert.False(t, env.Success)
}

func TestJJChat_ConversationCreatesChatTaskAndWakesOnce(t *testing.T) {
	wake := &chatWake{}
	h, store, obj := newChat(wake, true)
	user := uuid.New()
	code, env := sendChat(t, h, user, `{"message":"Hi JJ, how does leasing work?"}`, nil)

	require.Equal(t, http.StatusCreated, code)
	assert.Equal(t, "conversation", env.Data.Kind)
	assert.Equal(t, 0, obj.calls, "no orchestration for conversation")
	assert.Equal(t, 1, wake.calls)
	assert.Equal(t, "signaled", env.Data.Runner.Wake)
	_, err := uuid.Parse(env.Data.ConversationID)
	require.NoError(t, err, "conversation id generated")

	require.Len(t, store.tasks, 1)
	task := store.tasks[0]
	assert.Equal(t, env.Data.TaskID, task.ID.String())
	assert.Equal(t, "JJ", task.ManagerDomain)
	assert.Nil(t, task.ParentID)
	assert.Equal(t, domain.TaskPriorityHighP1, task.Priority)
	assert.Equal(t, domain.DefaultMaxRetries, task.MaxRetries)
	assert.Equal(t, "Hi JJ, how does leasing work?", task.Goal)
	assert.Equal(t, "Chat: Hi JJ, how does leasing work?", task.Title)
	assert.Equal(t, user, task.UserID)

	var payload struct {
		Kind           string `json:"kind"`
		ConversationID string `json:"conversation_id"`
		History        []any  `json:"history"`
	}
	require.NoError(t, json.Unmarshal(task.InputPayload, &payload))
	assert.Equal(t, "chat", payload.Kind)
	assert.Equal(t, env.Data.ConversationID, payload.ConversationID)
	assert.NotNil(t, payload.History, "history is an array, never null")
}

func TestJJChat_TitleTruncatedTo60Chars(t *testing.T) {
	h, store, _ := newChat(nil, false)
	msg := strings.Repeat("é", 100)
	code, _ := sendChat(t, h, uuid.New(), `{"message":"`+msg+`"}`, nil)
	require.Equal(t, http.StatusCreated, code)
	assert.Equal(t, "Chat: "+strings.Repeat("é", 60), store.tasks[0].Title)
}

func TestJJChat_HistoryPreservedValidatedAndCapped(t *testing.T) {
	h, store, _ := newChat(nil, false)
	conv := uuid.New().String()
	var turns []string
	for i := 0; i < 14; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		turns = append(turns, fmt.Sprintf(`{"role":%q,"text":"turn-%d"}`, role, i))
	}
	turns = append(turns, `{"role":"assistant","text":"`+strings.Repeat("x", 2500)+`"}`)
	body := fmt.Sprintf(`{"message":"next","conversation_id":%q,"history":[%s]}`, conv, strings.Join(turns, ","))
	code, env := sendChat(t, h, uuid.New(), body, nil)
	require.Equal(t, http.StatusCreated, code)
	assert.Equal(t, conv, env.Data.ConversationID)

	var payload struct {
		ConversationID string `json:"conversation_id"`
		History        []struct{ Role, Text string }
	}
	require.NoError(t, json.Unmarshal(store.tasks[0].InputPayload, &payload))
	assert.Equal(t, conv, payload.ConversationID)
	require.Len(t, payload.History, 10, "capped to the last 10 turns")
	assert.Equal(t, "turn-5", payload.History[0].Text, "oldest turns are dropped")
	assert.Len(t, []rune(payload.History[9].Text), 2000, "turn text truncated")
}

func TestJJChat_ValidationErrors(t *testing.T) {
	h, store, _ := newChat(nil, false)
	user := uuid.New()
	cases := []struct {
		name, body, want string
	}{
		{"empty message", `{"message":""}`, "message is required"},
		{"blank message", `{"message":"   "}`, "message is required"},
		{"missing message", `{}`, "message is required"},
		{"too long", `{"message":"` + strings.Repeat("a", 8001) + `"}`, "8000"},
		{"bad conversation id", `{"message":"hi","conversation_id":"nope"}`, "conversation_id must be a UUID"},
		{"bad role", `{"message":"hi","history":[{"role":"system","text":"x"}]}`, `role "system"`},
		{"bad priority", `{"message":"hi","priority":"urgent"}`, "NORMAL_P2"},
		{"numeric priority", `{"message":"hi","priority":3}`, "priority must be a string"},
		{"long key", `{"message":"hi","idempotency_key":"` + strings.Repeat("k", 256) + `"}`, "idempotency_key"},
		{"bad json", `{`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, env := sendChat(t, h, user, tc.body, nil)
			assert.Equal(t, http.StatusBadRequest, code)
			assert.Equal(t, "VALIDATION_ERROR", env.Error.Code)
			assert.Contains(t, env.Error.Message, tc.want)
		})
	}
	assert.Empty(t, store.tasks, "nothing persisted for invalid requests")
}

func TestJJChat_ObjectiveRoutesToOrchestrator(t *testing.T) {
	wake := &chatWake{}
	h, store, obj := newChat(wake, true)
	code, env := sendChat(t, h, uuid.New(), `{"message":"Research three Go task queue libraries and compare them"}`, nil)
	require.Equal(t, http.StatusCreated, code)
	assert.Equal(t, "objective", env.Data.Kind)
	assert.Equal(t, 1, obj.calls)
	assert.Empty(t, store.tasks, "no chat task for objectives")
	assert.NotEmpty(t, env.Data.Orchestration)
	assert.NotEmpty(t, env.Data.Plan)
	assert.Equal(t, 1, wake.calls)
	assert.Equal(t, "signaled", env.Data.Runner.Wake)
	assert.NotEmpty(t, env.Data.ConversationID)
}

func TestJJChat_ObjectiveWithNothingRunnableDoesNotWake(t *testing.T) {
	wake := &chatWake{}
	h, _, obj := newChat(wake, true)
	obj.runnable = 0
	code, env := sendChat(t, h, uuid.New(), `{"message":"Research three Go task queue libraries and compare them"}`, nil)
	require.Equal(t, http.StatusCreated, code)
	assert.Equal(t, 0, wake.calls)
	assert.Equal(t, "skipped", env.Data.Runner.Wake)
}

func TestJJChat_WakeOutcomes(t *testing.T) {
	cases := []struct {
		name       string
		wake       RunnerWakeup
		scale      bool
		wantWake   string
		wantDetail string
	}{
		{"disabled by config", &chatWake{}, false, "disabled", ""},
		{"no controller", nil, true, "disabled", ""},
		{"generic failure", &chatWake{err: errors.New("dial tcp 10.0.0.1: secret-host refused")}, true, "failed", "could not signal"},
		{"capacity failure", &chatWake{err: errors.New("StartInstances: api error InsufficientInstanceCapacity: no capacity")}, true, "failed", "AWS has no capacity for the worker instance right now; it will retry"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, store, _ := newChat(tc.wake, tc.scale)
			code, env := sendChat(t, h, uuid.New(), `{"message":"hello"}`, nil)
			require.Equal(t, http.StatusCreated, code, "wake failure must never fail the request")
			assert.Equal(t, tc.wantWake, env.Data.Runner.Wake)
			assert.Contains(t, env.Data.Runner.Detail, tc.wantDetail)
			assert.NotContains(t, env.Data.Runner.Detail, "secret-host")
			assert.Len(t, store.tasks, 1, "task persisted")
		})
	}
}

type outcomeWake struct{ out runner.WakeOutcome }

func (o outcomeWake) TriggerWakeup(context.Context) error { return nil }
func (o outcomeWake) Wake(context.Context) (runner.WakeOutcome, error) {
	return o.out, nil
}

func TestJJChat_WakeSkippedWhenRunnerAlreadyActive(t *testing.T) {
	h, _, _ := newChat(outcomeWake{runner.WakeSkipped}, true)
	_, env := sendChat(t, h, uuid.New(), `{"message":"hello"}`, nil)
	assert.Equal(t, "skipped", env.Data.Runner.Wake)
}

func TestJJChat_IdempotencyKeyReturnsOriginalTask(t *testing.T) {
	wake := &chatWake{}
	h, store, _ := newChat(wake, true)
	user := uuid.New()
	body := `{"message":"hello there","idempotency_key":"abc-1"}`
	code1, e1 := sendChat(t, h, user, body, nil)
	require.Equal(t, http.StatusCreated, code1)
	code2, e2 := sendChat(t, h, user, body, nil)
	assert.Equal(t, http.StatusOK, code2)
	assert.Equal(t, e1.Data.TaskID, e2.Data.TaskID)
	assert.Equal(t, e1.Data.ConversationID, e2.Data.ConversationID)
	assert.Len(t, store.tasks, 1, "no second task")
	assert.Equal(t, 1, wake.calls, "duplicate does not wake again")

	// header form
	code3, e3 := sendChat(t, h, user, `{"message":"hello there"}`, map[string]string{"Idempotency-Key": "abc-1"})
	assert.Equal(t, http.StatusOK, code3)
	assert.Equal(t, e1.Data.TaskID, e3.Data.TaskID)
}

func TestJJChat_StoreErrorIsNotSwallowed(t *testing.T) {
	h, _, _ := newChat(nil, false)
	h.tasks.(*fakeChatStore).failErr = errors.New("db down")
	code, _ := sendChat(t, h, uuid.New(), `{"message":"hello"}`, nil)
	assert.Equal(t, http.StatusInternalServerError, code)
}

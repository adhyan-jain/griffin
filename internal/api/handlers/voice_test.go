package handlers

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/griffin/core/internal/provider/gemini"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/service"
)

type fakeProfiles struct {
	name   string
	getErr error
	setErr error
}

func (f *fakeProfiles) ManagerName(context.Context, uuid.UUID) (string, error) {
	return f.name, f.getErr
}
func (f *fakeProfiles) SetManagerName(_ context.Context, _ uuid.UUID, n string) error {
	if f.setErr == nil {
		f.name = n
	}
	return f.setErr
}

type fakeBrain struct {
	enabled bool
	reply   string
	err     error
	system  string
	turns   []gemini.Turn
	calls   int
}

func (f *fakeBrain) Enabled() bool { return f.enabled }
func (f *fakeBrain) Chat(_ context.Context, system string, turns []gemini.Turn) (string, error) {
	f.calls++
	f.system, f.turns = system, turns
	return f.reply, f.err
}

type voiceEnv struct {
	Data  VoiceTurnResponse     `json:"data"`
	Error struct{ Code string } `json:"error"`
}

func postVoice(t *testing.T, h *VoiceHandler, user uuid.UUID, body string) (int, voiceEnv) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/voice/turn", strings.NewReader(body))
	if user != uuid.Nil {
		req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, user))
	}
	rec := httptest.NewRecorder()
	h.PostTurn(rec, req)
	var env voiceEnv
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env), rec.Body.String())
	return rec.Code, env
}

func TestVoice_SmallTalkIsAnsweredByTheFastModelAsTheChosenManager(t *testing.T) {
	brain := &fakeBrain{enabled: true, reply: "Not much, you?"}
	h := NewVoiceHandler(brain, &fakeProfiles{name: "Jarvis"})
	code, env := postVoice(t, h, uuid.New(), `{"text":"hey Jarvis wassup","history":[{"role":"user","text":"hi"},{"role":"assistant","text":"hello"}]}`)

	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "conversation", env.Data.Kind)
	assert.Equal(t, "Not much, you?", env.Data.Reply)
	assert.False(t, env.Data.Handoff)
	assert.Equal(t, "Jarvis", env.Data.ManagerName)
	assert.Contains(t, brain.system, "You are Jarvis,")
	require.Len(t, brain.turns, 3)
	assert.Equal(t, "wassup", brain.turns[2].Text, "the name is not part of what was asked")
	assert.Equal(t, "user", brain.turns[2].Role)
}

func TestVoice_WorkIsHandedOffWithoutCallingTheFastModel(t *testing.T) {
	brain := &fakeBrain{enabled: true, reply: "should not be used"}
	h := NewVoiceHandler(brain, &fakeProfiles{})
	_, env := postVoice(t, h, uuid.New(), `{"text":"JJ research the best laptops under 60000 and compare them"}`)

	assert.Equal(t, "objective", env.Data.Kind)
	assert.True(t, env.Data.Handoff)
	assert.Equal(t, "On it.", env.Data.Reply)
	assert.Zero(t, brain.calls, "work must not be answered from a model that cannot act")
}

func TestVoice_OnlyTheNameMeansIAmListening(t *testing.T) {
	brain := &fakeBrain{enabled: true}
	h := NewVoiceHandler(brain, &fakeProfiles{name: "Jarvis"})
	_, env := postVoice(t, h, uuid.New(), `{"text":"Hey Jarvis"}`)
	assert.Equal(t, "wake", env.Data.Kind)
	assert.Equal(t, "Yes?", env.Data.Reply)
	assert.False(t, env.Data.Handoff)
	assert.Zero(t, brain.calls)
}

func TestVoice_FastModelFailureFallsBackToTheWorker(t *testing.T) {
	cases := map[string]error{
		"fast voice model is rate limited": gemini.ErrRateLimited,
		"fast voice model declined":        gemini.ErrBlocked,
		"fast voice model failed":          errors.New("boom"),
	}
	for want, brainErr := range cases {
		h := NewVoiceHandler(&fakeBrain{enabled: true, err: brainErr}, &fakeProfiles{})
		code, env := postVoice(t, h, uuid.New(), `{"text":"wassup"}`)
		require.Equal(t, http.StatusOK, code, "the owner's turn must not fail because the fast path did")
		assert.True(t, env.Data.Handoff)
		assert.Equal(t, want, env.Data.Fallback)
		assert.Empty(t, env.Data.Reply)
	}
	for name, brain := range map[string]VoiceBrain{"nil brain": nil, "no key": &fakeBrain{enabled: false}} {
		h := NewVoiceHandler(brain, nil)
		_, env := postVoice(t, h, uuid.New(), `{"text":"wassup"}`)
		assert.True(t, env.Data.Handoff, name)
		assert.Equal(t, "JJ", env.Data.ManagerName, "%s: no profile store means the default name", name)
	}
}

func TestVoice_ProfileFailureKeepsTheDefaultName(t *testing.T) {
	h := NewVoiceHandler(&fakeBrain{enabled: true, reply: "hey"}, &fakeProfiles{getErr: errors.New("db down")})
	_, env := postVoice(t, h, uuid.New(), `{"text":"wassup"}`)
	assert.Equal(t, "JJ", env.Data.ManagerName)
	assert.Equal(t, "hey", env.Data.Reply)
}

func TestVoice_Validation(t *testing.T) {
	h := NewVoiceHandler(&fakeBrain{enabled: true}, nil)
	for name, body := range map[string]string{
		"empty":    `{"text":"   "}`,
		"too long": `{"text":"` + strings.Repeat("a", 2001) + `"}`,
		"bad role": `{"text":"hi","history":[{"role":"system","text":"x"}]}`,
		"bad json": `{`,
	} {
		code, _ := postVoice(t, h, uuid.New(), body)
		assert.Equal(t, http.StatusBadRequest, code, name)
	}
	code, _ := postVoice(t, h, uuid.Nil, `{"text":"hi"}`)
	assert.Equal(t, http.StatusUnauthorized, code)
}

func TestSettings_ManagerNameRoundTrip(t *testing.T) {
	store := &fakeProfiles{}
	h := NewSettingsHandler(store)
	user := uuid.New()
	do := func(method string, fn http.HandlerFunc, body string) (int, map[string]any) {
		req := httptest.NewRequest(method, "/api/v1/settings/manager-name", strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, user))
		rec := httptest.NewRecorder()
		fn(rec, req)
		var env struct{ Data map[string]any }
		_ = json.Unmarshal(rec.Body.Bytes(), &env)
		return rec.Code, env.Data
	}

	code, data := do(http.MethodGet, h.GetManagerName, "")
	require.Equal(t, 200, code)
	assert.Equal(t, "JJ", data["manager_name"])
	assert.Equal(t, true, data["is_default"])

	code, data = do(http.MethodPut, h.PutManagerName, `{"manager_name":"  Mr   Stark "}`)
	require.Equal(t, 200, code)
	assert.Equal(t, "Mr Stark", data["manager_name"])
	assert.Equal(t, false, data["is_default"])
	assert.Equal(t, "Mr Stark", store.name)

	for _, bad := range []string{`{"manager_name":""}`, `{"manager_name":"x\ny"}`, `{"manager_name":"<b>"}`, `{`} {
		code, _ = do(http.MethodPut, h.PutManagerName, bad)
		assert.Equal(t, 400, code, bad)
	}
	assert.Equal(t, "Mr Stark", store.name, "a rejected name must not change the stored one")

	code, data = do(http.MethodPut, h.PutManagerName, `{"manager_name":"JJ"}`)
	require.Equal(t, 200, code)
	assert.Equal(t, true, data["is_default"], "sending the default name puts it back")
	_ = domain.DefaultManagerName
}

func TestJJChat_UsesAndCarriesTheManagerName(t *testing.T) {
	store := &fakeProfiles{name: "Jarvis"}
	h, tasks, obj := newChat(&chatWake{}, false)
	h.WithProfiles(store)
	user := uuid.New()

	code, _ := sendChat(t, h, user, `{"message":"Jarvis, how does leasing work?"}`, nil)
	require.Equal(t, http.StatusCreated, code)
	require.Len(t, tasks.tasks, 1)
	assert.Equal(t, "Jarvis, how does leasing work?", tasks.tasks[0].Goal, "the owner's own words are stored")
	assert.Equal(t, "Jarvis", domain.ManagerNameFrom(tasks.tasks[0].InputPayload))

	code, _ = sendChat(t, h, user, `{"message":"Jarvis research the best laptops and compare them","history":[{"role":"user","text":"hi"},{"role":"assistant","text":"hello"}]}`, nil)
	require.Equal(t, http.StatusCreated, code)
	require.NotNil(t, obj.last)
	assert.Equal(t, 1, obj.calls, "a request addressed to the chosen name is still classified as work")
	assert.Equal(t, "Jarvis research the best laptops and compare them", obj.last.RequestText)
	assert.Equal(t, "Jarvis", domain.ManagerNameFrom(obj.last.Context))
	var ctx struct{ History []chatTurn }
	require.NoError(t, json.Unmarshal(obj.last.Context, &ctx))
	require.Len(t, ctx.History, 2, "a conversation that became work carries what was already said")
	assert.Equal(t, "hello", ctx.History[1].Text)
}

// ---- task commands through the voice route ----

type voiceFakeStore struct {
	tasks   []*domain.Task
	deleted []uuid.UUID
}

func (f *voiceFakeStore) ListTasks(_ context.Context, flt repository.TaskFilter) ([]*domain.Task, error) {
	var out []*domain.Task
	for _, t := range f.tasks {
		if flt.UserID == nil || t.UserID == *flt.UserID {
			out = append(out, t)
		}
	}
	return out, nil
}
func (f *voiceFakeStore) CancelTask(context.Context, uuid.UUID, string) error { return nil }
func (f *voiceFakeStore) DeleteTask(_ context.Context, id uuid.UUID) error {
	f.deleted = append(f.deleted, id)
	return nil
}
func (f *voiceFakeStore) UpdateTask(context.Context, *domain.Task) error { return nil }
func (f *voiceFakeStore) ListApprovals(context.Context, uuid.UUID, *domain.ApprovalStatus) ([]*domain.Approval, error) {
	return nil, nil
}
func (f *voiceFakeStore) RespondApproval(context.Context, uuid.UUID, uuid.UUID, bool, string) (*domain.Approval, error) {
	return nil, nil
}

func postVoiceFull(t *testing.T, h *VoiceHandler, user uuid.UUID, body string) VoiceTurnResponse {
	t.Helper()
	code, env := postVoice(t, h, user, body)
	require.Equal(t, http.StatusOK, code)
	return env.Data
}

func TestVoice_TasksCanBeListedAndDeletedByVoiceWithAConfirmation(t *testing.T) {
	user := uuid.New()
	mk := func(title string, st domain.TaskStatus) *domain.Task {
		return &domain.Task{ID: uuid.New(), UserID: user, Title: title, Goal: title, Status: st, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	}
	store := &voiceFakeStore{tasks: []*domain.Task{mk("Cover letter", domain.TaskStatusCompleted), mk("Report", domain.TaskStatusCompleted), mk("Research", domain.TaskStatusRunning)}}
	brain := &fakeBrain{enabled: true, reply: "from the brain"}
	h := NewVoiceHandler(brain, &fakeProfiles{}).WithCommands(&service.VoiceCommands{Tasks: store, Approvals: store, Secret: []byte("k")})

	// reading is immediate and never reaches the model
	r := postVoiceFull(t, h, user, `{"text":"hey JJ list my tasks"}`)
	assert.Equal(t, "command", r.Kind)
	assert.Contains(t, r.Reply, "You have 3 tasks.")
	assert.Empty(t, r.ConfirmToken)
	assert.Zero(t, brain.calls)

	// a change asks first, and only on an unlocked device
	r = postVoiceFull(t, h, user, `{"text":"delete finished tasks"}`)
	assert.Contains(t, r.Reply, "Unlock your phone")
	r = postVoiceFull(t, h, user, `{"text":"delete finished tasks","unlocked":true}`)
	require.NotEmpty(t, r.ConfirmToken)
	assert.Equal(t, "Delete 2 finished tasks? Say yes to confirm.", r.Reply)
	assert.Empty(t, store.deleted, "nothing is deleted until the owner agrees")

	no := postVoiceFull(t, h, user, `{"confirm":{"token":"`+r.ConfirmToken+`","yes":false},"unlocked":true}`)
	assert.Equal(t, "Okay, I will leave it.", no.Reply)
	assert.Empty(t, store.deleted)
	yes := postVoiceFull(t, h, user, `{"confirm":{"token":"`+r.ConfirmToken+`","yes":true},"unlocked":true}`)
	assert.Equal(t, "Deleted 2.", yes.Reply)
	assert.Len(t, store.deleted, 2)

	// another person's token is useless to them
	r2 := postVoiceFull(t, h, user, `{"text":"delete finished tasks","unlocked":true}`)
	other := postVoiceFull(t, h, uuid.New(), `{"confirm":{"token":"`+r2.ConfirmToken+`","yes":true},"unlocked":true}`)
	assert.NotContains(t, other.Reply, "Deleted")
	assert.Len(t, store.deleted, 2)
}

func TestVoice_ASentenceThatOnlyLooksLikeATaskCommandCarriesOn(t *testing.T) {
	user := uuid.New()
	store := &voiceFakeStore{}
	brain := &fakeBrain{enabled: true, reply: "Sure."}
	h := NewVoiceHandler(brain, &fakeProfiles{}).WithCommands(&service.VoiceCommands{Tasks: store, Approvals: store})
	// "delete the moon" names none of their tasks: ordinary conversation, answered by the model
	r := postVoiceFull(t, h, user, `{"text":"delete the moon photos","unlocked":true}`)
	assert.Equal(t, "conversation", r.Kind)
	assert.Equal(t, "Sure.", r.Reply)
	assert.Equal(t, 1, brain.calls)
	// and without the commands wired, task words are just conversation too
	plain := NewVoiceHandler(&fakeBrain{enabled: true, reply: "ok"}, &fakeProfiles{})
	assert.Equal(t, "conversation", postVoiceFull(t, plain, user, `{"text":"list my tasks"}`).Kind)
}

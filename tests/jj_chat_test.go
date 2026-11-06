package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/api"
	"github.com/griffin/core/internal/domain"
)

type chatResp struct {
	Success bool `json:"success"`
	Data    struct {
		Kind           string `json:"kind"`
		ConversationID string `json:"conversation_id"`
		TaskID         string `json:"task_id"`
		Orchestration  struct {
			ID         string `json:"id"`
			RootTaskID string `json:"root_task_id"`
		} `json:"orchestration"`
		Runner struct {
			Wake   string `json:"wake"`
			Detail string `json:"detail"`
		} `json:"runner"`
	} `json:"data"`
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type chatEnv struct {
	*TestEnv
	ts   *httptest.Server
	wake *fakeWakeup
}

func newChatEnv(t *testing.T, wakeErr error) *chatEnv {
	t.Helper()
	env := SetupTestEnv(t)
	env.Config.ScaleToZeroEnabled = true
	wake := &fakeWakeup{err: wakeErr}
	server := api.NewServer(api.ServerDependencies{
		Config: env.Config, DB: env.DB, Engine: env.Engine, TaskRepo: env.TaskRepo,
		ApprovalRepo: env.ApprovalRepo, AgentRepo: env.AgentRepo, ArtifactRepo: env.ArtifactRepo,
		StorageService: env.StorageService, WorkerDefRepo: env.WorkerDefRepo, OrchRepo: env.OrchRepo,
		Orchestrator: env.Orchestrator, Factory: env.AgentFactory, ManagerRegistry: env.ManagerReg,
		Wakeup: wake,
	})
	ts := httptest.NewServer(server.Handler())
	t.Cleanup(func() { ts.Close(); env.Teardown() })
	return &chatEnv{TestEnv: env, ts: ts, wake: wake}
}

func (e *chatEnv) do(t *testing.T, method, path, auth, body string, hdr map[string]string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, e.ts.URL+path, bytes.NewBufferString(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := e.ts.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func (e *chatEnv) send(t *testing.T, auth, body string, hdr map[string]string) (int, chatResp) {
	t.Helper()
	code, b := e.do(t, http.MethodPost, "/api/v1/jj/messages", auth, body, hdr)
	var r chatResp
	require.NoError(t, json.Unmarshal(b, &r), string(b))
	return code, r
}

func (e *chatEnv) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, e.DB.Pool.QueryRow(context.Background(), q, args...).Scan(&n))
	return n
}

func chatBody(msg string, extra string) string {
	b, _ := json.Marshal(msg)
	if extra != "" {
		extra = "," + extra
	}
	return `{"message":` + string(b) + extra + `}`
}

func TestJJChat_ConversationsCreateChatTasksNotOrchestrations(t *testing.T) {
	e := newChatEnv(t, nil)
	uid := CreateTestUser(t, e.DB, "chat1@example.com")
	auth := GenerateTestJWT(uid, "chat1@example.com")

	for _, msg := range []string{
		"hi",
		"What is a goroutine?",
		"Explain what a Go worker does in three bullet points",
		"Hi JJ. Reply with a short greeting and tell me which execution provider you are currently using.",
	} {
		code, r := e.send(t, auth, chatBody(msg, ""), nil)
		require.Equal(t, http.StatusCreated, code, msg)
		assert.Equal(t, "conversation", r.Data.Kind, msg)
		assert.Equal(t, "signaled", r.Data.Runner.Wake)

		task, err := e.TaskRepo.GetTaskByIDAndUser(context.Background(), uuid.MustParse(r.Data.TaskID), uid)
		require.NoError(t, err)
		assert.Equal(t, domain.TaskStatusQueued, task.Status)
		assert.Equal(t, "JJ", task.ManagerDomain)
		assert.Nil(t, task.ParentID)
		assert.Equal(t, domain.TaskPriorityHighP1, task.Priority)
		assert.Equal(t, domain.DefaultMaxRetries, task.MaxRetries)
		assert.Equal(t, msg, task.Goal)
	}
	assert.Equal(t, 0, e.count(t, `SELECT count(*) FROM orchestrations`))
	assert.Equal(t, 4, e.count(t, `SELECT count(*) FROM tasks`))
	assert.Equal(t, int32(4), e.wake.calls.Load())
}

func TestJJChat_HistoryPreservedAndCapped(t *testing.T) {
	e := newChatEnv(t, nil)
	uid := CreateTestUser(t, e.DB, "chat2@example.com")
	auth := GenerateTestJWT(uid, "chat2@example.com")
	conv := uuid.New().String()

	var turns []map[string]string
	for i := 0; i < 13; i++ {
		role := []string{"user", "assistant"}[i%2]
		turns = append(turns, map[string]string{"role": role, "text": fmt.Sprintf("turn-%d", i)})
	}
	hb, _ := json.Marshal(turns)
	code, r := e.send(t, auth, chatBody("and then?", fmt.Sprintf(`"conversation_id":%q,"history":%s`, conv, hb)), nil)
	require.Equal(t, http.StatusCreated, code)
	assert.Equal(t, conv, r.Data.ConversationID)

	task, err := e.TaskRepo.GetTaskByIDAndUser(context.Background(), uuid.MustParse(r.Data.TaskID), uid)
	require.NoError(t, err)
	var p struct {
		Kind, ConversationID string `json:"-"`
		History              []struct{ Role, Text string }
	}
	require.NoError(t, json.Unmarshal(task.InputPayload, &p))
	require.Len(t, p.History, 10)
	assert.Equal(t, "turn-3", p.History[0].Text)
	assert.Equal(t, "turn-12", p.History[9].Text)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(task.InputPayload, &raw))
	assert.Equal(t, "chat", raw["kind"])
	assert.Equal(t, conv, raw["conversation_id"])
}

func TestJJChat_SequentialMessagesAreOrdered(t *testing.T) {
	e := newChatEnv(t, nil)
	uid := CreateTestUser(t, e.DB, "chat3@example.com")
	auth := GenerateTestJWT(uid, "chat3@example.com")
	conv := uuid.New().String()

	var ids []uuid.UUID
	for i := 0; i < 5; i++ {
		code, r := e.send(t, auth, chatBody(fmt.Sprintf("message number %d", i), fmt.Sprintf(`"conversation_id":%q`, conv)), nil)
		require.Equal(t, http.StatusCreated, code)
		assert.Equal(t, conv, r.Data.ConversationID)
		ids = append(ids, uuid.MustParse(r.Data.TaskID))
	}
	rows, err := e.DB.Pool.Query(context.Background(),
		`SELECT id FROM tasks WHERE input_payload->>'conversation_id' = $1 ORDER BY created_at, id`, conv)
	require.NoError(t, err)
	defer rows.Close()
	var got []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		require.NoError(t, rows.Scan(&id))
		got = append(got, id)
	}
	assert.Equal(t, ids, got, "tasks are created in submission order")
}

func TestJJChat_ConcurrentMessagesCreateDistinctTasks(t *testing.T) {
	e := newChatEnv(t, nil)
	uid := CreateTestUser(t, e.DB, "chat4@example.com")
	auth := GenerateTestJWT(uid, "chat4@example.com")
	conv := uuid.New().String()

	var wg sync.WaitGroup
	var mu sync.Mutex
	seen := map[string]bool{}
	var bad atomic.Int32
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodPost, e.ts.URL+"/api/v1/jj/messages",
				bytes.NewBufferString(chatBody(fmt.Sprintf("concurrent %d", i), fmt.Sprintf(`"conversation_id":%q`, conv))))
			req.Header.Set("Authorization", auth)
			resp, err := e.ts.Client().Do(req)
			if err != nil {
				bad.Add(1)
				return
			}
			defer resp.Body.Close()
			var r chatResp
			_ = json.NewDecoder(resp.Body).Decode(&r)
			if resp.StatusCode != http.StatusCreated {
				bad.Add(1)
				return
			}
			mu.Lock()
			seen[r.Data.TaskID] = true
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	require.Zero(t, bad.Load())
	assert.Len(t, seen, 10)
	assert.Equal(t, 10, e.count(t, `SELECT count(*) FROM tasks WHERE user_id = $1 AND input_payload->>'conversation_id' = $2`, uid, conv))
}

func TestJJChat_ObjectiveRoutesToManagerWithDependencies(t *testing.T) {
	e := newChatEnv(t, nil)
	uid := CreateTestUser(t, e.DB, "chat5@example.com")
	auth := GenerateTestJWT(uid, "chat5@example.com")

	code, r := e.send(t, auth, chatBody("Investigate and fix the bug in the codebase", ""), nil)
	require.Equal(t, http.StatusCreated, code)
	assert.Equal(t, "objective", r.Data.Kind)
	assert.NotEmpty(t, r.Data.ConversationID)
	assert.Equal(t, "signaled", r.Data.Runner.Wake)
	assert.Equal(t, int32(1), e.wake.calls.Load())

	orchID := uuid.MustParse(r.Data.Orchestration.ID)
	dels, err := e.OrchRepo.GetDelegationsByOrchestrationID(context.Background(), orchID, uid)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(dels), 2)
	deps := 0
	for _, d := range dels {
		ds, err := e.TaskRepo.GetDependencies(context.Background(), d.ChildTaskID)
		require.NoError(t, err)
		deps += len(ds)
	}
	assert.GreaterOrEqual(t, deps, 1, "the multi-step plan keeps its dependencies")
}

func TestJJChat_UnauthenticatedIs401(t *testing.T) {
	e := newChatEnv(t, nil)
	code, _ := e.do(t, http.MethodPost, "/api/v1/jj/messages", "", `{"message":"hi"}`, nil)
	assert.Equal(t, http.StatusUnauthorized, code)
	assert.Equal(t, 0, e.count(t, `SELECT count(*) FROM tasks`))
}

func TestJJChat_OtherUserCannotReadChatTask(t *testing.T) {
	e := newChatEnv(t, nil)
	owner := CreateTestUser(t, e.DB, "chat6a@example.com")
	other := CreateTestUser(t, e.DB, "chat6b@example.com")
	code, r := e.send(t, GenerateTestJWT(owner, "chat6a@example.com"), chatBody("my private question?", ""), nil)
	require.Equal(t, http.StatusCreated, code)

	path := "/api/v1/tasks/" + r.Data.TaskID
	c, _ := e.do(t, http.MethodGet, path, GenerateTestJWT(owner, "chat6a@example.com"), "", nil)
	assert.Equal(t, http.StatusOK, c)
	c, _ = e.do(t, http.MethodGet, path, GenerateTestJWT(other, "chat6b@example.com"), "", nil)
	assert.Equal(t, http.StatusNotFound, c)
}

func TestJJChat_WakeFailureStill201AndReportsReason(t *testing.T) {
	e := newChatEnv(t, errors.New("trigger runner start: aws: api error InsufficientInstanceCapacity: We currently do not have sufficient capacity"))
	uid := CreateTestUser(t, e.DB, "chat7@example.com")
	code, r := e.send(t, GenerateTestJWT(uid, "chat7@example.com"), chatBody("hello", ""), nil)
	require.Equal(t, http.StatusCreated, code)
	assert.Equal(t, "failed", r.Data.Runner.Wake)
	assert.Equal(t, "AWS has no capacity for the worker instance right now; it will retry", r.Data.Runner.Detail)
	assert.Equal(t, 1, e.count(t, `SELECT count(*) FROM tasks WHERE status = 'QUEUED'`), "task persisted and still queued")
}

func TestJJChat_IdempotencyKeyReturnsSameTaskOneRow(t *testing.T) {
	e := newChatEnv(t, nil)
	uid := CreateTestUser(t, e.DB, "chat8@example.com")
	auth := GenerateTestJWT(uid, "chat8@example.com")

	c1, r1 := e.send(t, auth, chatBody("hello once", `"idempotency_key":"k-1"`), nil)
	require.Equal(t, http.StatusCreated, c1)
	c2, r2 := e.send(t, auth, chatBody("hello once", ""), map[string]string{"Idempotency-Key": "k-1"})
	assert.Equal(t, http.StatusOK, c2)
	assert.Equal(t, r1.Data.TaskID, r2.Data.TaskID)
	assert.Equal(t, r1.Data.ConversationID, r2.Data.ConversationID)
	assert.Equal(t, 1, e.count(t, `SELECT count(*) FROM tasks`))
	assert.Equal(t, int32(1), e.wake.calls.Load(), "no duplicate wake")

	// a different user may reuse the key
	uid2 := CreateTestUser(t, e.DB, "chat8b@example.com")
	c3, r3 := e.send(t, GenerateTestJWT(uid2, "chat8b@example.com"), chatBody("hello once", `"idempotency_key":"k-1"`), nil)
	assert.Equal(t, http.StatusCreated, c3)
	assert.NotEqual(t, r1.Data.TaskID, r3.Data.TaskID)
}

func TestJJChat_InvalidInputIs400(t *testing.T) {
	e := newChatEnv(t, nil)
	uid := CreateTestUser(t, e.DB, "chat9@example.com")
	auth := GenerateTestJWT(uid, "chat9@example.com")

	code, r := e.send(t, auth, chatBody("hi", `"priority":"urgent"`), nil)
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Contains(t, r.Error.Message, "HIGH_P1")
	code, r = e.send(t, auth, `{"message":""}`, nil)
	assert.Equal(t, http.StatusBadRequest, code)
	code, _ = e.send(t, auth, chatBody("hi", `"conversation_id":"x"`), nil)
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Equal(t, 0, e.count(t, `SELECT count(*) FROM tasks`))
}

func TestJJChat_ChatTaskGETIsReadOnly(t *testing.T) {
	e := newChatEnv(t, nil)
	uid := CreateTestUser(t, e.DB, "chat10@example.com")
	auth := GenerateTestJWT(uid, "chat10@example.com")
	_, r := e.send(t, auth, chatBody("hi", ""), nil)

	paths := []string{"/api/v1/tasks/" + r.Data.TaskID, "/api/v1/tasks/" + r.Data.TaskID + "/events", "/api/v1/tasks"}
	for _, p := range paths {
		e.do(t, http.MethodGet, p, auth, "", nil)
	}
	before := snapshotTables(t, e.TestEnv)
	for _, p := range paths {
		for i := 0; i < 20; i++ {
			c, _ := e.do(t, http.MethodGet, p, auth, "", nil)
			require.Less(t, c, 500)
		}
	}
	assert.Equal(t, before, snapshotTables(t, e.TestEnv))
}

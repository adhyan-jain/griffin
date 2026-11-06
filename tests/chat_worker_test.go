package tests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/worker"
)

// fakeChatExecutor is a controllable provider: it echoes the task goal unless told otherwise.
type fakeChatExecutor struct {
	mode string // "echo", "empty", "denied"
}

func (f *fakeChatExecutor) Name() string                            { return "antigravity" }
func (f *fakeChatExecutor) IsHealthy(context.Context) (bool, error) { return true, nil }
func (f *fakeChatExecutor) RuntimeFacts() worker.RuntimeFacts {
	return worker.RuntimeFacts{Name: "Google Antigravity CLI (agy)", Version: "1.5.1", Model: "default"}
}
func (f *fakeChatExecutor) Execute(_ context.Context, task *domain.Task) (*worker.ExecutionResult, error) {
	switch f.mode {
	case "empty":
		return &worker.ExecutionResult{ExecutionID: uuid.New(), ProviderName: "antigravity", Status: "SUCCESS", TextOutput: "  \n"}, nil
	case "denied":
		err := errors.New("antigravity model attempted a tool the headless runner cannot approve (denied: RunCommand)")
		return &worker.ExecutionResult{ExecutionID: uuid.New(), ProviderName: "antigravity", Status: "FAILED", Error: err, Retryable: true}, err
	}
	time.Sleep(50 * time.Millisecond)
	return &worker.ExecutionResult{
		ExecutionID:  uuid.New(),
		ProviderName: "antigravity",
		Status:       "SUCCESS",
		TextOutput:   "echo: " + task.Goal,
		Output:       json.RawMessage(`{"response":"ignored"}`),
	}, nil
}

func newChatTask(t *testing.T, userID uuid.UUID, goal string, maxRetries int) *domain.Task {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"kind": "chat", "conversation_id": uuid.NewString(),
		"history": []map[string]string{{"role": "user", "text": "earlier"}},
	})
	require.NoError(t, err)
	return &domain.Task{UserID: userID, Title: "chat", Goal: goal, ManagerDomain: "JJ", InputPayload: payload, MaxRetries: maxRetries}
}

func newChatWorker(env *TestEnv, id string, ex worker.TaskExecutor) *worker.Worker {
	return worker.NewWorker(worker.Config{
		ID: id, Engine: env.Engine, TaskRepo: env.TaskRepo, Executor: ex,
		LeaseDuration: 10 * time.Second, HeartbeatInterval: time.Second, PollInterval: 50 * time.Millisecond,
	})
}

func TestChatTask_CompletesWithContractOutput(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "chat_ok@example.com")

	task, err := env.Engine.CreateTask(ctx, newChatTask(t, userID, "hello JJ", 3), nil)
	require.NoError(t, err)

	processed, err := newChatWorker(env, "w-chat", &fakeChatExecutor{mode: "echo"}).ProcessNext(ctx)
	require.NoError(t, err)
	require.True(t, processed)

	got, err := env.Engine.GetTask(ctx, task.ID)
	require.NoError(t, err)
	require.Equal(t, domain.TaskStatusCompleted, got.Status)
	assert.JSONEq(t, `{"response":"echo: hello JJ","provider":{"name":"antigravity","version":"1.5.1","model":"default"}}`, string(got.OutputPayload))
}

func TestChatTask_MockExecutorContract(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "chat_mock@example.com")

	task, err := env.Engine.CreateTask(ctx, newChatTask(t, userID, "hi", 3), nil)
	require.NoError(t, err)
	_, err = newChatWorker(env, "w-mock", worker.NewMockExecutor()).ProcessNext(ctx)
	require.NoError(t, err)

	got, err := env.Engine.GetTask(ctx, task.ID)
	require.NoError(t, err)
	var out struct {
		Response string `json:"response"`
		Provider struct{ Name, Version, Model string }
	}
	require.NoError(t, json.Unmarshal(got.OutputPayload, &out))
	assert.NotEmpty(t, out.Response)
	assert.Equal(t, "mock", out.Provider.Name)
}

func TestChatTask_EmptyOrDeniedRetriesThenFails(t *testing.T) {
	for _, mode := range []string{"empty", "denied"} {
		t.Run(mode, func(t *testing.T) {
			env := SetupTestEnv(t)
			defer env.Teardown()
			ctx := context.Background()
			userID := CreateTestUser(t, env.DB, fmt.Sprintf("chat_%s@example.com", mode))

			task, err := env.Engine.CreateTask(ctx, newChatTask(t, userID, "reviews on varkala", 2), nil)
			require.NoError(t, err)
			w := newChatWorker(env, "w-"+mode, &fakeChatExecutor{mode: mode})

			processed, err := w.ProcessNext(ctx)
			require.NoError(t, err)
			require.True(t, processed)
			got, err := env.Engine.GetTask(ctx, task.ID)
			require.NoError(t, err)
			assert.NotEqual(t, domain.TaskStatusCompleted, got.Status)
			assert.Equal(t, domain.TaskStatusRetrying, got.Status, "first failure must be retried")

			_, err = env.DB.Pool.Exec(ctx, `UPDATE tasks SET scheduled_for = NULL WHERE id = $1`, task.ID)
			require.NoError(t, err)
			processed, err = w.ProcessNext(ctx)
			require.NoError(t, err)
			require.True(t, processed)

			got, err = env.Engine.GetTask(ctx, task.ID)
			require.NoError(t, err)
			assert.Equal(t, domain.TaskStatusFailed, got.Status)
			require.NotNil(t, got.FailureReason)
			if mode == "denied" {
				assert.Contains(t, *got.FailureReason, "RunCommand")
			} else {
				assert.Contains(t, *got.FailureReason, "empty response")
			}
			assert.Empty(t, got.OutputPayload)
		})
	}
}

func TestChatTask_ConcurrentTasksNeverMixOutputs(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "chat_conc@example.com")

	var ids [2]uuid.UUID
	goals := [2]string{"question alpha", "question beta"}
	for i := range ids {
		task, err := env.Engine.CreateTask(ctx, newChatTask(t, userID, goals[i], 3), nil)
		require.NoError(t, err)
		ids[i] = task.ID
	}

	ex := &fakeChatExecutor{mode: "echo"}
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = newChatWorker(env, fmt.Sprintf("w-conc-%d", i), ex).ProcessNext(ctx)
		}(i)
	}
	wg.Wait()

	for i, id := range ids {
		got, err := env.Engine.GetTask(ctx, id)
		require.NoError(t, err)
		require.Equal(t, domain.TaskStatusCompleted, got.Status)
		var out struct{ Response string }
		require.NoError(t, json.Unmarshal(got.OutputPayload, &out))
		assert.Equal(t, "echo: "+goals[i], out.Response)
	}
}

func TestNonChatTask_OutputUnchanged(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "nonchat@example.com")

	task, err := env.Engine.CreateTask(ctx, &domain.Task{UserID: userID, Title: "t", Goal: "g"}, nil)
	require.NoError(t, err)
	_, err = newChatWorker(env, "w-nc", &fakeChatExecutor{mode: "echo"}).ProcessNext(ctx)
	require.NoError(t, err)
	got, err := env.Engine.GetTask(ctx, task.ID)
	require.NoError(t, err)
	assert.JSONEq(t, `{"response":"ignored"}`, string(got.OutputPayload))
}

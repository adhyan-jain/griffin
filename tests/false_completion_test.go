package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/api"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/worker"
)

// fakeExecutor returns a canned output for every task and records the task it was handed.
type fakeExecutor struct {
	mu     sync.Mutex
	output json.RawMessage
	err    error
	seen   []*domain.Task
}

func (f *fakeExecutor) Execute(_ context.Context, task *domain.Task) (*worker.ExecutionResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *task
	f.seen = append(f.seen, &cp)
	if f.err != nil {
		return &worker.ExecutionResult{Status: "FAILED", Error: f.err}, nil
	}
	return &worker.ExecutionResult{Status: "SUCCESS", Output: f.output, PromptTokens: 10, CompletionTokens: 5}, nil
}
func (f *fakeExecutor) Name() string                            { return "fake" }
func (f *fakeExecutor) IsHealthy(context.Context) (bool, error) { return true, nil }
func (f *fakeExecutor) lastTask() *domain.Task {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.seen) == 0 {
		return nil
	}
	return f.seen[len(f.seen)-1]
}

func newTestWorker(env *TestEnv, id string, exec worker.TaskExecutor) *worker.Worker {
	return worker.NewWorker(worker.Config{
		ID:                id,
		Engine:            env.Engine,
		TaskRepo:          env.TaskRepo,
		Executor:          exec,
		LeaseDuration:     10 * time.Second,
		HeartbeatInterval: time.Second,
		PollInterval:      100 * time.Millisecond,
	})
}

// makeRetryableNow pulls the retry backoff forward so the task can be claimed immediately.
func makeRetryableNow(t *testing.T, env *TestEnv, id uuid.UUID) {
	t.Helper()
	_, err := env.DB.Pool.Exec(context.Background(), `UPDATE tasks SET scheduled_for = now() - interval '1 second' WHERE id = $1`, id)
	require.NoError(t, err)
}

func TestWorkerEmptyOutputIsNeverCompleted(t *testing.T) {
	for name, out := range map[string]json.RawMessage{
		"nil":          nil,
		"empty object": json.RawMessage(`{}`),
		"null":         json.RawMessage(`null`),
		"empty string": json.RawMessage(`""`),
	} {
		t.Run(name, func(t *testing.T) {
			env := SetupTestEnv(t)
			defer env.Teardown()
			ctx := context.Background()
			userID := CreateTestUser(t, env.DB, "empty_out@example.com")
			exec := &fakeExecutor{output: out}
			w := newTestWorker(env, "w-empty", exec)

			task, err := env.Engine.CreateTask(ctx, &domain.Task{UserID: userID, Title: "t", Goal: "g", MaxRetries: 2}, nil)
			require.NoError(t, err)

			processed, err := w.ProcessNext(ctx)
			require.NoError(t, err)
			require.True(t, processed)

			got, err := env.Engine.GetTask(ctx, task.ID)
			require.NoError(t, err)
			assert.Equal(t, domain.TaskStatusRetrying, got.Status)
			require.NotNil(t, got.FailureReason)
			assert.Contains(t, *got.FailureReason, "no result")

			attempts, err := env.TaskRepo.GetAttempts(ctx, task.ID)
			require.NoError(t, err)
			require.Len(t, attempts, 1)
			assert.Equal(t, "FAILED", attempts[0].Status)
			require.NotNil(t, attempts[0].ErrorMessage)
			assert.Contains(t, *attempts[0].ErrorMessage, "no result")

			// Retry budget exhausted -> FAILED, never COMPLETED.
			makeRetryableNow(t, env, task.ID)
			processed, err = w.ProcessNext(ctx)
			require.NoError(t, err)
			require.True(t, processed)
			got, err = env.Engine.GetTask(ctx, task.ID)
			require.NoError(t, err)
			assert.Equal(t, domain.TaskStatusFailed, got.Status)
		})
	}
}

func TestWorkerNonEmptyOutputStillCompletes(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "nonempty_out@example.com")
	w := newTestWorker(env, "w-ok", &fakeExecutor{output: json.RawMessage(`{"response":"hello"}`)})

	task, err := env.Engine.CreateTask(ctx, &domain.Task{UserID: userID, Title: "t", Goal: "g"}, nil)
	require.NoError(t, err)
	processed, err := w.ProcessNext(ctx)
	require.NoError(t, err)
	require.True(t, processed)

	got, err := env.Engine.GetTask(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskStatusCompleted, got.Status)
	assert.JSONEq(t, `{"response":"hello"}`, string(got.OutputPayload))
}

func TestEngineCompleteTaskRejectsEmptyOutputForWorkers(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "engine_empty@example.com")

	for _, out := range []json.RawMessage{nil, json.RawMessage(`{}`), json.RawMessage(`null`), json.RawMessage(`""`), json.RawMessage(` {} `)} {
		task, err := env.Engine.CreateTask(ctx, &domain.Task{UserID: userID, Title: "t", Goal: "g"}, nil)
		require.NoError(t, err)
		claimed, err := env.Engine.ClaimTask(ctx, "w1", time.Minute)
		require.NoError(t, err)
		require.Equal(t, task.ID, claimed.ID)

		err = env.Engine.CompleteTask(ctx, task.ID, "w1", out)
		require.ErrorIs(t, err, domain.ErrValidation, "output %q", string(out))
		got, err := env.Engine.GetTask(ctx, task.ID)
		require.NoError(t, err)
		assert.Equal(t, domain.TaskStatusRunning, got.Status)
		require.NoError(t, env.Engine.CancelTask(ctx, task.ID, "cleanup"))
	}
}

func TestEngineCompleteTaskOrchestratorMayCompleteWithSummary(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "engine_jj@example.com")
	task, err := env.Engine.CreateTask(ctx, &domain.Task{UserID: userID, Title: "root", Goal: "g", Status: domain.TaskStatusRunning}, nil)
	require.NoError(t, err)
	require.NoError(t, env.Engine.CompleteTask(ctx, task.ID, "JJ_ORCHESTRATOR", json.RawMessage(`{"summary":"done"}`)))
}

func TestHTTPCompleteWithEmptyOutputIsRejected(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "http_empty@example.com")

	server := api.NewServer(api.ServerDependencies{
		Config: env.Config, DB: env.DB, Engine: env.Engine, TaskRepo: env.TaskRepo,
		ApprovalRepo: env.ApprovalRepo, AgentRepo: env.AgentRepo, ArtifactRepo: env.ArtifactRepo,
		StorageService: env.StorageService, WorkerDefRepo: env.WorkerDefRepo, OrchRepo: env.OrchRepo,
		Orchestrator: env.Orchestrator, Factory: env.AgentFactory, ManagerRegistry: env.ManagerReg,
	})
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	task, err := env.Engine.CreateTask(ctx, &domain.Task{UserID: userID, Title: "t", Goal: "g"}, nil)
	require.NoError(t, err)
	_, err = env.Engine.ClaimTask(ctx, "http-w", time.Minute)
	require.NoError(t, err)

	for _, body := range []string{``, `{}`, `{"output":{}}`, `{"output":null}`} {
		req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/v1/workers/%s/complete", ts.URL, task.ID), bytes.NewReader([]byte(body)))
		req.Header.Set("X-Worker-Key", TestWorkerKey)
		req.Header.Set("X-Worker-ID", "http-w")
		req.Header.Set("Content-Type", "application/json")
		resp, err := ts.Client().Do(req)
		require.NoError(t, err)
		resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "body %q", body)

		got, err := env.Engine.GetTask(ctx, task.ID)
		require.NoError(t, err)
		assert.Equal(t, domain.TaskStatusRunning, got.Status)
	}
}

// toolExecutor is a fake provider that can run tool tiers (or not), to exercise the act-approval gate.
type toolExecutor struct {
	fakeExecutor
	tools bool
}

func (t *toolExecutor) ToolsEnabled() bool { return t.tools }

func actTask(userID uuid.UUID) *domain.Task {
	return &domain.Task{UserID: userID, Title: "Apply to Acme internship", Goal: "Submit my application to Acme", MaxRetries: 2, InputPayload: json.RawMessage(`{"tool_mode":"act"}`)}
}

func TestActTier_PausesForApprovalThenRunsOnlyAfterApproval(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "act_gate@example.com")
	exec := &toolExecutor{fakeExecutor: fakeExecutor{output: json.RawMessage(`{"response":"applied"}`)}, tools: true}
	w := newTestWorker(env, "w-act", exec)

	task, err := env.Engine.CreateTask(ctx, actTask(userID), nil)
	require.NoError(t, err)

	// 1. First run: paused for approval, the provider never ran.
	processed, err := w.ProcessNext(ctx)
	require.NoError(t, err)
	require.True(t, processed)
	got, _ := env.Engine.GetTask(ctx, task.ID)
	assert.Equal(t, domain.TaskStatusAwaitingApproval, got.Status)
	assert.Nil(t, exec.lastTask(), "an act task must not reach the provider before approval")

	pending := domain.ApprovalStatusPending
	list, err := env.ApprovalRepo.ListApprovals(ctx, userID, &pending)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, domain.ToolApprovalAction, list[0].ActionType)
	assert.Equal(t, domain.ApprovalRiskHigh, list[0].RiskLevel)
	assert.Contains(t, list[0].Summary, "Apply to Acme internship")

	// 2. Nothing runs while it waits.
	processed, err = w.ProcessNext(ctx)
	require.NoError(t, err)
	assert.False(t, processed, "a task awaiting approval must not be claimed")
	assert.Nil(t, exec.lastTask())

	// 3. The user approves: now it runs and completes.
	_, err = env.Engine.RespondApproval(ctx, list[0].ID, userID, true, "go ahead")
	require.NoError(t, err)
	processed, err = w.ProcessNext(ctx)
	require.NoError(t, err)
	require.True(t, processed)
	require.NotNil(t, exec.lastTask(), "after approval the provider runs")
	got, _ = env.Engine.GetTask(ctx, task.ID)
	assert.Equal(t, domain.TaskStatusCompleted, got.Status)
}

func TestActTier_RejectedNeverRuns(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "act_reject@example.com")
	exec := &toolExecutor{fakeExecutor: fakeExecutor{output: json.RawMessage(`{"response":"applied"}`)}, tools: true}
	w := newTestWorker(env, "w-act-rej", exec)

	task, err := env.Engine.CreateTask(ctx, actTask(userID), nil)
	require.NoError(t, err)
	_, err = w.ProcessNext(ctx)
	require.NoError(t, err)

	pending := domain.ApprovalStatusPending
	list, _ := env.ApprovalRepo.ListApprovals(ctx, userID, &pending)
	require.Len(t, list, 1)
	_, err = env.Engine.RespondApproval(ctx, list[0].ID, userID, false, "no")
	require.NoError(t, err)

	processed, _ := w.ProcessNext(ctx)
	assert.False(t, processed)
	assert.Nil(t, exec.lastTask(), "a rejected act task must never run")
	got, _ := env.Engine.GetTask(ctx, task.ID)
	assert.Equal(t, domain.TaskStatusCancelled, got.Status)
}

func TestActTier_ToolsOffMeansNoGateAndNoTools(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "act_off@example.com")
	exec := &toolExecutor{fakeExecutor: fakeExecutor{output: json.RawMessage(`{"response":"text only"}`)}, tools: false}
	w := newTestWorker(env, "w-act-off", exec)

	task, err := env.Engine.CreateTask(ctx, actTask(userID), nil)
	require.NoError(t, err)
	processed, err := w.ProcessNext(ctx)
	require.NoError(t, err)
	require.True(t, processed)
	got, _ := env.Engine.GetTask(ctx, task.ID)
	assert.Equal(t, domain.TaskStatusCompleted, got.Status, "with tools off the task runs text-only, nothing to approve")
}

func TestActTier_OnlyTheOwnersApprovalCounts(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	owner := CreateTestUser(t, env.DB, "act_owner@example.com")
	other := CreateTestUser(t, env.DB, "act_other@example.com")
	task, err := env.Engine.CreateTask(ctx, actTask(owner), nil)
	require.NoError(t, err)

	ok, err := env.Engine.HasApprovedAction(ctx, task, domain.ToolApprovalAction)
	require.NoError(t, err)
	assert.False(t, ok)
	// A task of another user is evaluated against that user's approvals only.
	otherTask, err := env.Engine.CreateTask(ctx, actTask(other), nil)
	require.NoError(t, err)
	ok, _ = env.Engine.HasApprovedAction(ctx, otherTask, domain.ToolApprovalAction)
	assert.False(t, ok)
}

package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/api"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/runner"
	"github.com/griffin/core/internal/worker"
)

func TestScaleToZero_TaskSubmissionReturns202AndWakesRunner(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	userID := CreateTestUser(t, env.DB, "scale_to_zero_user@example.com")
	jwtToken := GenerateTestJWT(userID, "scale_to_zero_user@example.com")

	// Start with stopped runner
	mockMgr := runner.NewMockInstanceManager(runner.RunnerStateStopped)
	wakeupCtrl := runner.NewWakeupController(mockMgr, runner.WakeupConfig{
		CooldownWindow: 10 * time.Second,
	}, nil)

	// Configure server with ScaleToZeroEnabled: true
	env.Config.ScaleToZeroEnabled = true
	server := api.NewServer(api.ServerDependencies{
		Config:       env.Config,
		Logger:       nil,
		DB:           env.DB,
		Engine:       env.Engine,
		TaskRepo:     env.TaskRepo,
		ApprovalRepo: env.ApprovalRepo,
		AgentRepo:    env.AgentRepo,
		ArtifactRepo: env.ArtifactRepo,
		Wakeup:       wakeupCtrl,
	})

	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	client := &http.Client{Timeout: 5 * time.Second}

	// Submit task
	body := map[string]any{
		"title":          "Scale-to-zero probe task",
		"goal":           "Verify 202 Accepted and wakeup trigger",
		"manager_domain": "JJ",
		"priority":       "HIGH_P1",
	}
	payload, _ := json.Marshal(body)

	req, err := http.NewRequest("POST", ts.URL+"/api/v1/tasks", bytes.NewReader(payload))
	require.NoError(t, err)
	req.Header.Set("Authorization", jwtToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Must return 202 Accepted in scale-to-zero mode
	assert.Equal(t, http.StatusAccepted, resp.StatusCode, "Expected 202 Accepted for async runner wake-up")
	assert.Equal(t, "WAKEUP_SIGNALED", resp.Header.Get("X-Runner-Status"))

	var apiResp struct {
		Success bool        `json:"success"`
		Data    domain.Task `json:"data"`
	}
	err = json.NewDecoder(resp.Body).Decode(&apiResp)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskStatusQueued, apiResp.Data.Status)

	// Wait briefly for asynchronous wakeup goroutine to complete
	time.Sleep(50 * time.Millisecond)

	// Verify runner was started
	assert.Equal(t, 1, mockMgr.StartCalls)
	state, _ := mockMgr.GetRunnerState(context.Background())
	assert.Equal(t, runner.RunnerStateRunning, state)
}

func TestScaleToZero_SchedulerTickEndpoint(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	userID := CreateTestUser(t, env.DB, "scheduler_tick_user@example.com")

	mockMgr := runner.NewMockInstanceManager(runner.RunnerStateStopped)
	wakeupCtrl := runner.NewWakeupController(mockMgr, runner.WakeupConfig{
		CooldownWindow: 10 * time.Second,
	}, nil)

	env.Config.ScaleToZeroEnabled = true
	server := api.NewServer(api.ServerDependencies{
		Config:       env.Config,
		Logger:       nil,
		DB:           env.DB,
		Engine:       env.Engine,
		TaskRepo:     env.TaskRepo,
		ApprovalRepo: env.ApprovalRepo,
		AgentRepo:    env.AgentRepo,
		ArtifactRepo: env.ArtifactRepo,
		Wakeup:       wakeupCtrl,
	})

	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	client := &http.Client{Timeout: 5 * time.Second}

	// 1. Tick with 0 runnable tasks -> wakeupTriggered should be false
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/scheduler/tick", nil)
	req.Header.Set("X-Worker-API-Key", env.Config.WorkerAPIKey)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var tickResp struct {
		Success bool                 `json:"success"`
		Data    handlersTickResponse `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&tickResp)
	assert.False(t, tickResp.Data.WakeupTriggered)
	assert.Equal(t, 0, mockMgr.StartCalls)

	// 2. Create a runnable task directly
	_, err = env.Engine.CreateTask(context.Background(), &domain.Task{
		UserID: userID,
		Title:  "Scheduled Task",
		Goal:   "Run periodic task",
	}, nil)
	require.NoError(t, err)

	// Reset cooldown
	wakeupCtrl.ResetCooldown()

	// 3. Tick with 1 runnable task -> wakeupTriggered should be true
	req2, _ := http.NewRequest("POST", ts.URL+"/api/v1/scheduler/tick", nil)
	req2.Header.Set("X-Worker-API-Key", env.Config.WorkerAPIKey)

	resp2, err := client.Do(req2)
	require.NoError(t, err)
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)

	var tickResp2 struct {
		Success bool                 `json:"success"`
		Data    handlersTickResponse `json:"data"`
	}
	_ = json.NewDecoder(resp2.Body).Decode(&tickResp2)
	assert.True(t, tickResp2.Data.WakeupTriggered)
	assert.Equal(t, 1, mockMgr.StartCalls)
}

func TestScaleToZero_IdleShutdownAndDrainingWithRealDB(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "idle_shutdown_user@example.com")

	// Create and complete a task so queue becomes empty
	task, err := env.Engine.CreateTask(ctx, &domain.Task{
		UserID: userID,
		Title:  "Quick Task",
		Goal:   "Complete immediately",
	}, nil)
	require.NoError(t, err)

	claimed, err := env.Engine.ClaimTask(ctx, "worker-1", 10*time.Second)
	require.NoError(t, err)
	require.NotNil(t, claimed)

	err = env.Engine.CompleteTask(ctx, task.ID, "worker-1", json.RawMessage(`{"result":"done"}`))
	require.NoError(t, err)

	// Setup Worker and Drainer
	mockExec := worker.NewMockExecutor()
	w := worker.NewWorker(worker.Config{
		ID:       "worker-1",
		Engine:   env.Engine,
		TaskRepo: env.TaskRepo,
		Executor: mockExec,
	})
	workerPool := worker.NewWorkerPool(w)
	stopper := &runner.MockStopper{}

	// Monitor with fast 50ms idle timeout
	monitor := runner.NewIdleShutdownMonitor(
		env.TaskRepo,
		workerPool,
		stopper,
		runner.IdleShutdownConfig{
			IdleTimeout:   50 * time.Millisecond,
			CheckInterval: 10 * time.Millisecond,
		},
		nil,
	)

	// Check 1: enters IDLE
	stopped, err := monitor.CheckOnce(ctx)
	require.NoError(t, err)
	assert.False(t, stopped)
	assert.Equal(t, runner.MonitorStateIdle, monitor.State())

	// Wait for idle timeout
	time.Sleep(60 * time.Millisecond)

	// Check 2: drains workers, verifies 0 tasks, triggers stop
	stopped, err = monitor.CheckOnce(ctx)
	require.NoError(t, err)
	assert.True(t, stopped)
	assert.Equal(t, runner.MonitorStateStopped, monitor.State())
	assert.True(t, stopper.Stopped)
	assert.Equal(t, 1, stopper.StopCalls)
}

func TestScaleToZero_TaskArrivalDuringDrainingAbortsShutdownWithRealDB(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "shutdown_abort_user@example.com")

	mockExec := worker.NewMockExecutor()
	w := worker.NewWorker(worker.Config{
		ID:       "worker-1",
		Engine:   env.Engine,
		TaskRepo: env.TaskRepo,
		Executor: mockExec,
	})
	workerPool := worker.NewWorkerPool(w)
	stopper := &runner.MockStopper{}

	monitor := runner.NewIdleShutdownMonitor(
		env.TaskRepo,
		workerPool,
		stopper,
		runner.IdleShutdownConfig{
			IdleTimeout:   10 * time.Millisecond,
			CheckInterval: 5 * time.Millisecond,
		},
		nil,
	)

	// Step 1: Queue empty, enters IDLE
	_, _ = monitor.CheckOnce(ctx)
	time.Sleep(20 * time.Millisecond)

	// Step 2: Now create a task in the database before the next check completes
	_, err := env.Engine.CreateTask(ctx, &domain.Task{
		UserID: userID,
		Title:  "Urgent Task Arriving During Idle",
		Goal:   "Must prevent shutdown",
	}, nil)
	require.NoError(t, err)

	// Step 3: Run check -> monitor detects task arrived and aborts shutdown!
	stopped, err := monitor.CheckOnce(ctx)
	require.NoError(t, err)
	assert.False(t, stopped, "Must not stop when a task is runnable")
	assert.Equal(t, runner.MonitorStateActive, monitor.State(), "State must revert to ACTIVE")
	assert.False(t, stopper.Stopped)
	assert.Equal(t, 0, stopper.StopCalls)

	// Verify worker can process the urgent task
	processed, err := w.ProcessNext(ctx)
	require.NoError(t, err)
	assert.True(t, processed, "Worker should process the task")
}

type handlersTickResponse struct {
	EvaluatedAt     time.Time `json:"evaluated_at"`
	RunnableTasks   int       `json:"runnable_tasks"`
	WakeupTriggered bool      `json:"wakeup_triggered"`
}

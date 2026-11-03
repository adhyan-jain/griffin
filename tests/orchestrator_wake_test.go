package tests

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/api/handlers"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/runner"
	"github.com/griffin/core/internal/service"
)

type fakeWakeup struct {
	calls atomic.Int32
	err   error
}

func (f *fakeWakeup) TriggerWakeup(ctx context.Context) error {
	f.calls.Add(1)
	return f.err
}

func submitOrchestration(t *testing.T, h *handlers.OrchestratorHandler, userID uuid.UUID, text string) int {
	t.Helper()
	body := bytes.NewBufferString(`{"request_text":"` + text + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/requests", body)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rec := httptest.NewRecorder()
	h.SubmitRequest(rec, req)
	return rec.Code
}

func TestOrchestratorWake_RunnablePlanWakesRunnerOnce(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	userID := CreateTestUser(t, env.DB, "wake1@example.com")
	wake := &fakeWakeup{}
	h := handlers.NewOrchestratorHandler(env.Orchestrator, env.ManagerReg, wake, true)

	assert.Equal(t, http.StatusCreated, submitOrchestration(t, h, userID, "Research cgroup v2 limits"))
	assert.Equal(t, int32(1), wake.calls.Load())
}

func TestOrchestratorWake_DisabledDoesNotWake(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	userID := CreateTestUser(t, env.DB, "wake2@example.com")
	wake := &fakeWakeup{}
	h := handlers.NewOrchestratorHandler(env.Orchestrator, env.ManagerReg, wake, false)

	assert.Equal(t, http.StatusCreated, submitOrchestration(t, h, userID, "Research cgroup v2 limits"))
	assert.Equal(t, int32(0), wake.calls.Load())
}

func TestOrchestratorWake_RejectedPlanDoesNotWake(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	userID := CreateTestUser(t, env.DB, "wake3@example.com")
	bad := service.NewJJOrchestrator(env.Engine, env.OrchRepo, env.TaskRepo, env.ManagerReg, env.AgentFactory, &invalidMockPlanner{}, nil)
	wake := &fakeWakeup{}
	h := handlers.NewOrchestratorHandler(bad, env.ManagerReg, wake, true)

	assert.NotEqual(t, http.StatusCreated, submitOrchestration(t, h, userID, "cyclic"))
	assert.Equal(t, int32(0), wake.calls.Load())
}

func TestOrchestratorWake_FailureStillReturns201(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	userID := CreateTestUser(t, env.DB, "wake4@example.com")
	wake := &fakeWakeup{err: errors.New("ec2 unavailable")}
	h := handlers.NewOrchestratorHandler(env.Orchestrator, env.ManagerReg, wake, true)

	assert.Equal(t, http.StatusCreated, submitOrchestration(t, h, userID, "Research cgroup v2 limits"))
	assert.Equal(t, int32(1), wake.calls.Load())
}

func TestOrchestratorWake_RapidSubmissionsStartInstanceOnce(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	userID := CreateTestUser(t, env.DB, "wake5@example.com")
	mgr := runner.NewMockInstanceManager(runner.RunnerStateStopped)
	ctrl := runner.NewWakeupController(mgr, runner.WakeupConfig{CooldownWindow: time.Minute}, nil)
	h := handlers.NewOrchestratorHandler(env.Orchestrator, env.ManagerReg, ctrl, true)

	require.Equal(t, http.StatusCreated, submitOrchestration(t, h, userID, "Research one"))
	require.Equal(t, http.StatusCreated, submitOrchestration(t, h, userID, "Research two"))
	assert.Equal(t, 1, mgr.StartCalls)
}

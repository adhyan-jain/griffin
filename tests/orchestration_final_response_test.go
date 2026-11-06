package tests

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
)

func submitTwoStep(t *testing.T, env *TestEnv, email string) *domain.OrchestrationRecord {
	t.Helper()
	userID := CreateTestUser(t, env.DB, email)
	orch, _, err := env.Orchestrator.SubmitRequest(context.Background(), &domain.UserRequest{
		UserID:      userID,
		RequestText: "Research Linux kernel cgroup v2 limits",
		Priority:    domain.TaskPriorityNormalP2,
	})
	require.NoError(t, err)
	return orch
}

func claimAndComplete(t *testing.T, env *TestEnv, output string) {
	t.Helper()
	ctx := context.Background()
	claimed, err := env.Engine.ClaimTask(ctx, "w", 60*time.Second)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.NoError(t, env.Engine.CompleteTask(ctx, claimed.ID, "w", json.RawMessage(output)))
}

func TestFinalResponse_IncludesStepResponseText(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	orch := submitTwoStep(t, env, "final_text@example.com")

	claimAndComplete(t, env, `{"response":"Facts: cgroup v2 has a unified hierarchy.","provider":{"name":"antigravity"}}`)
	claimAndComplete(t, env, `{"status":"success","summary":"Verified."}`)

	got, err := env.Orchestrator.ReconcileOrchestration(ctx, orch.ID)
	require.NoError(t, err)
	require.Equal(t, domain.OrchestrationStatusCompleted, got.Status)
	require.NotNil(t, got.FinalResponse)
	assert.Contains(t, *got.FinalResponse, "cgroup v2 has a unified hierarchy")
	assert.Contains(t, *got.FinalResponse, `"summary":"Verified."`)
	assert.NotContains(t, *got.FinalResponse, "Summary of Deliverables")
	assert.LessOrEqual(t, len([]rune(*got.FinalResponse)), 6100)

	root, err := env.TaskRepo.GetTaskByID(ctx, orch.RootTaskID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskStatusCompleted, root.Status)
	var payload struct{ Summary string }
	require.NoError(t, json.Unmarshal(root.OutputPayload, &payload), "root output must be valid JSON")
	assert.Equal(t, *got.FinalResponse, payload.Summary)
}

func TestFinalResponse_EmptyCompletedStepIsNeverSuccess(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	orch := submitTwoStep(t, env, "final_empty@example.com")

	claimAndComplete(t, env, `{"response":"  "}`)
	claimAndComplete(t, env, `{"status":"success"}`)

	got, err := env.Orchestrator.ReconcileOrchestration(ctx, orch.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.OrchestrationStatusFailed, got.Status)
	require.NotNil(t, got.ErrorMessage)
	assert.Contains(t, *got.ErrorMessage, "empty output")
	assert.Nil(t, got.FinalResponse)
}

func TestFinalResponse_FailureNamesStepAndReason(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	orch := submitTwoStep(t, env, "final_fail@example.com")

	claimed, err := env.Engine.ClaimTask(ctx, "w", 60*time.Second)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.NoError(t, env.Engine.FailTask(ctx, claimed.ID, "w", "model attempted a tool (RunCommand)", false))

	got, err := env.Orchestrator.ReconcileOrchestration(ctx, orch.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.OrchestrationStatusFailed, got.Status)
	require.NotNil(t, got.ErrorMessage)
	assert.Contains(t, *got.ErrorMessage, claimed.Title)
	assert.Contains(t, *got.ErrorMessage, "RunCommand")
}

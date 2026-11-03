package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/api"
	"github.com/griffin/core/internal/domain"
)

var readonlyTables = []string{"tasks", "task_events", "task_attempts", "approvals", "artifacts", "orchestrations", "orchestration_plans", "delegation_records"}

// snapshotTables fingerprints every row (including updated_at) of the tables a GET could touch.
func snapshotTables(t *testing.T, env *TestEnv) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, tbl := range readonlyTables {
		var fp string
		q := fmt.Sprintf(`SELECT count(*)::text || ':' || coalesce(md5(string_agg(x::text, ',' ORDER BY x.id::text)), '') FROM %s x`, tbl)
		if err := env.DB.Pool.QueryRow(context.Background(), q).Scan(&fp); err != nil {
			t.Fatalf("snapshot %s: %v", tbl, err)
		}
		out[tbl] = fp
	}
	var maxEvent string
	require.NoError(t, env.DB.Pool.QueryRow(context.Background(), `SELECT coalesce(max(id::text), '') FROM task_events`).Scan(&maxEvent))
	out["task_events.max_id"] = maxEvent
	return out
}

// Every GET route the PWA polls must be a pure read: no inserts, updates, or events.
func TestGETRoutesAreReadOnly(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()

	userID := CreateTestUser(t, env.DB, "get_readonly@example.com")
	auth := GenerateTestJWT(userID, "get_readonly@example.com")

	orch, plan, err := env.Orchestrator.SubmitRequest(ctx, &domain.UserRequest{UserID: userID, RequestText: "Research read-only GETs"})
	require.NoError(t, err)
	dels, err := env.OrchRepo.GetDelegationsByOrchestrationID(ctx, orch.ID, userID)
	require.NoError(t, err)
	childID := dels[0].ChildTaskID

	// Populate attempts, approvals and artifacts so those routes return real rows.
	c, err := env.Engine.ClaimTask(ctx, "w1", 0)
	require.NoError(t, err)
	require.NotNil(t, c)
	require.NoError(t, env.TaskRepo.RecordAttempt(ctx, &domain.TaskAttempt{TaskID: childID, WorkerID: "w1", AttemptNumber: 1, Status: "RUNNING"}))
	approvalTask, err := env.Engine.CreateTask(ctx, &domain.Task{UserID: userID, Title: "needs approval", Goal: "g"}, nil)
	require.NoError(t, err)
	require.NoError(t, env.Engine.RequestApproval(ctx, approvalTask.ID, &domain.Approval{
		UserID: userID, TaskID: approvalTask.ID, ActionType: "x", RiskLevel: domain.ApprovalRiskLevel("LOW"),
		Summary: "s", ActionPayload: json.RawMessage(`{}`),
	}))
	_, err = env.ArtifactRepo.CreateArtifact(ctx, &domain.Artifact{
		UserID: userID, TaskID: &childID, Title: "a", ArtifactType: "FILE",
		StorageURI: "mock://a", StorageProvider: "MOCK", Metadata: json.RawMessage(`{}`),
	})
	require.NoError(t, err)

	// Settle: one GET of the orchestration may legitimately sync derived state once.
	server := api.NewServer(api.ServerDependencies{
		Config: env.Config, DB: env.DB, Engine: env.Engine, TaskRepo: env.TaskRepo,
		ApprovalRepo: env.ApprovalRepo, AgentRepo: env.AgentRepo, ArtifactRepo: env.ArtifactRepo,
		StorageService: env.StorageService, WorkerDefRepo: env.WorkerDefRepo, OrchRepo: env.OrchRepo,
		Orchestrator: env.Orchestrator, Factory: env.AgentFactory, ManagerRegistry: env.ManagerReg,
	})
	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	get := func(path string) int {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+path, nil)
		req.Header.Set("Authorization", auth)
		resp, err := ts.Client().Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode
	}

	routes := []string{
		"/api/v1/tasks",
		"/api/v1/tasks/" + childID.String(),
		"/api/v1/tasks/" + orch.RootTaskID.String(),
		"/api/v1/tasks/" + childID.String() + "/events",
		"/api/v1/tasks/" + childID.String() + "/attempts",
		"/api/v1/artifacts",
		"/api/v1/approvals",
		"/api/v1/managers",
		"/api/v1/agents",
		"/api/v1/workers/definitions",
		"/api/v1/runner/status",
		"/api/v1/system/status",
		"/api/v1/orchestrator/requests/" + orch.ID.String(),
		"/api/v1/orchestrator/plans/" + plan.ID.String(),
	}

	// Settling pass (first read of an orchestration may reconcile real child progress once).
	for _, p := range routes {
		get(p)
	}
	before := snapshotTables(t, env)

	for _, p := range routes {
		for i := 0; i < 50; i++ {
			code := get(p)
			require.Less(t, code, 500, "GET %s returned %d", p, code)
		}
		after := snapshotTables(t, env)
		assert.Equal(t, before, after, "GET %s mutated the database", p)
	}
}

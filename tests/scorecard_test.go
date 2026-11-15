package tests

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/api/handlers"
	"github.com/griffin/core/internal/api/middleware"
	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/worker"
)

// scriptedExecutor behaves per task title, so a 100-task run has a known mix of outcomes.
type scriptedExecutor struct {
	mu sync.Mutex
}

func (s *scriptedExecutor) Execute(_ context.Context, task *domain.Task) (*worker.ExecutionResult, error) {
	switch {
	case strings.HasPrefix(task.Title, "empty"):
		return &worker.ExecutionResult{Status: "SUCCESS", Output: json.RawMessage(`{}`)}, nil // claims success, nothing to show
	case strings.HasPrefix(task.Title, "boom"):
		return &worker.ExecutionResult{Status: "FAILED", Error: errors.New("provider exploded")}, nil
	}
	return &worker.ExecutionResult{Status: "SUCCESS", Output: json.RawMessage(`{"response":"done"}`)}, nil
}
func (s *scriptedExecutor) Name() string                            { return "scripted" }
func (s *scriptedExecutor) IsHealthy(context.Context) (bool, error) { return true, nil }
func (s *scriptedExecutor) ToolsEnabled() bool                      { return true }

// TestScorecardOverAHundredTasks runs 100 tasks through the real engine, worker and database, then checks the
// scorecard against the mix that was put in and the audit trail of an approved task.
func TestScorecardOverAHundredTasks(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "scorecard@example.com")
	other := CreateTestUser(t, env.DB, "scorecard-other@example.com")
	w := newTestWorker(env, "w-score", &scriptedExecutor{})

	// 70 plain successes, 10 false completions (must be caught), 10 hard failures, 10 that need approval (6 yes, 4 no).
	var approvedTask uuid.UUID
	mk := func(title, payload string, retries int) *domain.Task {
		in := json.RawMessage(`{}`)
		if payload != "" {
			in = json.RawMessage(payload)
		}
		task, err := env.Engine.CreateTask(ctx, &domain.Task{UserID: userID, Title: title, Goal: title + " goal", MaxRetries: retries, InputPayload: in}, nil)
		require.NoError(t, err)
		return task
	}
	for i := 0; i < 70; i++ {
		mk("ok", "", 1)
	}
	for i := 0; i < 10; i++ {
		mk("empty", "", 1)
	}
	for i := 0; i < 10; i++ {
		mk("boom", "", 1)
	}
	var acts []*domain.Task
	for i := 0; i < 10; i++ {
		acts = append(acts, mk("act", `{"tool_mode":"act"}`, 2))
	}
	// Someone else's task must never show up in this owner's numbers.
	_, err := env.Engine.CreateTask(ctx, &domain.Task{UserID: other, Title: "ok", Goal: "g"}, nil)
	require.NoError(t, err)

	pump := func() {
		for i := 0; i < 400; i++ {
			_, err := env.DB.Pool.Exec(ctx, `UPDATE tasks SET scheduled_for = now() - interval '1 second' WHERE status = 'RETRYING'`)
			require.NoError(t, err)
			processed, err := w.ProcessNext(ctx)
			require.NoError(t, err)
			if !processed {
				return
			}
		}
		t.Fatal("queue did not drain")
	}
	pump() // act tasks stop at their approval

	pending := domain.ApprovalStatusPending
	list, err := env.ApprovalRepo.ListApprovals(ctx, userID, &pending)
	require.NoError(t, err)
	require.Len(t, list, 10)
	for i, ap := range list {
		_, err := env.Engine.RespondApproval(ctx, ap.ID, userID, i < 6, "bench")
		require.NoError(t, err)
		if i == 0 {
			approvedTask = ap.TaskID
		}
	}
	pump()

	repo := repository.NewReportRepository(env.DB.Pool)
	rows, err := repo.ScoreRows(ctx, userID, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	sc := domain.ComputeScorecard(rows)

	assert.Equal(t, 100, sc.Finished)
	assert.Equal(t, 76, sc.Completed, "70 ok + 6 approved")
	assert.Equal(t, 20, sc.Failed, "10 caught false completions + 10 provider failures")
	assert.Equal(t, 4, sc.Cancelled, "4 rejected approvals")
	assert.Equal(t, 0, sc.FalseCompletions, "no empty result may ever be recorded as COMPLETED")
	assert.Equal(t, 10, sc.CaughtFalseCompletions)
	assert.Equal(t, 10, sc.AskedApproval)
	assert.Equal(t, 4, sc.Rejected)
	assert.InDelta(t, 76.0/96.0, sc.SuccessRate, 1e-9)
	assert.Equal(t, 0.0, sc.FalseCompletionRate)
	assert.InDelta(t, 0.10, sc.ApprovalRate, 1e-9)

	// The same numbers through the HTTP handler.
	h := handlers.NewReportHandler(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/scorecard?days=1&format=md", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, userID))
	rec := httptest.NewRecorder()
	h.Scorecard(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "| Tasks finished | 100 |")
	assert.Contains(t, rec.Body.String(), "False-completion rate | 0.0%")

	// Audit of an approved task: instruction first, request before its decision, chain intact, owner-only.
	entries, err := repo.Audit(ctx, userID, approvedTask)
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	assert.Equal(t, "instruction", entries[0].Kind)
	var req2, dec, res int = -1, -1, -1
	for _, e := range entries {
		switch e.Kind {
		case "approval_requested":
			req2 = e.Seq
		case "approval_decided":
			dec = e.Seq
			assert.Equal(t, "owner", e.Actor)
		case "result":
			res = e.Seq
		}
	}
	require.True(t, req2 >= 0 && dec > req2 && res > dec, "order must be request < decision < result: %d %d %d", req2, dec, res)
	assert.Equal(t, -1, domain.VerifyAudit(entries))
	_, err = repo.Audit(ctx, other, approvedTask)
	assert.ErrorIs(t, err, domain.ErrNotFound, "another owner must not read this trail")
	_ = acts
}

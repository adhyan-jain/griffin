package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/griffin/core/internal/domain"
	"github.com/griffin/core/internal/repository"
	"github.com/griffin/core/internal/worker"
)

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

type failingAttemptRepo struct {
	repository.TaskRepository
}

func (failingAttemptRepo) RecordAttempt(context.Context, *domain.TaskAttempt) error {
	return errors.New("attempt store down")
}

func TestWorkerLogsRecordAttemptFailureAndContinues(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "attempt_log@example.com")

	logs := &syncBuffer{}
	w := worker.NewWorker(worker.Config{
		ID:                "w-attempt",
		Engine:            env.Engine,
		TaskRepo:          failingAttemptRepo{env.TaskRepo},
		Executor:          &fakeExecutor{output: json.RawMessage(`{"response":"ok"}`)},
		LeaseDuration:     10 * time.Second,
		HeartbeatInterval: time.Second,
		Logger:            slog.New(slog.NewTextHandler(logs, nil)),
	})

	task, err := env.Engine.CreateTask(ctx, &domain.Task{UserID: userID, Title: "t", Goal: "g"}, nil)
	require.NoError(t, err)

	processed, err := w.ProcessNext(ctx)
	require.NoError(t, err)
	require.True(t, processed)

	got, err := env.Engine.GetTask(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.TaskStatusCompleted, got.Status)
	assert.Contains(t, logs.String(), "RecordAttempt failed")
	assert.Contains(t, logs.String(), "attempt store down")
}

func TestNewWorkerWarnsWhenTaskRepoNil(t *testing.T) {
	logs := &syncBuffer{}
	worker.NewWorker(worker.Config{ID: "w-nil", Logger: slog.New(slog.NewTextHandler(logs, nil))})
	assert.Contains(t, logs.String(), "no TaskRepo")
}

func TestWorker_RecordsRichAttemptTelemetry(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()
	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "attempt_telemetry@example.com")

	zeroExit := 0
	termSuccess := "completed"
	w := worker.NewWorker(worker.Config{
		ID:       "w-telemetry",
		Engine:   env.Engine,
		TaskRepo: env.TaskRepo,
		Executor: &fakeExecutor{
			name:   "antigravity",
			output: json.RawMessage(`{"response":"telemetry ok"}`),
			result: &worker.ExecutionResult{
				ProviderName:      "antigravity",
				Model:             "gemini-2.5-pro",
				ExitCode:          &zeroExit,
				TerminationReason: &termSuccess,
				Usage: &worker.UsageMetadata{
					InputTokens:  120,
					OutputTokens: 45,
					TotalTokens:  165,
				},
			},
		},
		LeaseDuration:     10 * time.Second,
		HeartbeatInterval: time.Second,
	})

	task, err := env.Engine.CreateTask(ctx, &domain.Task{
		UserID:       userID,
		Title:        "Tailor resume",
		Goal:         "Tailor resume for role",
		InputPayload: json.RawMessage(`{"provider_hint":"claude"}`),
	}, nil)
	require.NoError(t, err)

	processed, err := w.ProcessNext(ctx)
	require.NoError(t, err)
	require.True(t, processed)

	attempts, err := env.TaskRepo.GetAttempts(ctx, task.ID)
	require.NoError(t, err)
	require.NotEmpty(t, attempts)
	latest := attempts[len(attempts)-1]

	assert.Equal(t, "claude", latest.PlannedProvider)
	assert.Equal(t, "antigravity", latest.ActualProvider)
	assert.Equal(t, "gemini-2.5-pro", latest.Model)
	require.NotNil(t, latest.ExitCode)
	assert.Equal(t, 0, *latest.ExitCode)
	require.NotNil(t, latest.TerminationReason)
	assert.Equal(t, "completed", *latest.TerminationReason)
	assert.NotEmpty(t, latest.Usage)
}

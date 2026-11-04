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

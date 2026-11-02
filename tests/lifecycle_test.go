package tests

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/griffin/core/internal/api"
	"github.com/griffin/core/internal/domain"
)

func TestGracefulShutdown(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	server := api.NewServer(api.ServerDependencies{
		Config:       env.Config,
		Logger:       nil,
		DB:           env.DB,
		Engine:       env.Engine,
		TaskRepo:     env.TaskRepo,
		ApprovalRepo: env.ApprovalRepo,
		AgentRepo:    env.AgentRepo,
		ArtifactRepo: env.ArtifactRepo,
	})

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Start()
	}()

	// Wait briefly for server to bind
	time.Sleep(100 * time.Millisecond)

	// Verify server responds
	resp, err := http.Get("http://localhost:8081/healthz")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("server should be healthy, got %v", err)
	}
	resp.Body.Close()

	// Trigger graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown error: %v", err)
	}

	// Verify server stopped
	select {
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("unexpected server exit error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not exit within timeout")
	}
}

func TestDatabaseTransactionRollbackOnFailure(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()
	userID := CreateTestUser(t, env.DB, "tx_rollback@example.com")
	taskID := uuid.New()

	simulatedErr := errors.New("simulated database failure during transaction")

	// Execute WithTx where an error occurs mid-transaction
	err := env.DB.WithTx(ctx, func(tx pgx.Tx) error {
		query := `
			INSERT INTO tasks (id, user_id, title, goal, manager_domain, status, priority)
			VALUES ($1, $2, 'Transient Task', 'Should be rolled back', 'JJ', 'QUEUED', 'NORMAL_P2')
		`
		if _, err := tx.Exec(ctx, query, taskID, userID); err != nil {
			return err
		}
		// Return error to trigger rollback
		return simulatedErr
	})

	if !errors.Is(err, simulatedErr) {
		t.Fatalf("expected simulatedErr, got: %v", err)
	}

	// Verify task was rolled back and does NOT exist
	_, err = env.Engine.GetTask(ctx, taskID)
	if err == nil {
		t.Fatal("task was committed despite transaction failure!")
	}
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for rolled back task, got: %v", err)
	}
}

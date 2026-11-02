package tests

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/griffin/core/internal/domain"
)

func TestRowLevelSecurityIsolation(t *testing.T) {
	env := SetupTestEnv(t)
	defer env.Teardown()

	ctx := context.Background()

	// 1. Create two separate users
	userA := CreateTestUser(t, env.DB, "user_a@example.com")
	userB := CreateTestUser(t, env.DB, "user_b@example.com")

	// 2. Insert tasks for User A directly via engine
	taskA, err := env.Engine.CreateTask(ctx, &domain.Task{
		UserID: userA,
		Title:  "User A Private Task",
		Goal:   "Confidential user A operations",
	}, nil)
	if err != nil {
		t.Fatalf("failed to create task for User A: %v", err)
	}

	// 3. Connect as User B with RLS session variable `request.jwt.claim.sub` set
	conn, err := env.DB.Pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("failed to acquire connection: %v", err)
	}
	defer conn.Release()

	// Ensure non-superuser test role exists for RLS testing
	roleSetup := `
		DO $$ BEGIN
			IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'griffin_test_user') THEN
				CREATE ROLE griffin_test_user LOGIN PASSWORD 'test';
			END IF;
			GRANT USAGE ON SCHEMA public, auth TO griffin_test_user;
			GRANT ALL ON ALL TABLES IN SCHEMA public TO griffin_test_user;
			GRANT ALL ON ALL SEQUENCES IN SCHEMA public TO griffin_test_user;
			GRANT griffin_test_user TO postgres;
		END $$;
	`
	if _, err := env.DB.Pool.Exec(ctx, roleSetup); err != nil {
		t.Fatalf("failed to setup test role: %v", err)
	}

	// Run within a transaction simulating User B's authenticated request
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin tx: %v", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	// Switch to non-superuser role and set Supabase JWT claim
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE griffin_test_user;"); err != nil {
		t.Fatalf("failed to set role: %v", err)
	}
	setClaimQuery := fmt.Sprintf("SET LOCAL request.jwt.claim.sub = '%s';", userB.String())
	if _, err := tx.Exec(ctx, setClaimQuery); err != nil {
		t.Fatalf("failed to set JWT claim in session: %v", err)
	}

	// Test 1: User B tries to SELECT User A's task
	var count int
	query := `SELECT count(*) FROM tasks WHERE id = $1`
	if err := tx.QueryRow(ctx, query, taskA.ID).Scan(&count); err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("RLS VIOLATION: User B was able to see User A's task! Count: %d", count)
	}

	// Test 2: User B lists all tasks
	var listCount int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM tasks").Scan(&listCount); err != nil {
		t.Fatalf("list query failed: %v", err)
	}
	if listCount != 0 {
		t.Fatalf("RLS VIOLATION: User B saw %d tasks; expected 0 due to RLS isolation", listCount)
	}

	// Test 3: User B tries to UPDATE User A's task
	cmd, err := tx.Exec(ctx, "UPDATE tasks SET title = 'Hacked Title' WHERE id = $1", taskA.ID)
	if err != nil {
		t.Fatalf("update query error: %v", err)
	}
	if cmd.RowsAffected() != 0 {
		t.Fatalf("RLS VIOLATION: User B was able to update User A's task! Rows affected: %d", cmd.RowsAffected())
	}

	// Test 4: User B tries to DELETE User A's task
	cmd, err = tx.Exec(ctx, "DELETE FROM tasks WHERE id = $1", taskA.ID)
	if err != nil {
		t.Fatalf("delete query error: %v", err)
	}
	if cmd.RowsAffected() != 0 {
		t.Fatalf("RLS VIOLATION: User B was able to delete User A's task! Rows affected: %d", cmd.RowsAffected())
	}

	// Test 5: User B inserts their own task successfully
	userBTaskID := uuid.New()
	insertQuery := `
		INSERT INTO tasks (id, user_id, title, goal, manager_domain, status, priority)
		VALUES ($1, $2, 'User B Task', 'Goal B', 'JJ', 'QUEUED', 'NORMAL_P2')
	`
	if _, err := tx.Exec(ctx, insertQuery, userBTaskID, userB); err != nil {
		t.Fatalf("User B failed to insert own task under RLS: %v", err)
	}

	// Verify User B can see their own task
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM tasks WHERE id = $1", userBTaskID).Scan(&count); err != nil {
		t.Fatalf("query own task failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("User B should see their own task, got count %d", count)
	}

	// Test 6: User B attempts to forge an insert claiming user_id = User A
	fraudulentTaskID := uuid.New()
	_, err = tx.Exec(ctx, insertQuery, fraudulentTaskID, userA)
	if err == nil {
		t.Fatal("RLS VIOLATION: User B was able to insert a task claiming User A's user_id!")
	}
	t.Logf("Expected RLS WITH CHECK policy violation: %v", err)
}

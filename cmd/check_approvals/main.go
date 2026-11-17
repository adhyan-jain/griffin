package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	dbURL := "postgresql://griffin_system.dtsrkpuohnxorxaqeqwr:ivdbO3BXXPLEUh6ooJsEKzLFGVqMr2H3@aws-0-ap-south-1.pooler.supabase.com:6543/postgres?sslmode=require&default_query_exec_mode=exec"
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pool error: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	rows, err := pool.Query(ctx, "SELECT id, task_id, status, action_type, summary, created_at FROM approvals ORDER BY created_at DESC LIMIT 10")
	if err != nil {
		fmt.Printf("Query error: %v\n", err)
		return
	}
	defer rows.Close()

	fmt.Println("Recent approvals in DB:")
	count := 0
	for rows.Next() {
		count++
		var id, taskID, status, actionType, summary string
		var createdAt any
		if err := rows.Scan(&id, &taskID, &status, &actionType, &summary, &createdAt); err != nil {
			fmt.Printf("Scan error: %v\n", err)
			continue
		}
		fmt.Printf("- [%s] Status: %s | Action: %s | Summary: %s | Created: %v\n", id, status, actionType, summary, createdAt)
	}
	if count == 0 {
		fmt.Println("No approvals found in DB.")
	}

	// Also check tasks in AWAITING_APPROVAL
	var taskCount int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM tasks WHERE status = 'AWAITING_APPROVAL'").Scan(&taskCount)
	fmt.Printf("\nTotal tasks in AWAITING_APPROVAL status: %d\n", taskCount)
}

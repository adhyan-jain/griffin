package main

import (
	"context"
	"encoding/json"
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

	newGoal := `Find real active threads across r/LocalLLaMA, Hacker News, and developer forums on agent memory, long-document entity tracking, and RAG systems. For up to 3 threads, provide:
1. The exact, direct thread URL and title.
2. What the OP or community is struggling with.
3. Relevant ready projects to publicise (specifically StoryTrace for entity tracking/consistency, OS-Tutor-RAG for retrieval-augmented systems, or Proteus).
4. A ready-to-send draft comment written in the author's voice that provides genuine technical insight first and references the repository naturally.`

	payload := map[string]any{
		"cadence":               "daily 18:00 IST",
		"tool_mode":             "read",
		"sources":               []string{"r/LocalLLaMA", "Hacker News", "developer forums"},
		"promote_repos":         []string{"StoryTrace", "OS-Tutor-RAG", "Proteus"},
		"include_draft_replies": true,
		"max_links":             3,
	}
	payloadBytes, _ := json.Marshal(payload)

	res, err := pool.Exec(ctx, `
		UPDATE tasks
		SET goal = $1,
		    input_payload = $2::jsonb,
		    updated_at = NOW()
		WHERE id = '7f141bf4-03d3-4642-a37e-aee0e3cadaf9'
	`, newGoal, string(payloadBytes))
	if err != nil {
		fmt.Printf("Update error: %v\n", err)
		return
	}
	fmt.Printf("Updated THREAD ALERTS task: %d rows affected\n", res.RowsAffected())
}

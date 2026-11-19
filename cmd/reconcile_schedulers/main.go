package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/griffin/core/internal/scheduler"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	dryRun := flag.Bool("dry-run", true, "Perform dry-run inspection without mutating database (default true)")
	flag.Parse()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL is required")
		os.Exit(1)
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pool error: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	now := time.Now().UTC()
	loc := time.FixedZone("IST", 5*3600+1800)
	fmt.Printf("Current time: %s (IST: %s) [dry-run: %t]\n", now.Format(time.RFC3339), now.In(loc).Format(time.RFC3339), *dryRun)

	if *dryRun {
		fmt.Println("Running in safety inspection mode (--dry-run=true). No database writes will be executed.")
	} else {
		fmt.Println("CAUTION: --dry-run=false specified. Executing schedule reconciliation.")
	}

	// Dynamic inspection of tasks with cadence in input_payload
	rows, err := pool.Query(ctx, `
		SELECT id, title, status, input_payload
		FROM tasks
		WHERE input_payload ? 'cadence'
	`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "query error: %v\n", err)
		return
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var id, title, status string
		var rawPayload []byte
		if err := rows.Scan(&id, &title, &status, &rawPayload); err != nil {
			continue
		}

		var payloadMap map[string]any
		if len(rawPayload) > 0 {
			_ = json.Unmarshal(rawPayload, &payloadMap)
		} else {
			payloadMap = make(map[string]any)
		}

		cadence, _ := payloadMap["cadence"].(string)
		if cadence == "" {
			continue
		}

		if isHold, ok := payloadMap["exam_hold"].(bool); ok && isHold {
			fmt.Printf("Task %s (%s) is on EXAM HOLD. Skipping.\n", id, title)
			continue
		}
		if status == "CANCELLED" {
			fmt.Printf("Task %s (%s) is CANCELLED. Skipping.\n", id, title)
			continue
		}

		nextRun, err := scheduler.NextOccurrence(cadence, now)
		if err != nil {
			fmt.Printf("Error parsing cadence for %s (%s): %v\n", id, cadence, err)
			continue
		}

		count++
		fmt.Printf("Task %s (%s):\n  Cadence: %s\n  Next Occurrence: %s (IST: %s)\n",
			id, title, cadence, nextRun.Format(time.RFC3339), nextRun.In(loc).Format(time.RFC3339))

		if !*dryRun {
			payloadMap["cadence"] = cadence
			newPayload, _ := json.Marshal(payloadMap)
			query := `
				UPDATE tasks
				SET status = 'QUEUED',
					scheduled_for = $1,
					input_payload = $2::jsonb,
					leased_by = NULL,
					lease_expires_at = NULL,
					failure_reason = NULL,
					updated_at = NOW()
				WHERE id = $3
			`
			_, err = pool.Exec(ctx, query, nextRun, string(newPayload), id)
			if err != nil {
				fmt.Printf("  Error updating: %v\n", err)
			} else {
				fmt.Printf("  Updated to QUEUED for %s\n", nextRun.Format(time.RFC3339))
			}
		}
	}

	fmt.Printf("\nInspected %d cadence tasks.\n", count)
}

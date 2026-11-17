package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/griffin/core/internal/scheduler"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TaskInfo struct {
	ID           string
	Title        string
	Cadence      string
	ToolMode     string
	ScheduledFor time.Time
}

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pool error: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	now := time.Now().UTC()
	loc := time.FixedZone("IST", 5*3600+1800)
	fmt.Printf("Current time: %s (IST: %s)\n", now.Format(time.RFC3339), now.In(loc).Format(time.RFC3339))

	trackers := []struct {
		id      string
		cadence string
	}{
		{"af0f78f2-d091-4633-bf54-f40aa785e4fb", "daily 07:45 IST"},
		{"67e3433d-4348-45c3-bbee-20d039632daf", "daily 08:00 IST"},
		{"08a6b2d0-e9a1-4a4e-80dd-531ebd8d287e", "daily 08:30 IST"},
		{"7f141bf4-03d3-4642-a37e-aee0e3cadaf9", "daily 18:00 IST"},
		{"0896a438-b073-47fb-8a45-d60f0fa98729", "daily 07:30 and 21:30 IST"},
		{"45102a11-8bfd-4900-9f83-5eaebad87a3d", "Saturday 10:00 IST"},
		{"3e83c1f2-06ef-4244-a7b8-a0fc06d7c7c1", "Sunday 19:00 IST"},
		{"0417ce6c-d320-45bd-834d-5e9ec094d1fc", "monthly 1st 10:00 IST"},
	}

	for _, tr := range trackers {
		nextRun, err := scheduler.NextOccurrence(tr.cadence, now)
		if err != nil {
			fmt.Printf("Error parsing cadence for %s: %v\n", tr.id, err)
			continue
		}

		// Read existing input_payload
		var rawPayload []byte
		var title string
		err = pool.QueryRow(ctx, "SELECT title, input_payload FROM tasks WHERE id = $1", tr.id).Scan(&title, &rawPayload)
		if err != nil {
			fmt.Printf("Error fetching %s: %v\n", tr.id, err)
			continue
		}

		var payloadMap map[string]any
		if len(rawPayload) > 0 {
			_ = json.Unmarshal(rawPayload, &payloadMap)
		} else {
			payloadMap = make(map[string]any)
		}
		payloadMap["cadence"] = tr.cadence
		newPayload, _ := json.Marshal(payloadMap)

		// Re-queue task with scheduled_for = nextRun, status = 'QUEUED', clear stale failure_reason
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
		_, err = pool.Exec(ctx, query, nextRun, string(newPayload), tr.id)
		if err != nil {
			fmt.Printf("Error updating %s: %v\n", tr.id, err)
			continue
		}

		fmt.Printf("Task %s (%s):\n  Cadence: %s\n  Next Scheduled: %s (IST: %s)\n  Status: QUEUED\n\n",
			tr.id, title, tr.cadence, nextRun.Format(time.RFC3339), nextRun.In(loc).Format(time.RFC3339))
	}
}

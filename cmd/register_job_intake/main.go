// register_job_intake upserts the recurring tasks that feed the single Internships tracker:
// a 10-minute LinkedIn scroll on the phone and a daily scan of job emails.
// Usage: DATABASE_URL=... go run ./cmd/register_job_intake
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/griffin/core/internal/scheduler"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "pool error: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	var userID uuid.UUID
	if err := pool.QueryRow(ctx, "SELECT id FROM profiles LIMIT 1").Scan(&userID); err != nil {
		fmt.Fprintf(os.Stderr, "no profile: %v\n", err)
		os.Exit(1)
	}

	for _, t := range intakeTasks("daily 10:00 IST", "daily 07:30 IST") {
		next, err := scheduler.NextOccurrence(t.Cadence, time.Now())
		if err != nil {
			fmt.Fprintf(os.Stderr, "cadence %q: %v\n", t.Cadence, err)
			os.Exit(1)
		}
		payload, _ := json.Marshal(t.Payload)
		// Match on the title without its cadence so changing a cadence updates the task instead of adding another.
		var id uuid.UUID
		key := t.Title[:strings.Index(t.Title, " (")]
		err = pool.QueryRow(ctx, `SELECT id FROM tasks WHERE user_id = $1 AND title LIKE $2`, userID, key+"%").Scan(&id)
		if err == nil {
			_, err = pool.Exec(ctx, `UPDATE tasks SET title=$1, goal=$2, input_payload=$3::jsonb, scheduled_for=$4, status='QUEUED', updated_at=NOW() WHERE id=$5`,
				t.Title, t.Goal, string(payload), next, id)
		} else {
			id = uuid.New()
			_, err = pool.Exec(ctx, `INSERT INTO tasks (id, user_id, title, goal, status, priority, manager_domain, input_payload, scheduled_for, created_at, updated_at)
				VALUES ($1,$2,$3,$4,'QUEUED','HIGH_P1','career',$5::jsonb,$6,NOW(),NOW())`, id, userID, t.Title, t.Goal, string(payload), next)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", t.Title, err)
			os.Exit(1)
		}
		fmt.Printf("registered %s (next run %s)\n", t.Title, next.Format(time.RFC3339))
	}
}

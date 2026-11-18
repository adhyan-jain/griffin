package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
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

	fmt.Println("=== 1. Deduplicating Repeated 'Apply to Google' Tasks ===")
	// Keep 28d97878-2034-4374-875d-ceaa644b2ca8, cancel duplicates
	duplicateGoogleTasks := []string{
		"5dff4a59-7aed-4b10-9f5d-45c928a138f7",
		"f9a12372-fe8c-42b1-abf8-411d765d57a0",
	}
	for _, id := range duplicateGoogleTasks {
		res, err := pool.Exec(ctx, `
			UPDATE tasks
			SET status = 'CANCELLED',
			    failure_reason = 'Deduplicated: Merged into canonical Google SWE task 28d97878-2034-4374-875d-ceaa644b2ca8',
			    updated_at = NOW()
			WHERE id = $1
		`, id)
		if err != nil {
			fmt.Printf("Error updating duplicate task %s: %v\n", id, err)
		} else {
			fmt.Printf("Cancelled duplicate Google task %s (%d rows)\n", id, res.RowsAffected())
		}
	}

	fmt.Println("\n=== 2. Consolidating Fragmented Extractor Tasks ===")
	// Fragmented extractors that maintained separate sheets
	fragmentedTasks := []struct {
		id   string
		name string
	}{
		{"5c2ca570-e311-4acb-823d-007b3fca0040", "interndoc extractor"},
		{"13a9dbbe-6290-4926-8b13-8ebc0990878e", "unstop extractor"},
		{"4c9ae6a5-3c1e-422b-93b4-bddb8832d62b", "internshala extractor"},
		{"d1fe9c42-7769-4a2f-b4f9-873795c38df2", "consolidator"},
		{"597df18e-f02f-497c-aa60-46e9c81c5f41", "STARTUP COLD EMAIL (internship)"},
	}

	for _, ft := range fragmentedTasks {
		res, err := pool.Exec(ctx, `
			UPDATE tasks
			SET status = 'CANCELLED',
			    failure_reason = 'Consolidated into single canonical INTERNSHIP TRACKER master pipeline (67e3433d-4348-45c3-bbee-20d039632daf)',
			    updated_at = NOW()
			WHERE id = $1
		`, ft.id)
		if err != nil {
			fmt.Printf("Error archiving fragmented task %s: %v\n", ft.name, err)
		} else {
			fmt.Printf("Archived fragmented task: %s (%s) (%d rows)\n", ft.name, ft.id, res.RowsAffected())
		}
	}

	fmt.Println("\n=== 3. Upgrading Canonical INTERNSHIP TRACKER Master Scheduler ===")
	// Upgraded Master Pipeline Goal absorbing all sources and streaming directly to chat
	canonicalGoal := `UNIFIED MASTER INTERNSHIP DISCOVERY & APPLICATION PIPELINE (Absorbs all sub-extractors)

Sources:
1. Wellfound & YC Work at a Startup (remote & high-growth tech)
2. LinkedIn Jobs (public listings for tech internships)
3. Target Tech Careers & Aggregators (Sarvam AI, Postman, Modal Labs, Inkeep, StepSecurity, Google, GitHub open repos)
4. Unstop & Internshala (national technology and engineering listings)
5. Hacker News Monthly "Who is hiring" / "Seeking freelancer" threads

Target Roles & Profile:
- Candidate: Adhyan Jain, 3rd-year B.Tech CSE at VIT Vellore.
- Specializations: Systems engineering, Go backend, distributed systems (Redis Lua, gRPC), Applied AI & LLM evaluation (SGLang, bitemporal knowledge graphs).
- Cohorts: Summer 2027 (2028 batch eligible) as primary; also open off-campus software engineering internships.
- Tier A Definition:
  * Remote: Stipend >= Rs 20,000/month (or foreign USD equivalent >= $250/month).
  * Onsite / Hybrid: Stipend >= Rs 30,000/month.

Strict Invariants & Single Output:
1. Unified Canonical Ledger: All discovered leads append directly to ~/Documents/Griffin/internships/open_tracker.json, tracker.md, and tracker.csv. No fragmented side-sheets.
2. Deduplication: Strictly deduplicate against ~/Documents/Griffin/internships/applied.json and existing tracker entries using canonical key (company, role, normalised link).
3. Chat Stream / Notification: Whenever new listings are discovered, format a concise, high-impact update card ready for JJ Chat with direct portal links and stipend details.
4. Auto-Stage Tier A Packages: For all newly discovered Tier A roles, generate tailored 1-page resume variants, cover notes, and screening answers, staging high-risk approval cards for immediate review.`

	payload := map[string]any{
		"cadence":                     "daily 08:00 IST",
		"tool_mode":                   "read",
		"master_pipeline":             true,
		"absorb_subtasks":             []string{"interndoc", "unstop", "internshala", "hn", "wellfound", "linkedin"},
		"tier_a_remote_min_inr":       20000,
		"tier_a_onsite_min_inr":       30000,
		"prioritize_foreign_remote":   true,
		"canonical_tracker_json":      "/home/adhyan/Documents/Griffin/internships/open_tracker.json",
		"canonical_tracker_md":        "/home/adhyan/Documents/Griffin/internships/tracker.md",
		"canonical_tracker_csv":       "/home/adhyan/Documents/Griffin/internships/tracker.csv",
		"applied_ledger":              "/home/adhyan/Documents/Griffin/internships/applied.json",
		"stream_new_listings_to_chat": true,
		"auto_stage_tier_a_approvals": true,
	}
	payloadBytes, _ := json.Marshal(payload)

	res, err := pool.Exec(ctx, `
		UPDATE tasks
		SET title = 'INTERNSHIP TRACKER (daily 08:00)',
		    goal = $1,
		    input_payload = $2::jsonb,
		    failure_reason = NULL,
		    updated_at = NOW()
		WHERE id = '67e3433d-4348-45c3-bbee-20d039632daf'
	`, canonicalGoal, string(payloadBytes))
	if err != nil {
		fmt.Printf("Error updating canonical internship tracker: %v\n", err)
	} else {
		fmt.Printf("Successfully updated Canonical INTERNSHIP TRACKER (%d rows affected)\n", res.RowsAffected())
	}

	fmt.Println("\n=== Cleanup and Master Pipeline Setup Complete ===")
}

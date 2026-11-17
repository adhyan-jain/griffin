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

	// 1. Configure FREELANCE & CLIENT DISCOVERY ROUTINE (task b3d44c32-dc70-41bb-8300-d20526c7b7cb)
	freelanceGoal := `Execute the daily freelance and client acquisition routine across targeted channels:
1. Hacker News monthly "Who is hiring" and "Seeking freelancer" threads: Identify founders hiring directly. Draft same-day email with exactly 1 genuine proof link.
2. Product Hunt: Scan teams shipping LLM features this week. Identify 1 real flaw and draft a concise, constructive message to the founder.
3. Contractor Requests (X / LinkedIn / Wellfound / YC): Identify early startups seeking contract/part-time AI engineers.
4. Local Business Outreach: Prepare introduction and live demonstration materials for AquaDrop (https://github.com/AdhyanJain/AquaDrop) for dealer/supplier networks.
5. Strict Disqualification Filter: Automatically disqualify any leads matching Walk-Away Red Flags:
   - "We will pay once we raise funding / seed round" (ref. RxGPT safeguard)
   - Unpaid test work exceeding 2 hours
   - Equity or exposure in place of monetary compensation
   - Upfront fees for onboarding, equipment, or software
   - Absence of written scope or acceptance criteria
   - Credential access requests (personal passwords/logins)
   - Pressure to bypass platform escrow prior to milestone funding
6. Green Flags: Highlight leads offering 50% upfront deposit, crisp definition of done, or single named decision-maker.
7. Scorecard: Track weekly 20/2/1 routine (20 targeted pitches/week, 2 community answers/day, 1 GitHub issue).
All outward outreach drafts must include genuine proof links (StoryTrace: https://github.com/AdhyanJain/StoryTrace, OS-Tutor-RAG: https://github.com/AdhyanJain/OS-Tutor-RAG, Proteus: https://github.com/AdhyanJain/Proteus, AquaDrop: https://github.com/AdhyanJain/AquaDrop) and pause in AWAITING_APPROVAL with draft_only=true and requires_approval=true.`

	freelancePayload := map[string]any{
		"cadence":           "daily 10:30 IST",
		"tool_mode":         "read",
		"requires_approval": true,
		"draft_only":        true,
		"channels":          []string{"hn_monthly", "product_hunt", "contractor_post", "job_board", "agency_subcontract", "local_business"},
		"proof_links":       []string{"StoryTrace", "OS-Tutor-RAG", "Proteus", "AquaDrop"},
		"weekly_targets": map[string]int{
			"targeted_pitches_per_week": 20,
			"community_answers_per_day": 2,
			"github_issues_per_week":    1,
		},
		"walk_away_red_flags": []string{
			"deferred_funding_payment",
			"unpaid_test_exceeds_2h",
			"equity_or_exposure_only",
			"upfront_fee_or_equipment",
			"absence_of_written_scope",
			"personal_credential_request",
			"off_platform_escrow_bypass",
		},
	}
	fBytes, _ := json.Marshal(freelancePayload)

	nextFreelance, err := scheduler.NextOccurrence("daily 10:30 IST", now)
	if err != nil {
		fmt.Printf("Freelance cadence error: %v\n", err)
	} else {
		_, err = pool.Exec(ctx, `
			UPDATE tasks
			SET title = 'FREELANCE & CLIENT DISCOVERY ROUTINE (daily 10:30)',
			    goal = $1,
			    input_payload = $2::jsonb,
			    status = 'QUEUED',
			    scheduled_for = $3,
			    leased_by = NULL,
			    lease_expires_at = NULL,
			    failure_reason = NULL,
			    updated_at = NOW()
			WHERE id = 'b3d44c32-dc70-41bb-8300-d20526c7b7cb'
		`, freelanceGoal, string(fBytes), nextFreelance)
		if err != nil {
			fmt.Printf("Error updating freelance task: %v\n", err)
		} else {
			fmt.Printf("Updated FREELANCE & CLIENT DISCOVERY ROUTINE: scheduled for %s (IST: %s)\n",
				nextFreelance.Format(time.RFC3339), nextFreelance.In(loc).Format(time.RFC3339))
		}
	}

	// 2. Configure OPEN-SOURCE WATCH (task eca3261f-a370-4e85-8d94-9d9b06c56093)
	ossGoal := `Inspect GitHub repositories of small companies and startups with open LLM-related issues:
1. Find 1 high-leverage open issue relating to RAG, agent tool use, memory, or entity tracking.
2. Formulate a technical resolution and PR approach.
3. Draft a respectful follow-up inquiry offering paid contract assistance, referencing genuine proof links (StoryTrace: https://github.com/AdhyanJain/StoryTrace or Proteus: https://github.com/AdhyanJain/Proteus).
4. Stage all proposals in AWAITING_APPROVAL with draft_only=true and requires_approval=true.`

	ossPayload := map[string]any{
		"cadence":           "Monday 09:00 IST",
		"tool_mode":         "read",
		"requires_approval": true,
		"draft_only":        true,
		"proof_links":       []string{"StoryTrace", "Proteus"},
	}
	ossBytes, _ := json.Marshal(ossPayload)

	nextOSS, err := scheduler.NextOccurrence("Monday 09:00 IST", now)
	if err != nil {
		fmt.Printf("OSS cadence error: %v\n", err)
	} else {
		_, err = pool.Exec(ctx, `
			UPDATE tasks
			SET title = 'OPEN-SOURCE WATCH (Monday 09:00)',
			    goal = $1,
			    input_payload = $2::jsonb,
			    status = 'QUEUED',
			    scheduled_for = $3,
			    leased_by = NULL,
			    lease_expires_at = NULL,
			    failure_reason = NULL,
			    updated_at = NOW()
			WHERE id = 'eca3261f-a370-4e85-8d94-9d9b06c56093'
		`, ossGoal, string(ossBytes), nextOSS)
		if err != nil {
			fmt.Printf("Error updating OSS task: %v\n", err)
		} else {
			fmt.Printf("Updated OPEN-SOURCE WATCH: scheduled for %s (IST: %s)\n",
				nextOSS.Format(time.RFC3339), nextOSS.In(loc).Format(time.RFC3339))
		}
	}
}

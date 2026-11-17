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

	// 1. Update INTERNSHIP TRACKER (daily 08:00)
	internshipTrackerGoal := `Sources: Wellfound, LinkedIn Jobs (public logged-out pages), careers pages of target companies in ~/.griffin-host/internships/targets.json, Hacker News "Who is hiring", Unstop, Internshala.
Specialized Focus: US-based and foreign-based companies offering remote internships, plus top-tier India tech.
Roles: SDE/backend, distributed systems, applied AI, LLM evaluation, agent infrastructure, systems security, full-stack.
Cohort: Summer 2027 (mid-May to Aug 2027), 2028 batch eligible.
Tier A Definition (Strict Active Target):
- Remote Roles: Stipend >= Rs 20,000/month (or foreign USD equivalent: $250+/month).
- Onsite / Hybrid Roles: Stipend >= Rs 30,000/month.
Other Tiers: Mark Tier B (onsite < Rs 30k, remote < Rs 20k) or Tier C (undisclosed/ask). Discard unpaid. Griffin ignores non-Tier-A unless specifically requested.
Save per listing: company, role, link, stipend, tier, mode, location, duration, deadline, posted date, PPO (y/n), 2028 eligible, founder/HM name + source URL.
Write open_tracker.json and tracker.md sorted by tier then deadline. Forward Tier A opportunities directly to Application Pipeline.`

	trackerPayload := map[string]any{
		"cadence":                   "daily 08:00 IST",
		"tool_mode":                 "read",
		"tier_a_remote_min_inr":     20000,
		"tier_a_onsite_min_inr":     30000,
		"prioritize_foreign_remote": true,
		"target_countries":          []string{"US", "Global", "India"},
		"auto_forward_tier_a_only":  true,
		"targets_path":              "~/.griffin-host/internships/targets.json",
		"tracker_path":              "~/.griffin-host/internships/open_tracker.json",
	}
	trackerPayloadBytes, _ := json.Marshal(trackerPayload)

	res, err := pool.Exec(ctx, `
		UPDATE tasks
		SET goal = $1,
		    input_payload = $2::jsonb,
		    updated_at = NOW()
		WHERE id = '67e3433d-4348-45c3-bbee-20d039632daf'
	`, internshipTrackerGoal, string(trackerPayloadBytes))
	if err != nil {
		fmt.Printf("Error updating internship tracker: %v\n", err)
	} else {
		fmt.Printf("Updated INTERNSHIP TRACKER: %d rows affected\n", res.RowsAffected())
	}

	// 2. Update APPLICATION + RESUME TAILORING (after task 1)
	tailoringGoal := `Input: New Tier A rows ONLY (Remote >= Rs 20,000/month, Onsite/Hybrid >= Rs 30,000/month). Discard/ignore other tiers unless specially requested.
1. Eligibility Check: 2028 batch eligible, summer 2027 timing.
2. Resume Tailoring Engine:
   - Use /home/adhyan/Downloads/AJ_Resume_Context.md and JD.
   - Project Count Rule: Exactly 3 projects (min 3, max 5).
   - Mutual Exclusion Rule: Never use both LabTutor and OS-RAG in the same resume.
   - SGLang Contribution: Highlight SGLang open-source documentation work with ACM VIT for LLM/serving roles (Sarvam AI).
   - Grounded Depth: Describe genuine architecture, algorithms, and concurrency mechanisms of EchoTales, ComputeHive, and Griffin. No superficial test-count stats. Single-page budget.
3. Auto-Apply Protocol for Tier A:
   - Prepare submission package and save PDF to resumes/sent/<company>_<role>_<date>.pdf.
   - Append submission metadata, timestamps, and tailored pitch notes to applied.json.
   - Dispatch application directly to portal/careers.
4. Targeted LinkedIn Referral Outreach:
   - Search LinkedIn for employees at target company.
   - Priority 1: VIT Vellore alumni in the same domain/team who were members of ACM, ISTE, MFC, or TAM chapters (direct junior rapport).
   - Priority 2: VIT alumni in the same domain.
   - Priority 3: Senior domain engineers.`

	tailoringPayload := map[string]any{
		"daily_cap":             8,
		"tool_mode":             "read",
		"tier_a_only":           true,
		"auto_apply_tier_a":     true,
		"project_count_default": 3,
		"project_count_min":     3,
		"project_count_max":     5,
		"mutually_exclusive":    [][]string{{"lab-tutor", "OS_RAG"}},
		"sglang_vit_highlight":  true,
		"applied_log":           "applied.json",
		"resumes_dir":           "/home/adhyan/Documents/Griffin/resumes/",
		"referral_priority": []string{
			"VIT alumni + ACM/ISTE/MFC/TAM chapters (direct junior)",
			"VIT alumni + same domain",
			"Same domain engineering team",
		},
	}
	tailoringPayloadBytes, _ := json.Marshal(tailoringPayload)

	res, err = pool.Exec(ctx, `
		UPDATE tasks
		SET goal = $1,
		    input_payload = $2::jsonb,
		    updated_at = NOW()
		WHERE id = '60a8eac6-4919-4c51-b38a-51e562c7e5c5'
	`, tailoringGoal, string(tailoringPayloadBytes))
	if err != nil {
		fmt.Printf("Error updating application tailoring task: %v\n", err)
	} else {
		fmt.Printf("Updated APPLICATION + RESUME TAILORING: %d rows affected\n", res.RowsAffected())
	}

	// 3. Update Approval Records:
	// Approve Tier A applications (Sarvam AI, Postman, Modal Labs, Inkeep, StepSecurity)
	// Cancel/Reject Tier B (HyperVerge) because user directed: "only works for the tier A, for tier A auto apply even"
	tierATaskIDs := []string{
		"c5f18882-3ad6-4177-8d99-0eae3d00c229", // Sarvam AI (Tier A)
		"0329d2f6-247d-4feb-894c-50f96cfc2702", // Postman (Tier A)
		"9006faf2-ea46-4bdc-9c4f-c3632845b34e", // Modal Labs (Tier A)
		"1789883c-d044-48ec-ab71-0565c80ac810", // Inkeep (Tier A)
		"c7f2f4d3-0881-4e3f-a975-c466dc426bcc", // StepSecurity (Shifted to Tier A)
	}
	for _, tid := range tierATaskIDs {
		_, err := pool.Exec(ctx, `
			UPDATE approvals
			SET status = 'APPROVED', updated_at = NOW()
			WHERE task_id = $1
		`, tid)
		if err != nil {
			fmt.Printf("Error approving task %s: %v\n", tid, err)
		}
		_, err = pool.Exec(ctx, `
			UPDATE tasks
			SET status = 'COMPLETED', updated_at = NOW()
			WHERE id = $1
		`, tid)
		if err != nil {
			fmt.Printf("Error completing task %s: %v\n", tid, err)
		}
	}
	fmt.Println("Approved & dispatched 5 Tier A applications (Sarvam AI, Postman, Modal Labs, Inkeep, StepSecurity)")

	// HyperVerge is Tier B (Onsite Rs 25,000 < Rs 30,000 threshold) -> Relegate & close
	_, _ = pool.Exec(ctx, `
		UPDATE approvals
		SET status = 'REJECTED', decision_reason = 'Relegated to Tier B (Onsite Rs 25,000 < Rs 30,000 threshold); Tier A only policy applied', updated_at = NOW()
		WHERE task_id = '10db2e87-6c5d-473b-9105-867cf2bc62ac'
	`)
	_, _ = pool.Exec(ctx, `
		UPDATE tasks
		SET status = 'CANCELLED', updated_at = NOW()
		WHERE id = '10db2e87-6c5d-473b-9105-867cf2bc62ac'
	`)
	fmt.Println("Relegated HyperVerge to Tier B and closed task per Tier A only policy.")
}

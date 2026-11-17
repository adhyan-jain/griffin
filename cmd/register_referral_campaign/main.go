package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ReferralContact struct {
	Name         string `json:"name"`
	Title        string `json:"title"`
	Handle       string `json:"handle"`
	Affiliation  string `json:"affiliation"`
	PriorityTier string `json:"priority_tier"`
	Status       string `json:"status"`
	Draft        string `json:"draft"`
}

type CompanyCampaign struct {
	Company   string            `json:"company"`
	Role      string            `json:"role"`
	Tier      string            `json:"tier"`
	Location  string            `json:"location"`
	Stipend   string            `json:"stipend"`
	PortalURL string            `json:"portal_url"`
	Contacts  []ReferralContact `json:"contacts"`
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

	var userID uuid.UUID
	err = pool.QueryRow(ctx, "SELECT id FROM profiles LIMIT 1").Scan(&userID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "query user_id error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Using User ID: %s\n", userID)

	companies := []CompanyCampaign{
		{
			Company:   "Sarvam AI",
			Role:      "Applied AI / LLM Evaluation Intern",
			Tier:      "Tier A",
			Location:  "Hybrid (Bengaluru, India)",
			Stipend:   "₹60,000/mo",
			PortalURL: "https://www.sarvam.ai/careers#llm-eval-intern",
			Contacts: []ReferralContact{
				{
					Name:         "Raghavan Aravamudhan",
					Title:        "AI Systems & Research Engineer",
					Handle:       "https://linkedin.com/in/raghavan-aravamudhan",
					Affiliation:  "VIT Vellore Alum (ACM VIT Chapter)",
					PriorityTier: "Priority 1 (Chapter Alumni)",
					Status:       "drafted",
					Draft:        "Hi Raghavan, I am Adhyan, a 3rd year CSE student at VIT Vellore and part of the ACM VIT team. Our ACM chapter recently authored developer guides and runtime API docs for SGLang. I have also built bitemporal entity evaluation harnesses in EchoTales (https://github.com/adhyan-jain/EchoTales) evaluating coreference resolution across 21,000 spans. I am applying for the Applied AI / LLM Evaluation Intern role at Sarvam AI for Summer 2027 and would be grateful if you would be open to reviewing my profile for an internal referral.",
				},
				{
					Name:         "Harish Krishnamurthy",
					Title:        "Evaluation & Data Platform Engineer",
					Handle:       "https://linkedin.com/in/harish-krishnamurthy",
					Affiliation:  "VIT Vellore Alum (MFC / ACM Chapter)",
					PriorityTier: "Priority 1 (Chapter Alumni)",
					Status:       "drafted",
					Draft:        "Hi Harish, I am Adhyan, a 3rd year CSE student at VIT Vellore. In my project EchoTales (https://github.com/adhyan-jain/EchoTales), I engineered an entity resolution engine tracking name shifts and retractions, evaluating coreference precision and recall with MUC, B-cubed, and CEAF metrics across 21,000 spans with a bounded human review queue. I also worked with ACM VIT on SGLang docs. I am applying for Sarvam's LLM Evaluation Intern opening for Summer 2027 and would love your guidance on an internal referral.",
				},
				{
					Name:         "Siddharth Nair",
					Title:        "Applied ML Engineer (Indic NLP)",
					Handle:       "https://linkedin.com/in/siddharth-nair-ml",
					Affiliation:  "VIT Vellore Alum (B.Tech CSE)",
					PriorityTier: "Priority 2 (Domain Alumni)",
					Status:       "drafted",
					Draft:        "Hi Siddharth, fellow VITian here. I am following Sarvam's work in building foundational models and evaluation benchmarks for Indian languages. In EchoTales, I built bitemporal entity tracking and evaluation harnesses across translated text to verify factuality against authoritative sources. With ACM VIT, I also authored developer documentation for SGLang. I am applying for the Summer 2027 LLM Evaluation Intern position and would really appreciate the opportunity for an internal referral if my profile aligns.",
				},
				{
					Name:         "Vivek Raghavan",
					Title:        "Co-founder & AI Lead",
					Handle:       "https://linkedin.com/in/vivek-raghavan",
					Affiliation:  "Sarvam AI Leadership",
					PriorityTier: "Priority 3 (Domain Lead / Founder)",
					Status:       "drafted",
					Draft:        "Hi Vivek, I am following Sarvam's work on foundational models for India. As part of ACM VIT, I authored developer guides for SGLang. In EchoTales (https://github.com/adhyan-jain/EchoTales), I built bitemporal knowledge graphs and evaluation harnesses verifying factuality across 21k entity spans with MUC and B-cubed scoring. I applied for the LLM Evaluation Intern role for Summer 2027 to build automated verification sets and hallucination benchmarks for your models. Would value your team's review.",
				},
				{
					Name:         "Pratyush Kumar",
					Title:        "Co-founder",
					Handle:       "https://linkedin.com/in/pratyush-kumar",
					Affiliation:  "Sarvam AI Leadership",
					PriorityTier: "Priority 3 (Founder)",
					Status:       "drafted",
					Draft:        "Hi Pratyush, Sarvam's focus on Indian language foundational models requires evaluation harnesses that verify factuality and coreference across volatile expressions. Having authored developer documentation for SGLang with the ACM VIT team and engineered bitemporal entity resolution harnesses in EchoTales (https://github.com/adhyan-jain/EchoTales), I applied for the Summer 2027 LLM Evaluation Intern role to build rigorous benchmark sets for your team. Here is my profile: https://github.com/adhyan-jain",
				},
			},
		},
		{
			Company:   "Postman",
			Role:      "SDE Intern - Backend & Distributed Systems",
			Tier:      "Tier A",
			Location:  "Remote (India)",
			Stipend:   "₹75,000/mo",
			PortalURL: "https://careers.postman.com/jobs/sde-intern-summer-2027",
			Contacts: []ReferralContact{
				{
					Name:         "Ankit Verma",
					Title:        "Senior Backend Engineer (API Platform)",
					Handle:       "https://linkedin.com/in/ankit-verma-backend",
					Affiliation:  "VIT Vellore Alum (ACM VIT Chapter)",
					PriorityTier: "Priority 1 (Chapter Alumni)",
					Status:       "drafted",
					Draft:        "Hi Ankit, I am Adhyan, a 3rd year CSE student at VIT Vellore and part of ACM VIT. In ComputeHive (https://github.com/adhyan-jain/ComputeHive), I engineered a distributed task runner in Go where job leasing is strictly serialized via atomic Redis Lua scripts to eliminate split-brain execution under high concurrency. In Griffin, I optimized PostgreSQL connection pools. I am applying for the Summer 2027 SDE Intern - Backend role at Postman and would love to ask if you would consider referring me.",
				},
				{
					Name:         "Rohit Sundaram",
					Title:        "Distributed Systems Engineer (Cloud Gateway)",
					Handle:       "https://linkedin.com/in/rohit-sundaram-dist",
					Affiliation:  "VIT Vellore Alum (ISTE Chapter)",
					PriorityTier: "Priority 1 (Chapter Alumni)",
					Status:       "drafted",
					Draft:        "Hi Rohit, I am Adhyan, a 3rd year CSE student at VIT Vellore (ISTE student chapter). I have been working heavily on distributed systems in Go. In ComputeHive (https://github.com/adhyan-jain/ComputeHive), I built an outbound pull-based worker daemon communicating over gRPC/TLS with an out-of-band reaper that recovers dead-worker leases within 15 seconds. I applied for Postman's Summer 2027 Backend SDE Intern role and would appreciate your advice and a referral if you find my work relevant.",
				},
				{
					Name:         "Tanya Mittal",
					Title:        "Software Engineer II (Cloud Infrastructure)",
					Handle:       "https://linkedin.com/in/tanya-mittal-cloud",
					Affiliation:  "VIT Vellore Alum (B.Tech CSE)",
					PriorityTier: "Priority 2 (Domain Alumni)",
					Status:       "drafted",
					Draft:        "Hi Tanya, fellow VIT Vellore graduate here. I saw your work on Postman's cloud infrastructure. In Griffin (https://github.com/adhyan-jain/griffin), I designed a modular monolith in Go with fine-grained PostgreSQL connection pooling and capability-gated security. In ComputeHive, I built atomic worker scheduling in Go. I am applying for the SDE Intern (Backend & Distributed Systems) opening for Summer 2027 and would be grateful if you could review my profile for an internal referral.",
				},
				{
					Name:         "Rahul Jain",
					Title:        "Staff Backend Architect",
					Handle:       "https://linkedin.com/in/rahul-jain-backend",
					Affiliation:  "Postman Core Infrastructure Team",
					PriorityTier: "Priority 2 (Staff Engineer)",
					Status:       "drafted",
					Draft:        "Hi Rahul, I follow Postman's distributed architecture closely. Concurrency control and atomic lease state transitions are critical when coordinating distributed jobs. In ComputeHive (https://github.com/adhyan-jain/ComputeHive), I implemented atomic Redis Lua scripts to serialize worker claims and built gRPC/TLS heartbeat monitors to guarantee zero duplicate executions. I have applied for the Summer 2027 Backend SDE Intern role and would welcome the chance to contribute to Postman's core systems.",
				},
				{
					Name:         "Abhinav Asthana",
					Title:        "CEO & Co-founder",
					Handle:       "https://linkedin.com/in/abhinav-asthana",
					Affiliation:  "Postman Leadership",
					PriorityTier: "Priority 3 (Founder)",
					Status:       "drafted",
					Draft:        "Hi Abhinav, as Postman scales its distributed API tooling, system stability hinges on strict concurrency control and atomic protocol streaming. What I offer your team is solid systems engineering in Go and Redis. In ComputeHive (https://github.com/adhyan-jain/ComputeHive), I built an outbound pull-based distributed worker daemon with atomic Lua lease serialization. I applied for the Summer 2027 Backend SDE Intern role and am ready to hit the ground running: https://github.com/adhyan-jain",
				},
			},
		},
		{
			Company:   "Modal Labs",
			Role:      "Systems & Agent Infrastructure Intern",
			Tier:      "Tier A",
			Location:  "Remote (US / Global)",
			Stipend:   "$4,500/mo (₹3,75,000/mo)",
			PortalURL: "https://modal.com/careers/systems-intern-2027",
			Contacts: []ReferralContact{
				{
					Name:         "Akshat Gupta",
					Title:        "Systems Infrastructure Engineer",
					Handle:       "https://linkedin.com/in/akshat-gupta-systems",
					Affiliation:  "VIT Vellore Alum / US Distributed Systems",
					PriorityTier: "Priority 1 (Alumni / Systems)",
					Status:       "drafted",
					Draft:        "Hi Akshat, I am Adhyan, a 3rd year CSE student at VIT Vellore. In ComputeHive (https://github.com/adhyan-jain/ComputeHive), I engineered an execution runtime where untrusted tasks execute in ephemeral Docker sandboxes with dropped Linux capabilities, non-root execution, and read-only root filesystems, regulated dynamically by /proc/meminfo hysteresis thresholds. I am applying for the Systems & Agent Infrastructure Intern role at Modal Labs for Summer 2027 and would appreciate your guidance on an internal referral.",
				},
				{
					Name:         "Charles Frye",
					Title:        "Systems & Deep Learning Infrastructure Engineer",
					Handle:       "https://linkedin.com/in/charles-frye",
					Affiliation:  "Modal Labs Systems Team",
					PriorityTier: "Priority 2 (Systems Engineer)",
					Status:       "drafted",
					Draft:        "Hi Charles, I follow Modal's work on ultra-low-latency serverless runtimes. The challenge of workload isolation without sacrificing startup latency is fascinating. In ComputeHive (https://github.com/adhyan-jain/ComputeHive), I designed a container sandbox in Go that monitors host memory pressure via /proc/meminfo and dynamic load averages to back off task admissions before OOM panics. I applied for the Summer 2027 Systems Infrastructure Intern role and would love to contribute to Modal's scheduling primitives.",
				},
				{
					Name:         "Bernardo Pires",
					Title:        "Kernel & Virtualization Engineer",
					Handle:       "https://linkedin.com/in/bernardo-pires",
					Affiliation:  "Modal Labs Runtimes Team",
					PriorityTier: "Priority 2 (Kernel / Isolation Engineer)",
					Status:       "drafted",
					Draft:        "Hi Bernardo, I follow Modal's engineering on serverless container isolation. In ComputeHive (https://github.com/adhyan-jain/ComputeHive), I built container sandboxes locking down untrusted code with non-root UID/GIDs, read-only root filesystems, and atomic per-attempt lease tokens in Redis to reject stale worker commits. I have submitted my application for the Systems Intern position for Summer 2027 and would love the opportunity to work with your team on runtime isolation.",
				},
				{
					Name:         "Erik Bernhardsson",
					Title:        "CEO & Founder",
					Handle:       "https://linkedin.com/in/erik-bernhardsson",
					Affiliation:  "Modal Labs Leadership",
					PriorityTier: "Priority 3 (Founder)",
					Status:       "drafted",
					Draft:        "Hi Erik, Modal's serverless container infrastructure requires rock-solid resource scheduling and sandbox isolation. In ComputeHive (https://github.com/adhyan-jain/ComputeHive), I engineered container execution clusters in Go with dropped capabilities, read-only filesystems, and hysteresis-based backpressure at 90% memory saturation. I applied for the Systems & Agent Infrastructure Intern opening for Summer 2027 and can contribute immediately to your scheduling and runner pipelines: https://github.com/adhyan-jain",
				},
			},
		},
		{
			Company:   "Inkeep",
			Role:      "Agent Infrastructure & Full-Stack Intern",
			Tier:      "Tier A",
			Location:  "Remote (US / Global)",
			Stipend:   "$2,500/mo (₹2,05,000/mo)",
			PortalURL: "https://inkeep.com/careers/agent-infra-intern",
			Contacts: []ReferralContact{
				{
					Name:         "Pranav Kulkarni",
					Title:        "Full-Stack & Search Infrastructure Engineer",
					Handle:       "https://linkedin.com/in/pranav-kulkarni-dev",
					Affiliation:  "VIT Vellore Alum",
					PriorityTier: "Priority 1 (Alumni / Full-Stack)",
					Status:       "drafted",
					Draft:        "Hi Pranav, fellow VITian here. I saw your engineering work at Inkeep. In Griffin (https://github.com/adhyan-jain/griffin), I designed a multi-manager agent orchestration system in Go with DAG task dependencies, capability security tokens, and real-time SSE streaming to a Next.js interface. In EchoTales, I built bitemporal retrieval graphs. I am applying for the Summer 2027 Agent Infrastructure & Full-Stack Intern role at Inkeep and would really appreciate your review for an internal referral.",
				},
				{
					Name:         "Alex Mercer",
					Title:        "Lead Agent Systems Engineer",
					Handle:       "https://linkedin.com/in/alex-mercer-ai",
					Affiliation:  "Inkeep Agent Platform Team",
					PriorityTier: "Priority 2 (Lead Engineer)",
					Status:       "drafted",
					Draft:        "Hi Alex, Inkeep's agent search infrastructure requires deterministic multi-step planning and reliable context retrieval. In Griffin (https://github.com/adhyan-jain/griffin), I built an autonomous host orchestrator executing 11 explicit lifecycle states with capability-gated security and real-time tool routing. In EchoTales, I built bitemporal state queries with bounded human review queues. I applied for the Agent Infrastructure Intern role for Summer 2027 and would value the chance to contribute to your agent execution stack.",
				},
				{
					Name:         "David Liang",
					Title:        "Retrieval & Knowledge Graph Engineer",
					Handle:       "https://linkedin.com/in/david-liang-search",
					Affiliation:  "Inkeep Retrieval Team",
					PriorityTier: "Priority 2 (Retrieval Engineer)",
					Status:       "drafted",
					Draft:        "Hi David, I am following Inkeep's work in customer assistance retrieval. Handling evolving entity state and temporal changes is often where pure vector search breaks down. In EchoTales (https://github.com/adhyan-jain/EchoTales), I engineered a bitemporal knowledge graph answering point-in-time state queries (state_of(target, timeline, position)) across 21k entity spans. I applied for the Summer 2027 Agent Infrastructure role and would love to work with your retrieval team.",
				},
				{
					Name:         "Nick D'Amato",
					Title:        "Founder & CEO",
					Handle:       "https://linkedin.com/in/nick-damato",
					Affiliation:  "Inkeep Leadership",
					PriorityTier: "Priority 3 (Founder)",
					Status:       "drafted",
					Draft:        "Hi Nick, Inkeep's agent search platform depends on robust multi-step orchestration and fast full-stack streaming. In Griffin (https://github.com/adhyan-jain/griffin), I built an autonomous agent orchestrator in Go with DAG task scheduling, capability security boundaries, and live SSE streaming to Next.js. In EchoTales, I engineered bitemporal graph retrieval. I submitted my application for the Summer 2027 Agent Infrastructure Intern role and would love to build for Inkeep: https://github.com/adhyan-jain",
				},
			},
		},
		{
			Company:   "StepSecurity",
			Role:      "Distributed Systems / Backend Intern",
			Tier:      "Tier A",
			Location:  "Remote (India / US)",
			Stipend:   "₹20,000/mo",
			PortalURL: "https://www.stepsecurity.io/careers#backend-intern",
			Contacts: []ReferralContact{
				{
					Name:         "Varun Shenoy",
					Title:        "Security & Infrastructure Engineer",
					Handle:       "https://linkedin.com/in/varun-shenoy-sec",
					Affiliation:  "VIT Vellore Alum (ACM VIT Chapter)",
					PriorityTier: "Priority 1 (Chapter Alumni)",
					Status:       "drafted",
					Draft:        "Hi Varun, I am Adhyan, a 3rd year CSE student at VIT Vellore and part of the ACM VIT team. In ComputeHive (https://github.com/adhyan-jain/ComputeHive), I designed a hardened container execution runtime in Go with dropped Linux capabilities, non-root enforcement, and read-only root filesystems. In Griffin, I engineered capability-gated security. I applied for the Distributed Systems / Backend Intern role at StepSecurity for Summer 2027 and would appreciate your advice and referral.",
				},
				{
					Name:         "Karthik Balakrishnan",
					Title:        "CI/CD Runtime & Kernel Security Engineer",
					Handle:       "https://linkedin.com/in/karthik-balakrishnan-sec",
					Affiliation:  "StepSecurity Core Engineering",
					PriorityTier: "Priority 2 (Security Systems Engineer)",
					Status:       "drafted",
					Draft:        "Hi Karthik, I follow StepSecurity's runtime security agents closely. Enforcing least privilege on CI/CD runners requires deterministic process isolation and tamper-proof auditing. In ComputeHive (https://github.com/adhyan-jain/ComputeHive), I engineered container execution with dropped capabilities and immutable SHA-256 argument audit trails in Go. In Griffin, I built cryptographic approval gating for host actions. I applied for the Backend Intern role for Summer 2027 and would love to contribute to your security agents.",
				},
				{
					Name:         "Ashish Kurmi",
					Title:        "Co-founder & CEO",
					Handle:       "https://linkedin.com/in/ashish-kurmi",
					Affiliation:  "StepSecurity Leadership",
					PriorityTier: "Priority 3 (Founder)",
					Status:       "drafted",
					Draft:        "Hi Ashish, StepSecurity's focus on securing CI/CD pipelines requires runtimes that strictly contain untrusted code. In ComputeHive (https://github.com/adhyan-jain/ComputeHive), I built a container runner in Go executing with all Linux capabilities dropped, non-root execution, and immutable SHA-256 execution logs. In Griffin, I designed a four-tier risk classification engine gating host capabilities behind cryptographic approvals. I applied for the Summer 2027 Backend Intern role and am eager to contribute: https://github.com/adhyan-jain",
				},
			},
		},
	}

	// 1. Write the referral_tracker.json file
	jsonData, err := json.MarshalIndent(companies, "", "  ")
	if err != nil {
		fmt.Printf("Error encoding JSON: %v\n", err)
	} else {
		filePath := "/home/adhyan/Documents/Griffin/outreach/referral_tracker.json"
		if err := os.WriteFile(filePath, jsonData, 0644); err != nil {
			fmt.Printf("Error writing file %s: %v\n", filePath, err)
		} else {
			fmt.Printf("Wrote referral tracker ledger to %s\n", filePath)
		}
	}

	// 2. Insert or update the Campaign in outreach_campaigns
	campaignName := "Summer 2027 Tier A Referral Outreach (LinkedIn)"
	campaignGoal := "Targeted employee referral outreach for Summer 2027 Tier A internships across Sarvam AI, Postman, Modal Labs, Inkeep, and StepSecurity (3-5 targeted contacts per company)."
	limits := map[string]any{
		"daily_cap":          15,
		"min_gap_seconds":    300,
		"jitter_seconds":     180,
		"quiet_start_hour":   22,
		"quiet_end_hour":     8,
		"followup_after_day": 5,
		"max_followups":      2,
	}
	limitsJSON, _ := json.Marshal(limits)

	var campaignID uuid.UUID
	err = pool.QueryRow(ctx, `
		SELECT id FROM outreach_campaigns
		WHERE user_id = $1 AND name = $2
	`, userID, campaignName).Scan(&campaignID)

	if err != nil {
		campaignID = uuid.New()
		_, err = pool.Exec(ctx, `
			INSERT INTO outreach_campaigns (
				id, user_id, name, goal, channel, status, limits, created_at, updated_at
			) VALUES ($1, $2, $3, $4, 'linkedin', 'active', $5::jsonb, NOW(), NOW())
		`, campaignID, userID, campaignName, campaignGoal, string(limitsJSON))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating campaign: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Created outreach campaign: %s (ID: %s)\n", campaignName, campaignID)
	} else {
		_, _ = pool.Exec(ctx, `
			UPDATE outreach_campaigns
			SET goal = $1, limits = $2::jsonb, updated_at = NOW()
			WHERE id = $3
		`, campaignGoal, string(limitsJSON), campaignID)
		fmt.Printf("Updated existing campaign (ID: %s)\n", campaignID)
	}

	// 3. Create a task in tasks table for tracking this referral campaign
	var campaignTaskID uuid.UUID
	taskTitle := "Summer 2027 Tier A Referral Outreach Campaign"
	err = pool.QueryRow(ctx, `
		SELECT id FROM tasks WHERE user_id = $1 AND title = $2
	`, userID, taskTitle).Scan(&campaignTaskID)
	if err != nil {
		campaignTaskID = uuid.New()
		taskPayload := map[string]any{
			"campaign_id": campaignID.String(),
			"channel":     "linkedin",
			"tier_a_only": true,
			"companies":   []string{"Sarvam AI", "Postman", "Modal Labs", "Inkeep", "StepSecurity"},
		}
		taskPayloadJSON, _ := json.Marshal(taskPayload)
		_, err = pool.Exec(ctx, `
			INSERT INTO tasks (
				id, user_id, title, goal, status, priority, manager_domain,
				input_payload, created_at, updated_at
			) VALUES (
				$1, $2, $3, $4, 'AWAITING_APPROVAL', 'NORMAL_P2', 'career',
				$5::jsonb, NOW(), NOW()
			)
		`, campaignTaskID, userID, taskTitle, campaignGoal, string(taskPayloadJSON))
		if err != nil {
			fmt.Printf("Error creating campaign task: %v\n", err)
		}
	}

	// 4. Insert each contact into outreach_targets and create an approval card in approvals
	totalInserted := 0
	for _, comp := range companies {
		for _, c := range comp.Contacts {
			infoStr := fmt.Sprintf("%s | %s | %s | %s", comp.Company, c.Title, c.Affiliation, c.PriorityTier)
			var targetID uuid.UUID
			err := pool.QueryRow(ctx, `
				SELECT id FROM outreach_targets
				WHERE campaign_id = $1 AND handle = $2
			`, campaignID, c.Handle).Scan(&targetID)

			if err != nil {
				targetID = uuid.New()
				_, err = pool.Exec(ctx, `
					INSERT INTO outreach_targets (
						id, campaign_id, user_id, name, handle, info, status, draft, created_at, updated_at
					) VALUES ($1, $2, $3, $4, $5, $6, 'drafted', $7, NOW(), NOW())
				`, targetID, campaignID, userID, c.Name, c.Handle, infoStr, c.Draft)
				if err != nil {
					fmt.Printf("Error inserting target %s: %v\n", c.Name, err)
					continue
				}
			} else {
				_, _ = pool.Exec(ctx, `
					UPDATE outreach_targets
					SET name = $1, info = $2, draft = $3, status = 'drafted', updated_at = NOW()
					WHERE id = $4
				`, c.Name, infoStr, c.Draft, targetID)
			}

			// Upsert approval record
			approvalSummary := fmt.Sprintf("Referral outreach: %s (%s at %s - %s)", c.Name, c.Title, comp.Company, c.PriorityTier)
			actionPayload := map[string]any{
				"contact":   fmt.Sprintf("%s (%s)", c.Name, c.Title),
				"company":   comp.Company,
				"priority":  c.PriorityTier,
				"handle":    c.Handle,
				"text":      c.Draft,
				"target_id": targetID.String(),
			}
			payloadJSON, _ := json.Marshal(actionPayload)

			var appID uuid.UUID
			err = pool.QueryRow(ctx, `
				SELECT id FROM approvals
				WHERE user_id = $1 AND action_type = 'REFERRAL_OUTREACH' AND summary = $2
			`, userID, approvalSummary).Scan(&appID)

			if err != nil {
				appID = uuid.New()
				_, err = pool.Exec(ctx, `
					INSERT INTO approvals (
						id, user_id, task_id, action_type, risk_level, status, summary,
						action_payload, created_at, updated_at
					) VALUES ($1, $2, $3, 'REFERRAL_OUTREACH', 'HIGH', 'PENDING', $4, $5::jsonb, NOW(), NOW())
				`, appID, userID, campaignTaskID, approvalSummary, string(payloadJSON))
				if err != nil {
					fmt.Printf("Error creating approval for %s: %v\n", c.Name, err)
				}
			} else {
				_, _ = pool.Exec(ctx, `
					UPDATE approvals
					SET action_payload = $1::jsonb, updated_at = NOW()
					WHERE id = $2
				`, string(payloadJSON), appID)
			}
			totalInserted++
		}
	}

	fmt.Printf("Successfully synchronized %d targets across %d companies in DB and JSON!\n", totalInserted, len(companies))
}

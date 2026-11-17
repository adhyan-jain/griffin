package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TailoringInfo struct {
	Company          string            `json:"company"`
	Role             string            `json:"role"`
	Tier             string            `json:"tier"`
	JobLink          string            `json:"job_link"`
	SourceURL        string            `json:"source_url"`
	BaseResume       string            `json:"base_resume"`
	BulletDiff       string            `json:"bullet_diff"`
	CoverNote        string            `json:"cover_note"`
	ScreeningAnswers map[string]string `json:"screening_answers"`
}

type AppTarget struct {
	Company    string
	Role       string
	Tier       string
	JobLink    string
	SourceURL  string
	BaseResume string
	BulletDiff string
	CoverNote  string
	Answers    map[string]string
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
	err = pool.QueryRow(ctx, "SELECT user_id FROM tasks LIMIT 1").Scan(&userID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "query user_id error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Using User ID: %s\n", userID)

	targets := []AppTarget{
		{
			Company:    "Sarvam AI",
			Role:       "Applied AI / LLM Evaluation Intern",
			Tier:       "Tier A",
			JobLink:    "https://www.sarvam.ai/careers#llm-eval-intern",
			SourceURL:  "https://linkedin.com/in/pratyush-kumar",
			BaseResume: "Adhyan_Jain_Resume_SarvamAI.md",
			BulletDiff: `+ EchoTales: Architected bitemporal narrative knowledge graph querying entity states via state_of(target, timeline, position, observer).
+ EchoTales: Engineered identity coreference resolution engine combining hard rule pre-filters with a 5-feature learned tiebreaker across 21k text spans.
+ EchoTales: Evaluated coreference precision/recall using MUC, B-cubed, and CEAF metrics against gold ground-truth datasets.
+ EchoTales: Built bounded DEFER escalation queue isolating ambiguous model decisions for human-in-the-loop validation.`,
			CoverNote: `Hi Pratyush, I am following Sarvam's work in building foundational models and evaluation benchmarks for Indian languages. In evaluation, the core challenge is verifying factuality, handling coreference across volatile expressions, and grounding model outputs against authoritative sources. Rather than just generic prompting, what I bring to Sarvam is hands-on experience building bitemporal knowledge graphs and evaluation harnesses. In my project EchoTales (https://github.com/adhyan-jain/EchoTales), I engineered an entity resolution engine tracking volatile name shifts, aliases, and retractions across translated text, evaluating coreference precision and recall using MUC, B-cubed, and CEAF metrics across 21,000 spans with a bounded DEFER queue. I can immediately build rigorous evaluation sets, benchmark hallucination rates, and develop structured automated verification harnesses for Sarvam's multi-lingual models.`,
			Answers: map[string]string{
				"Why Sarvam AI?":            "Sarvam is tackling foundational intelligence specifically designed for the Indian linguistic landscape, which requires novel benchmark design rather than standard Western dataset ports.",
				"What skills do you offer?": "Practical experience with bitemporal knowledge graphs, entity coreference evaluation (MUC/B3/CEAF), hallucination metrics, and Python/Ollama inference pipelines.",
			},
		},
		{
			Company:    "Postman",
			Role:       "SDE Intern - Backend & Distributed Systems",
			Tier:       "Tier A",
			JobLink:    "https://careers.postman.com/jobs/sde-intern-summer-2027",
			SourceURL:  "https://careers.postman.com",
			BaseResume: "Adhyan_Jain_Resume_Postman.md",
			BulletDiff: `+ ComputeHive: Engineered distributed task scheduler in Go coordinating job distribution across heterogeneous local/remote nodes.
+ ComputeHive: Enforced atomic state transitions via server-side Redis Lua scripts, strictly serializing claims and eliminating double-leases.
+ ComputeHive: Built outbound pull-based worker protocol over TLS and gRPC, enabling volunteer nodes behind NAT to claim work safely.
+ ComputeHive: Designed heartbeat failure detector (2s interval, 6s eviction) with background reaper requeuing dead worker tasks in <15s.`,
			CoverNote: `Hi Abhinav and Postman Engineering team, As Postman scales its distributed platform and API tooling, system reliability hinges on strict concurrency control, atomic distributed state transitions, and low-latency protocol streaming. What I offer your backend systems team is strong distributed systems engineering in Go and Redis. In my project ComputeHive (https://github.com/adhyan-jain/ComputeHive), I designed a self-hosted distributed task runner where job leasing is strictly serialized through atomic Redis Lua scripts to prevent split-brain execution under heavy concurrency, paired with an outbound pull-based worker daemon over gRPC/TLS and a sub-15s dead-worker recovery reaper. In Griffin (https://github.com/adhyan-jain/griffin), I engineered connection-pooled PostgreSQL state engines with fine-grained capability gating. I can immediately contribute to your high-throughput backend services, gRPC pipelines, and distributed coordination layers.`,
			Answers: map[string]string{
				"What is your core backend expertise?":    "Concurrent systems programming in Go, atomic Redis Lua scripting, gRPC/Protobuf streaming, and PostgreSQL connection pool optimization.",
				"How do you handle worker node failures?": "In ComputeHive, I implemented heartbeat monitoring with lease tokens so orphaned tasks are requeued within 15 seconds without risk of duplicate commits.",
			},
		},
		{
			Company:    "Modal Labs",
			Role:       "Systems & Agent Infrastructure Intern",
			Tier:       "Tier A",
			JobLink:    "https://modal.com/careers/systems-intern-2027",
			SourceURL:  "https://modal.com/careers",
			BaseResume: "Adhyan_Jain_Resume_ModalLabs.md",
			BulletDiff: `+ ComputeHive: Engineered multi-layered container sandbox security: non-root execution, dropped Linux capabilities, read-only root filesystems.
+ ComputeHive: Implemented system backpressure where workers poll /proc/meminfo and /proc/stat to refuse tasks above 90% RAM/CPU/disk with hysteresis.
+ ComputeHive: Developed atomic scheduling state machine powered by Redis Lua scripts, guaranteeing exactly-once lease semantics.
+ Griffin: Built local-first host daemon running under Linux systemd with capability tokens and isolated system privileges.`,
			CoverNote: `Hi Erik, I have been closely following Modal's work on ultra-low-latency serverless container runtimes. The hardest challenges in container orchestration are rapid sandbox initialization, resource backpressure, and zero-leak state coordination. What I can bring to Modal is practical experience building container sandboxes and distributed scheduling runtimes in Go. In ComputeHive (https://github.com/adhyan-jain/ComputeHive), I engineered an execution cluster where untrusted jobs are locked down in ephemeral Docker sandboxes with dropped Linux capabilities, non-root execution, and read-only root filesystems, while compute nodes dynamically regulate work based on /proc/meminfo and /proc/stat hysteresis thresholds at 90% saturation. Stale worker commits are prevented via per-attempt lease tokens. I can hit the ground running on Modal's container runtime, cluster scheduling, and worker isolation primitives.`,
			Answers: map[string]string{
				"Experience with container isolation?":     "Built hardened Docker sandboxes using cgroups, read-only root filesystems, non-root UID enforcement, and dropped Linux capabilities.",
				"Familiarity with distributed scheduling?": "Implemented lease-based pull scheduling with atomic Redis scripts and resource saturation backpressure.",
			},
		},
		{
			Company:    "Inkeep",
			Role:       "Agent Infrastructure & Full-Stack Intern",
			Tier:       "Tier A",
			JobLink:    "https://inkeep.com/careers/agent-infra-intern",
			SourceURL:  "https://wellfound.com/company/inkeep",
			BaseResume: "Adhyan_Jain_Resume_Inkeep.md",
			BulletDiff: `+ Griffin: Architected autonomous agent orchestrator coordinating 11 domain managers across Engineering, Research, and Operations.
+ Griffin: Implemented deterministic intent classification with LLM fallback planning to decompose user goals into structured DAG plans.
+ Griffin: Designed four-tier risk classification engine enforcing human approval gates on sensitive tool execution.
+ Griffin: Developed full-stack Next.js web application with real-time SSE streaming for live task tracking and multi-threaded agent chats.`,
			CoverNote: `Hi Nick, Inkeep's agent search and customer assistance infrastructure depends on reliable multi-step planning, precise context retrieval, and low-latency full-stack execution. What I bring to Inkeep is deep experience building production-grade agent orchestration engines and graph-augmented retrieval systems. In Griffin (https://github.com/adhyan-jain/griffin), I built an autonomous host orchestrator that decomposes user goals into structured DAG plans across 11 manager domains, enforcing capability security tokens and real-time SSE streaming to a Next.js interface. In EchoTales (https://github.com/adhyan-jain/EchoTales), I built a bitemporal knowledge graph answering point-in-time state queries (state_of(target, timeline, position)) with bounded human escalation queues. I can immediately build robust tool-routing layers, context caching pipelines, and full-stack agent interfaces for Inkeep.`,
			Answers: map[string]string{
				"What distinguishes your agent architecture?": "Separation of concerns between deterministic planners, capability-scoped execution tools, and human approval gates for sensitive actions.",
				"Full-stack skills?":                          "Next.js, React, TypeScript, TailwindCSS, Server-Sent Events, Go backends, and PostgreSQL.",
			},
		},
		{
			Company:    "HyperVerge",
			Role:       "Machine Learning Intern (Vision & Multimodal AI)",
			Tier:       "Tier B",
			JobLink:    "https://hyperverge.co/careers/ml-intern-2027",
			SourceURL:  "https://unstop.com",
			BaseResume: "Adhyan_Jain_Resume_HyperVerge.md",
			BulletDiff: `+ EchoTales: Built end-to-end multimodal pipeline transforming long-form fiction into full-cast audiobooks and visual comic panels.
+ EchoTales: Designed temporal character reference sheets maintaining visual attribute consistency across 40 comic panels conditioned on graph state.
+ EchoTales: Implemented dialogue attribution across 21,000 text spans, conditioning speech synthesis prosody on narrative certainty.
+ Griffin: Developed local-first display capture and UI inspection tools for multimodal agent perception.`,
			CoverNote: `Hi Kedar and HyperVerge ML team, As HyperVerge deploys multimodal AI and computer vision models for automated identity verification and document intelligence, the core problem is maintaining cross-modal attribute consistency and resolving messy unstructured text. What I bring to HyperVerge is experience developing multimodal alignment and temporal state tracking pipelines. In EchoTales (https://github.com/adhyan-jain/EchoTales), I architected an end-to-end multimodal adaptation pipeline combining bitemporal graph querying with multi-speaker voice synthesis (TTS prosody conditioning) and visual panel casting, maintaining character visual attribute consistency across evolving panels using point-in-time reference sheets. In Griffin (https://github.com/adhyan-jain/griffin), I built native screen capture and UI inspection pipelines. I can contribute directly to your multimodal document analysis, visual consistency, and identity verification pipelines.`,
			Answers: map[string]string{
				"Multimodal systems experience?": "Built alignment pipelines coupling knowledge graphs with diffusion reference conditioning and multi-speaker voice synthesis.",
				"What value can you deliver?":    "Direct contribution to identity verification, cross-modal consistency verification, and OCR/LLM document analysis pipelines.",
			},
		},
		{
			Company:    "StepSecurity",
			Role:       "Distributed Systems / Backend Intern",
			Tier:       "Tier B",
			JobLink:    "https://www.stepsecurity.io/careers#backend-intern",
			SourceURL:  "https://wellfound.com",
			BaseResume: "Adhyan_Jain_Resume_StepSecurity.md",
			BulletDiff: `+ ComputeHive: Engineered multi-layered container isolation: dropped all Linux capabilities, enforced non-root execution, mounted read-only rootfs.
+ ComputeHive: Implemented immutable audit logging system tracking job inputs and arguments by SHA-256 digest.
+ Griffin: Engineered host agent daemon in Go enforcing fine-grained capability security model and four-tier risk classification engine.
+ Griffin: Built client-side encrypted document vault with passphrase-based encryption for zero-exposure credential protection.`,
			CoverNote: `Hi Ashish, StepSecurity's focus on securing CI/CD pipelines and runtime environments requires systems that strictly enforce least privilege and contain untrusted code. What I can offer StepSecurity is practical engineering in container sandboxing, capability access control, and policy enforcement engines in Go. In ComputeHive (https://github.com/adhyan-jain/ComputeHive), I designed a hardened container runner that executes untrusted code with all Linux capabilities dropped, non-root UID/GID enforcement, read-only root filesystems, and immutable SHA-256 argument audit trails. In Griffin (https://github.com/adhyan-jain/griffin), I designed a four-tier risk classification engine that gates sensitive host capabilities and file modifications behind cryptographic approvals. I can immediately assist in developing StepSecurity's runtime policy agents, network filtering rules, and secure Go backend infrastructure.`,
			Answers: map[string]string{
				"Why StepSecurity?":               "CI/CD and container pipeline security is the critical attack surface for modern engineering organizations, requiring defense-in-depth at the systems layer.",
				"Specific security capabilities?": "Linux capability manipulation, non-root execution sandboxes, policy engines, and encrypted credential vaults in Go.",
			},
		},
	}

	for _, t := range targets {
		taskID := uuid.New()
		approvalID := uuid.New()

		info := TailoringInfo{
			Company:          t.Company,
			Role:             t.Role,
			Tier:             t.Tier,
			JobLink:          t.JobLink,
			SourceURL:        t.SourceURL,
			BaseResume:       t.BaseResume,
			BulletDiff:       t.BulletDiff,
			CoverNote:        t.CoverNote,
			ScreeningAnswers: t.Answers,
		}
		rawPayload, _ := json.Marshal(info)

		taskTitle := fmt.Sprintf("Apply to %s: %s", t.Company, t.Role)
		taskGoal := fmt.Sprintf("Submit tailored job application to %s for %s role with 1-page custom resume and cover pitch.", t.Company, t.Role)

		_, err = pool.Exec(ctx, `
			INSERT INTO tasks (
				id, user_id, title, goal, status, priority, manager_domain,
				input_payload, created_at, updated_at
			) VALUES (
				$1, $2, $3, $4, 'AWAITING_APPROVAL', 'NORMAL_P2', 'career',
				$5::jsonb, NOW(), NOW()
			)
		`, taskID, userID, taskTitle, taskGoal, string(rawPayload))
		if err != nil {
			fmt.Printf("Error inserting task for %s: %v\n", t.Company, err)
			continue
		}

		summary := fmt.Sprintf("Approval required to submit application to %s for %s (%s)", t.Company, t.Role, t.Tier)
		_, err = pool.Exec(ctx, `
			INSERT INTO approvals (
				id, user_id, task_id, action_type, risk_level, status, summary,
				action_payload, created_at, updated_at
			) VALUES (
				$1, $2, $3, 'JOB_APPLY', 'HIGH', 'PENDING', $4,
				$5::jsonb, NOW(), NOW()
			)
		`, approvalID, userID, taskID, summary, string(rawPayload))
		if err != nil {
			fmt.Printf("Error inserting approval for %s: %v\n", t.Company, err)
			continue
		}

		fmt.Printf("Registered approval for %s (Task: %s, Approval: %s)\n", t.Company, taskID, approvalID)
	}
}

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

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
	ResumePath   string `json:"resume_path"`
}

type CompanyCampaign struct {
	Company    string            `json:"company"`
	Role       string            `json:"role"`
	Tier       string            `json:"tier"`
	Location   string            `json:"location"`
	Stipend    string            `json:"stipend"`
	PortalURL  string            `json:"portal_url"`
	ResumeFile string            `json:"resume_file"`
	Contacts   []ReferralContact `json:"contacts"`
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
			Company:    "Sarvam AI",
			Role:       "Applied AI / LLM Evaluation Intern",
			Tier:       "Tier A",
			Location:   "Hybrid (Bengaluru, India)",
			Stipend:    "₹60,000/mo",
			PortalURL:  "https://www.sarvam.ai/careers#llm-eval-intern",
			ResumeFile: "/home/adhyan/Documents/Griffin/resumes/sent/SarvamAI_Applied_AI_LLM_Evaluation_Intern_2026-11-17.md",
			Contacts: []ReferralContact{
				{
					Name:         "Raghavan Aravamudhan",
					Title:        "AI Systems & Research Engineer",
					Handle:       "https://linkedin.com/in/raghavan-aravamudhan",
					Affiliation:  "VIT Vellore Alum (ACM VIT Chapter)",
					PriorityTier: "Priority 1 (Chapter Alumni)",
					Status:       "drafted",
					ResumePath:   "/home/adhyan/Documents/Griffin/resumes/sent/SarvamAI_Applied_AI_LLM_Evaluation_Intern_2026-11-17.md",
					Draft:        "Hi Raghavan, hope you are doing well! I am Adhyan, a 20-year-old 3rd year CSE student at VIT Vellore and currently part of the ACM VIT team. Our chapter recently worked on developer guides and runtime docs for SGLang, and I've also been building evaluation harnesses for bitemporal knowledge graphs in EchoTales (https://github.com/adhyan-jain/EchoTales). I saw your work at Sarvam AI and would love to ask if you might be open to reviewing my attached resume for an internal referral for the Summer 2027 LLM Evaluation Intern role. Thank you so much for your time and guidance!",
				},
				{
					Name:         "Harish Krishnamurthy",
					Title:        "Evaluation & Data Platform Engineer",
					Handle:       "https://linkedin.com/in/harish-krishnamurthy",
					Affiliation:  "VIT Vellore Alum (MFC / ACM Chapter)",
					PriorityTier: "Priority 1 (Chapter Alumni)",
					Status:       "drafted",
					ResumePath:   "/home/adhyan/Documents/Griffin/resumes/sent/SarvamAI_Applied_AI_LLM_Evaluation_Intern_2026-11-17.md",
					Draft:        "Hi Harish, hope you're having a great week! I am Adhyan, a 20-year-old 3rd year student at VIT Vellore. I've been working on LLM factuality evaluation in EchoTales (https://github.com/adhyan-jain/EchoTales), measuring coreference precision and recall with MUC and B-cubed metrics, along with SGLang documentation work with ACM VIT. I am really inspired by Sarvam's evaluation benchmarks for Indic models. If you have a few minutes, would you be open to glancing at my attached resume for an internship referral? I would truly appreciate any feedback or advice!",
				},
				{
					Name:         "Siddharth Nair",
					Title:        "Applied ML Engineer (Indic NLP)",
					Handle:       "https://linkedin.com/in/siddharth-nair-ml",
					Affiliation:  "VIT Vellore Alum (B.Tech CSE)",
					PriorityTier: "Priority 2 (Domain Alumni)",
					Status:       "drafted",
					ResumePath:   "/home/adhyan/Documents/Griffin/resumes/sent/SarvamAI_Applied_AI_LLM_Evaluation_Intern_2026-11-17.md",
					Draft:        "Hi Siddharth, hope you are doing well! I am Adhyan, a junior at VIT Vellore (CSE, batch of 2028). I've been deeply interested in Indic language evaluation, and built bitemporal entity resolution harnesses across translated text in EchoTales (https://github.com/adhyan-jain/EchoTales). I am applying for the Summer 2027 LLM Evaluation Intern position at Sarvam AI. Would you be comfortable taking a quick look at my attached resume to see if I might be a good fit for an internal referral? Thank you for your time!",
				},
				{
					Name:         "Vivek Raghavan",
					Title:        "Co-founder & AI Lead",
					Handle:       "https://linkedin.com/in/vivek-raghavan",
					Affiliation:  "Sarvam AI Leadership",
					PriorityTier: "Priority 3 (Domain Lead / Founder)",
					Status:       "drafted",
					ResumePath:   "/home/adhyan/Documents/Griffin/resumes/sent/SarvamAI_Applied_AI_LLM_Evaluation_Intern_2026-11-17.md",
					Draft:        "Dear Vivek, hope you are having a good week. I am Adhyan, a 20-year-old 3rd year CSE student at VIT Vellore. Our ACM VIT chapter contributed to developer documentation for SGLang, and in my project EchoTales (https://github.com/adhyan-jain/EchoTales), I engineered evaluation harnesses verifying factuality across 21k entity spans. I have applied for the Summer 2027 LLM Evaluation Intern role to help your team benchmark hallucination rates on multi-lingual models. I have attached my resume here and would be deeply grateful for your team's consideration: https://github.com/adhyan-jain",
				},
				{
					Name:         "Pratyush Kumar",
					Title:        "Co-founder",
					Handle:       "https://linkedin.com/in/pratyush-kumar",
					Affiliation:  "Sarvam AI Leadership",
					PriorityTier: "Priority 3 (Founder)",
					Status:       "drafted",
					ResumePath:   "/home/adhyan/Documents/Griffin/resumes/sent/SarvamAI_Applied_AI_LLM_Evaluation_Intern_2026-11-17.md",
					Draft:        "Dear Pratyush, hope you are well. I am Adhyan, a 20-year-old 3rd year undergraduate at VIT Vellore. I closely follow Sarvam's work on foundational models for India. Having authored runtime guides for SGLang with our ACM VIT team and built bitemporal coreference evaluation harnesses in EchoTales (https://github.com/adhyan-jain/EchoTales), I submitted my application for the Summer 2027 LLM Evaluation Intern opening. I've attached my resume for your review and would love the opportunity to learn from and support your team: https://github.com/adhyan-jain",
				},
			},
		},
		{
			Company:    "Postman",
			Role:       "SDE Intern - Backend & Distributed Systems",
			Tier:       "Tier A",
			Location:   "Remote (India)",
			Stipend:    "₹75,000/mo",
			PortalURL:  "https://careers.postman.com/jobs/sde-intern-summer-2027",
			ResumeFile: "/home/adhyan/Documents/Griffin/resumes/sent/Postman_SDE_Intern_Backend_Distributed_Systems_2026-11-17.md",
			Contacts: []ReferralContact{
				{
					Name:         "Ankit Verma",
					Title:        "Senior Backend Engineer (API Platform)",
					Handle:       "https://linkedin.com/in/ankit-verma-backend",
					Affiliation:  "VIT Vellore Alum (ACM VIT Chapter)",
					PriorityTier: "Priority 1 (Chapter Alumni)",
					Status:       "drafted",
					ResumePath:   "/home/adhyan/Documents/Griffin/resumes/sent/Postman_SDE_Intern_Backend_Distributed_Systems_2026-11-17.md",
					Draft:        "Hi Ankit, hope you're having a great week! I am Adhyan, a 20-year-old 3rd year student at VIT Vellore and part of the ACM chapter. I've been spending the past year building distributed systems in Go. In ComputeHive (https://github.com/adhyan-jain/ComputeHive), I built atomic worker lease serialization with Redis Lua scripts, and in Griffin I worked on PostgreSQL connection pooling. I am applying for Postman's Summer 2027 Backend SDE Intern role and would be really grateful for your advice and if you would be open to referring me. Attached my resume for your review!",
				},
				{
					Name:         "Rohit Sundaram",
					Title:        "Distributed Systems Engineer (Cloud Gateway)",
					Handle:       "https://linkedin.com/in/rohit-sundaram-dist",
					Affiliation:  "VIT Vellore Alum (ISTE Chapter)",
					PriorityTier: "Priority 1 (Chapter Alumni)",
					Status:       "drafted",
					ResumePath:   "/home/adhyan/Documents/Griffin/resumes/sent/Postman_SDE_Intern_Backend_Distributed_Systems_2026-11-17.md",
					Draft:        "Hi Rohit, hope you are doing well! I am Adhyan, a 20-year-old 3rd year CSE student at VIT Vellore (ISTE chapter). I really admire Postman's distributed cloud infrastructure. In my project ComputeHive (https://github.com/adhyan-jain/ComputeHive), I designed a pull-based worker daemon in Go over gRPC/TLS with dead-worker lease recovery. I applied for the Backend SDE Intern role for Summer 2027 and would be deeply grateful if you'd be open to reviewing my attached resume for an internal referral. Thanks so much for your guidance!",
				},
				{
					Name:         "Tanya Mittal",
					Title:        "Software Engineer II (Cloud Infrastructure)",
					Handle:       "https://linkedin.com/in/tanya-mittal-cloud",
					Affiliation:  "VIT Vellore Alum (B.Tech CSE)",
					PriorityTier: "Priority 2 (Domain Alumni)",
					Status:       "drafted",
					ResumePath:   "/home/adhyan/Documents/Griffin/resumes/sent/Postman_SDE_Intern_Backend_Distributed_Systems_2026-11-17.md",
					Draft:        "Hi Tanya, hope you're doing well! I'm Adhyan, a 3rd year student from VIT Vellore. I've been focusing heavily on backend engineering in Go, building modular monoliths and capability-gated database layers in Griffin (https://github.com/adhyan-jain/griffin), as well as distributed task scheduling in ComputeHive. I am applying for the Summer 2027 SDE Intern opening at Postman and would love to ask if you'd be open to reviewing my attached resume for a referral. Thank you for your time!",
				},
				{
					Name:         "Rahul Jain",
					Title:        "Staff Backend Architect",
					Handle:       "https://linkedin.com/in/rahul-jain-backend",
					Affiliation:  "Postman Core Infrastructure Team",
					PriorityTier: "Priority 2 (Staff Engineer)",
					Status:       "drafted",
					ResumePath:   "/home/adhyan/Documents/Griffin/resumes/sent/Postman_SDE_Intern_Backend_Distributed_Systems_2026-11-17.md",
					Draft:        "Hi Rahul, hope you are having a productive week! I am Adhyan, a 20-year-old 3rd year student at VIT Vellore studying distributed systems in Go. In ComputeHive (https://github.com/adhyan-jain/ComputeHive), I tackled concurrency and split-brain prevention using atomic Redis Lua scripts and heartbeat monitors. I am applying for Postman's Summer 2027 Backend SDE Intern role. If you have a moment, I would be grateful if you could glance at my attached resume: https://github.com/adhyan-jain. Thank you for your time and consideration!",
				},
				{
					Name:         "Abhinav Asthana",
					Title:        "CEO & Co-founder",
					Handle:       "https://linkedin.com/in/abhinav-asthana",
					Affiliation:  "Postman Leadership",
					PriorityTier: "Priority 3 (Founder)",
					Status:       "drafted",
					ResumePath:   "/home/adhyan/Documents/Griffin/resumes/sent/Postman_SDE_Intern_Backend_Distributed_Systems_2026-11-17.md",
					Draft:        "Dear Abhinav, hope you are well. I am Adhyan, a 20-year-old 3rd year CSE student at VIT Vellore. Postman has been a huge inspiration for my journey into distributed systems and backend engineering. In ComputeHive (https://github.com/adhyan-jain/ComputeHive), I engineered a distributed task runner in Go with atomic Redis Lua leasing and gRPC streaming. I submitted my application for the Summer 2027 Backend SDE Intern role. I have attached my resume and would be thrilled to contribute to Postman's team: https://github.com/adhyan-jain",
				},
			},
		},
		{
			Company:    "Modal Labs",
			Role:       "Systems & Agent Infrastructure Intern",
			Tier:       "Tier A",
			Location:   "Remote (US / Global)",
			Stipend:    "$4,500/mo (₹3,75,000/mo)",
			PortalURL:  "https://modal.com/careers/systems-intern-2027",
			ResumeFile: "/home/adhyan/Documents/Griffin/resumes/sent/ModalLabs_Systems_Agent_Infrastructure_Intern_2026-11-17.md",
			Contacts: []ReferralContact{
				{
					Name:         "Akshat Gupta",
					Title:        "Systems Infrastructure Engineer",
					Handle:       "https://linkedin.com/in/akshat-gupta-systems",
					Affiliation:  "VIT Vellore Alum / US Distributed Systems",
					PriorityTier: "Priority 1 (Alumni / Systems)",
					Status:       "drafted",
					ResumePath:   "/home/adhyan/Documents/Griffin/resumes/sent/ModalLabs_Systems_Agent_Infrastructure_Intern_2026-11-17.md",
					Draft:        "Hi Akshat, hope you are doing well! I am Adhyan, a 20-year-old 3rd year CSE student at VIT Vellore. I've been fascinated by systems and container internals. In ComputeHive (https://github.com/adhyan-jain/ComputeHive), I built a container runner in Go with dropped Linux capabilities, non-root execution, and read-only filesystems, regulated dynamically by /proc/meminfo memory pressure. I'm applying for Modal's Systems & Agent Infrastructure Intern role for Summer 2027 and would be deeply grateful if you'd be open to reviewing my attached resume for a referral!",
				},
				{
					Name:         "Charles Frye",
					Title:        "Systems & Deep Learning Infrastructure Engineer",
					Handle:       "https://linkedin.com/in/charles-frye",
					Affiliation:  "Modal Labs Systems Team",
					PriorityTier: "Priority 2 (Systems Engineer)",
					Status:       "drafted",
					ResumePath:   "/home/adhyan/Documents/Griffin/resumes/sent/ModalLabs_Systems_Agent_Infrastructure_Intern_2026-11-17.md",
					Draft:        "Hi Charles, hope you're having a great week! I am Adhyan, a 20-year-old 3rd year undergraduate at VIT Vellore. I've been studying serverless runtimes and workload isolation. In ComputeHive (https://github.com/adhyan-jain/ComputeHive), I implemented host backpressure in Go by tracking /proc/meminfo hysteresis thresholds to prevent container OOMs. I applied for the Summer 2027 Systems Infrastructure Intern role at Modal Labs and would love to share my attached resume with you. Thank you for your time and work on Modal!",
				},
				{
					Name:         "Bernardo Pires",
					Title:        "Kernel & Virtualization Engineer",
					Handle:       "https://linkedin.com/in/bernardo-pires",
					Affiliation:  "Modal Labs Runtimes Team",
					PriorityTier: "Priority 2 (Kernel / Isolation Engineer)",
					Status:       "drafted",
					ResumePath:   "/home/adhyan/Documents/Griffin/resumes/sent/ModalLabs_Systems_Agent_Infrastructure_Intern_2026-11-17.md",
					Draft:        "Hi Bernardo, hope you are well! I am Adhyan, a 20-year-old student studying container runtimes and process isolation. In ComputeHive (https://github.com/adhyan-jain/ComputeHive), I engineered sandbox execution in Go with read-only filesystems, dropped capabilities, and lease tokens in Redis to reject stale commits. I have submitted my application for the Systems Intern position for Summer 2027. I would be honored if you could take a look at my attached resume and code. Thank you for your time!",
				},
				{
					Name:         "Erik Bernhardsson",
					Title:        "CEO & Founder",
					Handle:       "https://linkedin.com/in/erik-bernhardsson",
					Affiliation:  "Modal Labs Leadership",
					PriorityTier: "Priority 3 (Founder)",
					Status:       "drafted",
					ResumePath:   "/home/adhyan/Documents/Griffin/resumes/sent/ModalLabs_Systems_Agent_Infrastructure_Intern_2026-11-17.md",
					Draft:        "Dear Erik, hope you are well. I am Adhyan, a 20-year-old 3rd year CSE student at VIT Vellore. Modal's serverless container runtime is one of the most exciting systems projects in tech. In ComputeHive (https://github.com/adhyan-jain/ComputeHive), I built container execution runtimes in Go with dropped capabilities, read-only rootfs, and memory saturation backpressure. I applied for the Summer 2027 Systems Intern role and have attached my resume here: https://github.com/adhyan-jain. Thank you for your time and consideration!",
				},
			},
		},
		{
			Company:    "Inkeep",
			Role:       "Agent Infrastructure & Full-Stack Intern",
			Tier:       "Tier A",
			Location:   "Remote (US / Global)",
			Stipend:    "$2,500/mo (₹2,05,000/mo)",
			PortalURL:  "https://inkeep.com/careers/agent-infra-intern",
			ResumeFile: "/home/adhyan/Documents/Griffin/resumes/sent/Inkeep_Agent_Infrastructure_FullStack_Intern_2026-11-17.md",
			Contacts: []ReferralContact{
				{
					Name:         "Pranav Kulkarni",
					Title:        "Full-Stack & Search Infrastructure Engineer",
					Handle:       "https://linkedin.com/in/pranav-kulkarni-dev",
					Affiliation:  "VIT Vellore Alum",
					PriorityTier: "Priority 1 (Alumni / Full-Stack)",
					Status:       "drafted",
					ResumePath:   "/home/adhyan/Documents/Griffin/resumes/sent/Inkeep_Agent_Infrastructure_FullStack_Intern_2026-11-17.md",
					Draft:        "Hi Pranav, hope you're doing well! I'm Adhyan, a 20-year-old 3rd year CSE student at VIT Vellore. I've been following Inkeep's search infrastructure closely. In Griffin (https://github.com/adhyan-jain/griffin), I designed an autonomous agent orchestrator in Go with DAG task scheduling and real-time SSE streaming to a Next.js UI. I am applying for the Agent Infrastructure & Full-Stack Intern opening for Summer 2027 and would be deeply grateful if you could glance at my attached resume for an internal referral. Thanks so much!",
				},
				{
					Name:         "Alex Mercer",
					Title:        "Lead Agent Systems Engineer",
					Handle:       "https://linkedin.com/in/alex-mercer-ai",
					Affiliation:  "Inkeep Agent Platform Team",
					PriorityTier: "Priority 2 (Lead Engineer)",
					Status:       "drafted",
					ResumePath:   "/home/adhyan/Documents/Griffin/resumes/sent/Inkeep_Agent_Infrastructure_FullStack_Intern_2026-11-17.md",
					Draft:        "Hi Alex, hope you're having a good week! I am Adhyan, a 20-year-old student at VIT Vellore building agent execution engines. In Griffin (https://github.com/adhyan-jain/griffin), I engineered an orchestrator with 11 lifecycle states, capability-gated security boundaries, and live SSE streaming to Next.js. In EchoTales, I built bitemporal state queries. I applied for the Summer 2027 Agent Infrastructure Intern role at Inkeep. I have attached my resume for your review and would love any feedback on my profile!",
				},
				{
					Name:         "David Liang",
					Title:        "Retrieval & Knowledge Graph Engineer",
					Handle:       "https://linkedin.com/in/david-liang-search",
					Affiliation:  "Inkeep Retrieval Team",
					PriorityTier: "Priority 2 (Retrieval Engineer)",
					Status:       "drafted",
					ResumePath:   "/home/adhyan/Documents/Griffin/resumes/sent/Inkeep_Agent_Infrastructure_FullStack_Intern_2026-11-17.md",
					Draft:        "Hi David, hope you are well! I am Adhyan, a 3rd year undergraduate at VIT Vellore. I've been exploring how temporal entity shifts impact retrieval. In EchoTales (https://github.com/adhyan-jain/EchoTales), I engineered a bitemporal knowledge graph answering point-in-time state queries across 21k entity spans. I applied for the Summer 2027 Agent Infrastructure Intern opening at Inkeep and would be very grateful if you might be open to reviewing my attached resume for an internal referral. Thank you for your time!",
				},
				{
					Name:         "Nick D'Amato",
					Title:        "Founder & CEO",
					Handle:       "https://linkedin.com/in/nick-damato",
					Affiliation:  "Inkeep Leadership",
					PriorityTier: "Priority 3 (Founder)",
					Status:       "drafted",
					ResumePath:   "/home/adhyan/Documents/Griffin/resumes/sent/Inkeep_Agent_Infrastructure_FullStack_Intern_2026-11-17.md",
					Draft:        "Dear Nick, hope you are having a great week. I am Adhyan, a 20-year-old 3rd year CSE student at VIT Vellore. Inkeep's agent search platform has been inspiring to follow. In Griffin (https://github.com/adhyan-jain/griffin), I engineered an autonomous agent runtime in Go with DAG dependencies, capability boundaries, and Next.js SSE streaming. I submitted my application for the Summer 2027 Agent Infrastructure Intern role. Attached my resume for your review: https://github.com/adhyan-jain. Thank you for your time!",
				},
			},
		},
		{
			Company:    "StepSecurity",
			Role:       "Distributed Systems / Backend Intern",
			Tier:       "Tier A",
			Location:   "Remote (India / US)",
			Stipend:    "₹20,000/mo",
			PortalURL:  "https://www.stepsecurity.io/careers#backend-intern",
			ResumeFile: "/home/adhyan/Documents/Griffin/resumes/sent/StepSecurity_Distributed_Systems_Backend_Intern_2026-11-17.md",
			Contacts: []ReferralContact{
				{
					Name:         "Varun Shenoy",
					Title:        "Security & Infrastructure Engineer",
					Handle:       "https://linkedin.com/in/varun-shenoy-sec",
					Affiliation:  "VIT Vellore Alum (ACM VIT Chapter)",
					PriorityTier: "Priority 1 (Chapter Alumni)",
					Status:       "drafted",
					ResumePath:   "/home/adhyan/Documents/Griffin/resumes/sent/StepSecurity_Distributed_Systems_Backend_Intern_2026-11-17.md",
					Draft:        "Hi Varun, hope you're doing well! I am Adhyan, a 20-year-old 3rd year student at VIT Vellore and part of the ACM team. In ComputeHive (https://github.com/adhyan-jain/ComputeHive), I designed a container sandbox in Go executing with dropped Linux capabilities, non-root UID/GID, and read-only filesystems. In Griffin, I built capability-gated security. I applied for the Distributed Systems / Backend Intern role at StepSecurity for Summer 2027 and would be really grateful for your advice and a referral if you think my profile aligns. Attached my resume!",
				},
				{
					Name:         "Karthik Balakrishnan",
					Title:        "CI/CD Runtime & Kernel Security Engineer",
					Handle:       "https://linkedin.com/in/karthik-balakrishnan-sec",
					Affiliation:  "StepSecurity Core Engineering",
					PriorityTier: "Priority 2 (Security Systems Engineer)",
					Status:       "drafted",
					ResumePath:   "/home/adhyan/Documents/Griffin/resumes/sent/StepSecurity_Distributed_Systems_Backend_Intern_2026-11-17.md",
					Draft:        "Hi Karthik, hope you are having a good week! I am Adhyan, a 20-year-old 3rd year CSE student at VIT Vellore. I follow StepSecurity's work on CI/CD runner protection closely. In ComputeHive (https://github.com/adhyan-jain/ComputeHive), I implemented hardened container execution with immutable SHA-256 argument logs in Go, and in Griffin I built cryptographic approval gating for host tools. I applied for the Summer 2027 Backend Intern role and would be deeply grateful if you could glance at my attached resume. Thank you!",
				},
				{
					Name:         "Ashish Kurmi",
					Title:        "Co-founder & CEO",
					Handle:       "https://linkedin.com/in/ashish-kurmi",
					Affiliation:  "StepSecurity Leadership",
					PriorityTier: "Priority 3 (Founder)",
					Status:       "drafted",
					ResumePath:   "/home/adhyan/Documents/Griffin/resumes/sent/StepSecurity_Distributed_Systems_Backend_Intern_2026-11-17.md",
					Draft:        "Dear Ashish, hope you are well. I am Adhyan, a 20-year-old 3rd year student at VIT Vellore. StepSecurity's mission to protect CI/CD pipelines is critical. In ComputeHive (https://github.com/adhyan-jain/ComputeHive), I engineered a Go container runner with dropped capabilities, non-root execution, and immutable audit logs, while in Griffin I built a 4-tier risk classification engine. I applied for the Summer 2027 Backend Intern role. I have attached my resume for your consideration: https://github.com/adhyan-jain. Thank you for your time!",
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
			fmt.Printf("Wrote updated polite referral tracker ledger to %s\n", filePath)
		}
	}

	// 2. Upsert Campaign in outreach_campaigns
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
	}

	// 3. Find campaign task ID
	var campaignTaskID uuid.UUID
	taskTitle := "Summer 2027 Tier A Referral Outreach Campaign"
	_ = pool.QueryRow(ctx, `SELECT id FROM tasks WHERE user_id = $1 AND title = $2`, userID, taskTitle).Scan(&campaignTaskID)

	// 4. Update all 21 targets in outreach_targets and approvals with polite drafts & attached resume
	totalUpdated := 0
	for _, comp := range companies {
		for _, c := range comp.Contacts {
			infoStr := fmt.Sprintf("%s | %s | %s | %s | Resume: %s", comp.Company, c.Title, c.Affiliation, c.PriorityTier, c.ResumePath)
			var targetID uuid.UUID
			err := pool.QueryRow(ctx, `
				SELECT id FROM outreach_targets
				WHERE campaign_id = $1 AND handle = $2
			`, campaignID, c.Handle).Scan(&targetID)

			if err == nil {
				_, _ = pool.Exec(ctx, `
					UPDATE outreach_targets
					SET name = $1, info = $2, draft = $3, status = 'drafted', updated_at = NOW()
					WHERE id = $4
				`, c.Name, infoStr, c.Draft, targetID)
			} else {
				targetID = uuid.New()
				_, _ = pool.Exec(ctx, `
					INSERT INTO outreach_targets (
						id, campaign_id, user_id, name, handle, info, status, draft, created_at, updated_at
					) VALUES ($1, $2, $3, $4, $5, $6, 'drafted', $7, NOW(), NOW())
				`, targetID, campaignID, userID, c.Name, c.Handle, infoStr, c.Draft)
			}

			// Update approval
			approvalSummary := fmt.Sprintf("Referral outreach: %s (%s at %s - %s)", c.Name, c.Title, comp.Company, c.PriorityTier)
			actionPayload := map[string]any{
				"contact":     fmt.Sprintf("%s (%s)", c.Name, c.Title),
				"company":     comp.Company,
				"priority":    c.PriorityTier,
				"handle":      c.Handle,
				"text":        c.Draft,
				"resume_path": c.ResumePath,
				"target_id":   targetID.String(),
			}
			payloadJSON, _ := json.Marshal(actionPayload)

			var appID uuid.UUID
			err = pool.QueryRow(ctx, `
				SELECT id FROM approvals
				WHERE user_id = $1 AND action_type = 'REFERRAL_OUTREACH' AND summary = $2
			`, userID, approvalSummary).Scan(&appID)

			if err == nil {
				_, _ = pool.Exec(ctx, `
					UPDATE approvals
					SET action_payload = $1::jsonb, updated_at = NOW()
					WHERE id = $2
				`, string(payloadJSON), appID)
			} else {
				appID = uuid.New()
				_, _ = pool.Exec(ctx, `
					INSERT INTO approvals (
						id, user_id, task_id, action_type, risk_level, status, summary,
						action_payload, created_at, updated_at
					) VALUES ($1, $2, $3, 'REFERRAL_OUTREACH', 'HIGH', 'PENDING', $4, $5::jsonb, NOW(), NOW())
				`, appID, userID, campaignTaskID, approvalSummary, string(payloadJSON))
			}
			totalUpdated++
		}
	}
	fmt.Printf("Updated %d targets and approvals with polite drafts & attached resume paths!\n", totalUpdated)

	// 5. IMPLEMENT EXAM PAUSE on DSA COACH (0896a438-b073-47fb-8a45-d60f0fa98729)
	// User requested: "pause this schedule till 29 november, i have my exams going on"
	resumptionTime, _ := time.Parse(time.RFC3339, "2026-11-29T02:00:00Z") // 07:30 IST is 02:00 UTC
	var dsaPayload []byte
	_ = pool.QueryRow(ctx, "SELECT input_payload FROM tasks WHERE id = '0896a438-b073-47fb-8a45-d60f0fa98729'").Scan(&dsaPayload)
	var dsaMap map[string]any
	if len(dsaPayload) > 0 {
		_ = json.Unmarshal(dsaPayload, &dsaMap)
	} else {
		dsaMap = make(map[string]any)
	}
	dsaMap["paused_until"] = "2026-11-29T07:30:00+05:30"
	dsaMap["pause_reason"] = "Exams active through November 28; resuming November 29 07:30 IST per user directive"
	dsaMap["exam_hold"] = true
	newDsaPayload, _ := json.Marshal(dsaMap)

	res, err := pool.Exec(ctx, `
		UPDATE tasks
		SET scheduled_for = $1,
		    status = 'QUEUED',
		    input_payload = $2::jsonb,
		    failure_reason = 'PAUSED_FOR_EXAMS_UNTIL_2026-11-29',
		    updated_at = NOW()
		WHERE id = '0896a438-b073-47fb-8a45-d60f0fa98729'
	`, resumptionTime, string(newDsaPayload))
	if err != nil {
		fmt.Printf("Error updating DSA coach pause: %v\n", err)
	} else {
		fmt.Printf("DSA Coach schedule successfully paused until 2026-11-29 07:30 IST (%d rows updated)\n", res.RowsAffected())
	}

	// 6. UPDATE INTERNSHIP TRACKER (67e3433d-4348-45c3-bbee-20d039632daf) for APPEND-ONLY INVARIANT
	var trackerPayload []byte
	_ = pool.QueryRow(ctx, "SELECT input_payload FROM tasks WHERE id = '67e3433d-4348-45c3-bbee-20d039632daf'").Scan(&trackerPayload)
	var trMap map[string]any
	if len(trackerPayload) > 0 {
		_ = json.Unmarshal(trackerPayload, &trMap)
	} else {
		trMap = make(map[string]any)
	}
	trMap["append_only"] = true
	trMap["deduplicate_keys"] = []string{"company", "role", "portal_url"}
	trMap["never_replace_history"] = true
	newTrPayload, _ := json.Marshal(trMap)

	appendGoal := `Sources: Wellfound, LinkedIn Jobs (public logged-out pages), careers pages of target companies in ~/.griffin-host/internships/targets.json, Hacker News "Who is hiring", Unstop, Internshala.
Specialized Focus: US-based and foreign-based companies offering remote internships, plus top-tier India tech.
Roles: SDE/backend, distributed systems, applied AI, LLM evaluation, agent infrastructure, systems security, full-stack.
Cohort: Summer 2027 (mid-May to Aug 2027), 2028 batch eligible.
Tier A Definition (Strict Active Target):
- Remote Roles: Stipend >= Rs 20,000/month (or foreign USD equivalent: $250+/month).
- Onsite / Hybrid Roles: Stipend >= Rs 30,000/month.
Other Tiers: Mark Tier B or Tier C. Griffin active application pipeline strictly targets Tier A.
CRITICAL INVARIANT: APPEND-ONLY TRACKING.
- Always read existing listings in open_tracker.json, tracker.csv, and tracker.md.
- Deduplicate against existing records using (company, role, link).
- Append new rows to the cumulative history. NEVER overwrite, replace, or truncate previous entries.
- Forward newly appended Tier A opportunities directly to Application Pipeline.`

	_, _ = pool.Exec(ctx, `
		UPDATE tasks
		SET goal = $1,
		    input_payload = $2::jsonb,
		    updated_at = NOW()
		WHERE id = '67e3433d-4348-45c3-bbee-20d039632daf'
	`, appendGoal, string(newTrPayload))
	fmt.Println("Enforced APPEND-ONLY invariant on INTERNSHIP TRACKER task.")
}

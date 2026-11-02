# Griffin System Overview

## 1. Executive Summary

**Griffin** is a cloud-first personal AI workforce platform managed by a top-level orchestrator named **JJ**. The platform functions as an organized, multi-tiered digital workforce rather than a loose collection of disconnected chatbots. Griffin empowers an individual to operate with the capacity, intelligence, and coordination of an elite multi-department organization.

JJ acts as the executive Chief of Staff: receiving user requests, understanding strategic intent, decomposing goals into multi-stage Directed Acyclic Graphs (DAGs), delegating execution to 11 specialist domain managers, coordinating on-demand dynamic workers, enforcing strict human approval gates for high-risk actions, and delivering unified proactive briefings.

Griffin is designed **cloud-first**: it executes 24/7 on dedicated cloud infrastructure independent of whether the user's laptop is connected, sleeping, or offline.

---

## 2. Core Architectural Principles

1. **Autonomous but Accountable (Human-in-the-Loop)**
   All actions with external side effects (sending emails, submitting job applications, financial transactions, destructive git pushes) require explicit human approval via the Griffin Approval Center. Read-only research, analysis, document drafting, and internal planning execute autonomously.

2. **Cloud-First Persistence**
   The core engine (Go Modular Monolith, PostgreSQL, Scheduler) resides permanently in the cloud. Scheduled routines (daily briefings at 08:00 IST, market scans, application status checks) trigger regardless of local device status.

3. **Provider-Neutral Agent Abstraction**
   Agent definitions are decoupled from underlying LLM vendors. Tasks specify capability requirements, reasoning budgets, and tool access; a central router maps these to the most cost-effective and capable provider (Google Antigravity CLI, Anthropic Claude Cowork, Anthropic Messages API, Google GenAI / Gemini, or Claude Code).

4. **Stateless Workers, Stateful Engine**
   Workers are lightweight task execution specifications executed on-demand, not permanently spinning LLM sessions consuming idle resources. All durable state—tasks, dependencies, leases, memory, audit trails—resides in Supabase PostgreSQL.

5. **Pragmatic Modular Monolith**
   Griffin intentionally avoids distributed microservice complexity. A single, high-performance Go binary houses the REST/SSE API, task queue, scheduler, and manager orchestration logic, enabling low operational overhead, straightforward local testing, and atomic deployments.

---

## 3. High-Level System Topology

```mermaid
flowchart TD
    subgraph Clients ["Client Layer (Desktop & Mobile PWA)"]
        Browser[Next.js 15 PWA App]
        PWA_Worker[Service Worker / Push Notifications]
    end

    subgraph CloudHosting ["Cloud Infrastructure (Railway / Render)"]
        subgraph GoMonolith ["Griffin Core (Go 1.23 Modular Monolith)"]
            Router[HTTP REST & SSE Gateway]
            JJ[JJ Orchestration Engine]
            Managers[11 Specialist Domain Managers]
            TaskEngine[Task DAG & Distributed Queue Engine]
            Scheduler[Durable Cron Scheduler (Asia/Kolkata)]
            ProviderRouter[Provider-Neutral Execution Router]
            SecurityMiddleware[Auth & Approval Gatekeeper]
        end
    end

    subgraph DataPlane ["Persistence & Auth (Supabase Cloud)"]
        PG[(PostgreSQL 16 + RLS + pgvector)]
        Auth[Supabase Auth / GoTrue]
        ObjectStore[Supabase Storage / S3 Bucket]
    end

    subgraph KnowledgeLayer ["Workspace & Knowledge Layer"]
        GDrive[Google Drive Workspace]
        LocalDrive[Local Synced Mirror]
    end

    subgraph ExecutionLayer ["AI Execution Environments"]
        GeminiAPI[Direct Gemini 2.5/3.x API]
        AnthropicAPI[Direct Claude 3.5/3.7 API]
        AGY_CLI[Antigravity CLI Runner]
        ClaudeCowork[Claude Cowork (Inbox/Outbox Protocol)]
        ClaudeCodeCLI[Claude Code CLI Runner]
    end

    Browser <-->|HTTPS REST / SSE| Router
    PWA_Worker <-->|Web Push| Router
    Router --> SecurityMiddleware
    SecurityMiddleware --> JJ
    JJ <--> Managers
    Managers --> TaskEngine
    Scheduler --> TaskEngine
    TaskEngine <-->|FOR UPDATE SKIP LOCKED| PG
    SecurityMiddleware <--> Auth
    TaskEngine <--> ObjectStore
    TaskEngine --> ProviderRouter
    
    ProviderRouter --> GeminiAPI
    ProviderRouter --> AnthropicAPI
    ProviderRouter --> AGY_CLI
    ProviderRouter --> ClaudeCowork
    ProviderRouter --> ClaudeCodeCLI
    
    ClaudeCowork <--> GDrive
    Managers <--> GDrive
```

---

## 4. Component Responsibilities

| Component | Technology | Primary Responsibilities |
| :--- | :--- | :--- |
| **PWA Frontend** | Next.js 15, TypeScript, Tailwind CSS, shadcn/ui | Unified responsive web/mobile interface; JJ conversation canvas; Task DAG visualizer; Approval Center; Opportunity tracking; Provider health dashboard; Web Push notification handler. |
| **Core Monolith** | Go 1.23 (`net/http`, `chi` router) | High-throughput API; domain services; JJ prompt orchestration; task state machine; distributed queue worker; cron scheduling; token encryption; audit logging. |
| **Task Engine** | Go + PostgreSQL (`SKIP LOCKED`) | Persistent DAG management; parent-child dependency resolution; lease distribution; heartbeat monitoring; dead-worker reclamation; idempotency verification. |
| **Scheduler** | Go (`robfig/cron/v3`) + PostgreSQL | Timezone-aware (Asia/Kolkata) recurring job runner; missed execution catch-up; conditional workflow triggers; automated daily briefings. |
| **Database** | Supabase PostgreSQL 16 | Relational storage for tasks, agents, runs, approvals, opportunities, and audit logs; Row-Level Security (RLS) enforcement; `pgvector` for semantic document embeddings. |
| **Auth & Security** | Supabase Auth (GoTrue) + AES-GCM-256 | JWT verification; user session management; encrypted storage of third-party OAuth tokens (Google Drive, GitHub). |
| **Object Storage** | S3-compatible / Supabase Storage | Long-term storage of execution artifacts, task logs, generated PDFs, code diffs, raw web scrapings. |
| **Document Workspace**| Google Drive API | Human-readable document collaboration, structured deliverable storage, and Claude Cowork Inbox/Outbox handoff bridge. |
| **Execution Router**| Go Provider Abstraction Layer | Capability-based routing, timeout handling, retries with exponential backoff, rate limiting, and fallback between execution environments. |

---

## 5. Information Flow & Life of a Request

1. **Ingress**: The user submits a prompt (e.g., *"Find relevant NeurIPS 2026 AI agent papers, summarize key ideas, and draft a research memo"*) via the Next.js PWA.
2. **Intent Parsing & Decomposition**: JJ receives the prompt, loads user profile context and active memory, classifies intent, and determines that this requires the **Research Manager**.
3. **DAG Generation**: The Research Manager decomposes the goal into subtasks:
   - *Task 1*: Query arXiv and NeurIPS proceedings for recent agent papers (Worker: Literature Scraper).
   - *Task 2*: Filter and summarize top 5 papers (Worker: Deep Research Analyst).
   - *Task 3*: Generate structured research memo in Google Drive (Worker: Technical Writer).
4. **Queue & Lease Allocation**: Tasks are inserted into the PostgreSQL `tasks` table with dependencies (`Task 2` depends on `Task 1`; `Task 3` depends on `Task 2`).
5. **Worker Execution**: The task engine picks up `Task 1` via `SELECT FOR UPDATE SKIP LOCKED`, acquires a 60-second lease, and dispatches it through the Provider Router.
6. **Provider Execution**: Provider Router evaluates capabilities: literature scraping runs via direct Gemini API with search grounding. Results are written to task outputs.
7. **Subsequent Pipeline & Artifact Generation**: When `Task 1` completes, `Task 2` transitions to `READY`. For `Task 3`, the writer creates a structured Google Doc via Google Drive API and outputs an artifact link.
8. **Synthesis & Briefing**: JJ aggregates the completed outputs, performs a sanity check, updates task state to `COMPLETED`, and delivers a conversational summary to the user with clickable links to the generated artifacts.

---

## 6. Target Deployment Footprint (V1)

Griffin V1 is optimized for zero operational headache, high uptime, and modest infrastructure cost for a single-user power deployment:

- **Compute**: Single containerized Go backend running on **Railway** ($5–$10/month) with persistent volume for scratch workspaces.
- **Frontend**: Hosted on **Vercel** (Hobby / Pro tier, $0–$20/month) or co-located on Railway.
- **Database & Storage**: Managed **Supabase** (Free or Pro tier $25/month).
- **Google Workspace**: Dedicated Google Workspace / Google One account for Antigravity & Drive integration.
- **AI Subscriptions**: Existing Google Antigravity account, Anthropic Claude Pro/Team subscription, and targeted pay-as-you-go API keys for cloud-native fallbacks.

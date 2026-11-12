# Griffin Agent Operating Manual (agents.md)

Welcome to the **Griffin** codebase. This document is the primary context and operating manual for AI agents (and human engineers) contributing to this repository.

Before modifying code or proposing architectural changes, you **must** read and adhere to the guidelines, architectural invariants, and decision records documented here.

---

## 1. Project Mission & Identity

**Griffin** is a cloud-first personal AI workforce.
- **JJ** is the top-level orchestrator and Chief of Staff.
- JJ coordinates **11 specialist domain managers** (Career, Research, Engineering, Networking, Finance, etc.), which delegate work to specialist workers.
- Griffin runs on an AWS EC2 Linux runner in Mumbai (`ap-south-1`) with on-demand scale-to-zero, so the user's laptop is **never required to remain open or online**, while compute stops when idle to minimize costs (>90% compute savings).
- **Antigravity CLI (`agy`)** under the user's existing subscription is the **PRIMARY** execution engine, delivering high-intelligence reasoning with **zero per-token API billing**.

---

## 2. Canonical Living Ledgers

Every agent working on Griffin must consult and update these two living ledgers:

1. **[`decisions.md`](decisions.md)**:
   - **All that has been done and all that will be done.**
   - Contains all Architectural Decision Records (ADRs), technical choices, and planned phase milestones.
   - *Rule*: When making an architectural choice, state machine adjustment, or infrastructure decision, you must document it in `decisions.md`.
2. **[`evolution.md`](evolution.md)**:
   - **How the product changed from one thing to another as we built it.**
   - Documents the history of assumptions, empirical findings, and product pivots.
   - *Rule*: When building code reveals that an architectural theory was flawed or requires a pivot, chronicle that insight in `evolution.md`.
3. **[`docs/PRD.md`](docs/PRD.md)**:
   - Live phase tracker, deliverable statuses, and automated test scorecard.

---

## 3. Core Architectural Invariants (Non-Negotiables)

1. **Antigravity CLI is PRIMARY (ADR-009)**:
   - JJ and all background workers route tasks to the authenticated `agy` CLI on EC2 by default.
   - No Google AI Studio, Anthropic, or OpenAI API keys are required for V1 task execution. For ultra-low latency voice/intent classification and conversational auto-threading (ADR-084, ADR-107), lightweight Gemini Flash Lite is used when `GEMINI_API_KEY` is provided, gracefully falling back to local heuristic matching if absent.
2. **Strict Prohibition of Silent Paid Fallback (ADR-014)**:
   - Tasks default to `allow_paid_fallback = false`.
   - If Antigravity credentials expire or fail canary checks, tasks pause in `BLOCKED_AWAITING_PROVIDER` and alert the user. Never silently bill per-token APIs for worker labor.
3. **PostgreSQL `SKIP LOCKED` for Queueing (ADR-002)**:
   - Do not introduce Redis, RabbitMQ, Celery, or external message brokers.
   - Task claiming uses PostgreSQL transactions with `SELECT ... FOR UPDATE SKIP LOCKED`.
4. **Time-Bounded Leases & Recovery (ADR-006)**:
   - Running tasks have distributed leases (default 60s) renewed by worker heartbeats every 15s.
   - Abandoned or timed-out tasks are reclaimed by the background `LeaseReaper` daemon.
5. **Bounded Concurrency on EC2 (ADR-013)**:
   - Hard cap of **2 concurrent `agy` subprocesses** on the EC2 host.
   - EC2 instance is configured with a **2 GiB swapfile** to prevent Linux OOM kills.
6. **Zero Inbound Attack Surface (ADR-012)**:
   - Inbound port 22 (SSH) is completely closed.
   - All host access and initial interactive OAuth setup occur via **AWS Systems Manager (SSM) Session Manager**.
7. **Database-Level Tenant Isolation (ADR-003)**:
   - `FORCE ROW LEVEL SECURITY` is enabled across all tenant tables.
   - Never trust frontend-supplied user IDs; validate Supabase JWTs and enforce `auth.uid() = user_id`.
8. **On-Demand Scale-to-Zero & Atomic Pre-Shutdown Safety (ADR-029, ADR-031)**:
   - EC2 compute scales to zero after a configurable idle period (`IDLE_SHUTDOWN_SECONDS`, default 300s).
   - Ingress API remains available 24/7 independently, returning `202 Accepted` on queued work and waking the runner asynchronously.
   - The idle shutdown monitor enforces an atomic PostgreSQL recheck (`CountRunnableTasks`) immediately prior to issuing system shutdown, completely neutralizing shutdown race conditions.

---

## 4. Current Implementation State

| Phase / Milestone | Description | Status |
| :--- | :--- | :--- |
| **Phase 0** | Architecture & Feasibility Audit (18 architecture documents) | **COMPLETE** |
| **Phase 0.5** | AWS Antigravity Cloud Runner Proof of Concept & Empirical Benchmarks | **VALIDATED & STAGED** |
| **Phase 1** | Core Engine & Data Foundations (Go Monolith, Migrations, Task Engine, RLS, REST API) | **COMPLETE (25/25 Tests PASS)** |
| **Phase 2** | Antigravity Primary Execution Provider & Subprocess Containment | **COMPLETE (37/37 Tests PASS)** |
| **Scale-to-Zero** | On-Demand EC2 Scale-to-Zero, Wake-up Controller, Idle Shutdown, EventBridge Tick | **IMPLEMENTED (42/42 Tests PASS)** |
| **Phase 3** | JJ Orchestration Engine, 11 Specialist Managers, Agent Factory & Capability Taxonomy | **COMPLETE (62/62 Tests PASS)** |
| **Phase 4** | AWS S3 Object Storage Integration & Dual-Persistence (Presigned URLs, Multipart, Compensating Saga) | **COMPLETE (76/76 Tests PASS, LIVE VERIFIED)** |
| **Phase 5** | Next.js 15 PWA Interface (10 Screens, TypeScript, Tailwind, Responsive Mobile Drawer/Navbar) | **COMPLETE (0 Build Errors, Tested)** |
| **Phase 6** | Production Hardening, Cloud Release Runbooks & Unattended Antigravity Execution | **COMPLETE** |
| **Epoch 12** | Outreach engine (`migrations/000007`, `/outreach`), browser hand (`internal/hands/browser`), editable approvals, discuss-then-go-ahead, phone self-registration, listener state machine | **BUILT; Gmail/LinkedIn and the phone-side hand NOT verified (see `docs/ROADMAP.md`)** |

---

## 5. Repository Directory Layout

```text
griffin/
├── cmd/
│   └── griffin-server/      # Main entrypoint (server lifecycle, background daemons, startup sync)
├── web/                     # Next.js 15 TypeScript PWA Frontend (10 Management Screens)
│   ├── src/
│   │   ├── app/             # App Router: /, /chat, /tasks, /approvals, /artifacts, /workforce, /settings, /login
│   │   ├── components/      # Navigation, RunnerPill, StatusBadge
│   │   └── lib/             # Typed API client & auth session management
│   └── public/              # PWA manifest, icons
├── internal/
│   ├── api/                 # HTTP routing, CORS middleware, server setup
│   │   ├── handlers/        # Tasks, Approvals, Agents, Artifacts, Workers, Orchestrator, WorkerDefs, Health, Auth, Runner
│   │   ├── httputil/        # Standard JSON envelopes & error mapping
│   │   └── middleware/      # CORS, Supabase JWT Auth, Worker Auth, Logger, Recovery, RequestID
│   ├── config/              # Environment-driven configuration (S3, runner, AGY, database)
│   ├── database/            # pgxpool connection pool, migrations runner, tx helpers
│   ├── domain/              # Task state machine, capabilities, orchestrations, worker definitions, sentinel errors
│   ├── manager/             # 11 Specialist managers, Registry, deliverable verification
│   ├── provider/            # AI execution providers (Antigravity subprocess, mock)
│   ├── repository/          # PostgreSQL repositories (SKIP LOCKED, RLS, artifacts, orchestrations, worker defs)
│   ├── runner/              # Scale-to-zero instance manager, wakeup controller, idle shutdown monitor
│   ├── service/             # TaskEngine, JJOrchestrator, AgentFactory, Planner
│   ├── storage/             # Neutral storage provider (AWS S3, Mock, StorageService, compensating saga)
│   └── worker/              # Worker runner, TaskExecutor interface, MockExecutor, Reaper, PlanAdapter
├── migrations/              # Version-controlled idempotent SQL migrations & rollbacks
│   ├── 000001_init_schema.up.sql / down.sql
│   ├── 000002_rls_policies.up.sql / down.sql
│   ├── 000003_orchestration_and_agent_factory.up.sql / down.sql
│   └── 000004_s3_storage_artifacts.up.sql / down.sql
├── tests/                   # Full integration test suite (Engine, RLS, API, Worker, Orchestration, S3 Live)
├── infra/                   # Automated AWS EC2 launch & cloud-init provisioning scripts
├── docs/
│   └── PRD.md               # The product: what is built, what each ask's status is, what is next
├── decisions.md             # Canonical record of all decisions made & planned
├── evolution.md             # Chronicle of product pivots and lessons learned

├── agents.md                # This operating manual
├── Makefile                 # Developer build, test, run, and migration targets
├── README.md                # Quick start, API reference, and curl examples
└── .env.example             # Configuration template
```

---

## 6. Standard Developer & Agent Commands

All commands are runnable via [`Makefile`](Makefile):

```bash
# Build server binary into bin/griffin-server
make build

# First run: .env files, local database and migrations in one command
make setup

# Run server locally (default port 8080)
make run

# Run full test suite with verbose output
make test-v

# Run a single targeted test package
go test -v ./tests/ -run TestConcurrentWorkerClaims

# Apply database migrations to local griffin_dev
make migrate

# Rollback migrations on local griffin_dev
make migrate-down
```

---

## 7. Rules of Engagement for AI Agents

1. **Test-Driven Development (TDD) at Seams**:
   - Write failing tests first before modifying business logic.
   - Test against agreed architectural seams: domain models, repository transactions, task engine coordination, and HTTP handlers.
2. **Never Commit Secrets or Personal Credentials**:
   - Do not commit `.env` files, Supabase service keys, personal Google OAuth tokens, or AWS access keys.
3. **No Unapproved Cloud Resource Creation**:
   - Never execute `aws ec2 run-instances` or create AWS cloud resources without explicit user approval.
4. **Idempotence & Crash Resilience**:
   - Treat worker interruptions, network drops, and server restarts as normal operating conditions. Use idempotency keys, leases, and reconciliation loops.
5. **Keep Ledgers Current**:
   - Always update `decisions.md` when introducing new architectural rules or patterns.
   - Always update `evolution.md` when a pivot or implementation discovery occurs.
   - Keep `docs/PRD.md` (section 4, what is built) synchronized with actual test results.

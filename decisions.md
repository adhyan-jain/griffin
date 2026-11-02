# Griffin Architecture & Product Decision Log (decisions.md)

This document is the canonical ledger of all architectural, product, engineering, and infrastructural decisions made for **Griffin** — both what has been done and what is planned for future phases.

---

## 1. Decisions Made (Historical & Implemented)

### Foundation & Architecture

#### ADR-001: Go Modular Monolith over Microservices
- **Status**: IMPLEMENTED (Phase 1)
- **Decision**: Build Griffin as a single cohesive modular monolith in Go (using standard library `net/http`, `log/slog`, `context`), deployed to a single host.
- **Rationale**: Eliminates distributed systems network latency, serialization overhead, multi-repo synchronization, and Kubernetes/broker operational burden.
- **Location**: [`cmd/griffin-server/main.go`](cmd/griffin-server/main.go), [`internal/`](internal/)

#### ADR-002: PostgreSQL `SKIP LOCKED` Task Queue over Redis / Celery / RabbitMQ
- **Status**: IMPLEMENTED (Phase 1)
- **Decision**: Implement distributed task queue semantics directly in PostgreSQL 16/17 using `SELECT ... FOR UPDATE SKIP LOCKED`.
- **Rationale**: PostgreSQL is already our single source of truth for persistent state. `SKIP LOCKED` guarantees zero duplicate task claims across concurrent workers with sub-millisecond query latency and zero external broker infrastructure.
- **Location**: [`internal/repository/task_repo.go`](internal/repository/task_repo.go)

#### ADR-003: Row Level Security (RLS) & Tenant Isolation
- **Status**: IMPLEMENTED (Phase 1)
- **Decision**: Enable `FORCE ROW LEVEL SECURITY` across all tenant-owned tables (`tasks`, `approvals`, `agents`, `artifacts`, etc.) with `auth.uid() = user_id` policies.
- **Rationale**: Guarantees database-level tenant isolation even if an application-layer bug or rogue query occurs. Never trust frontend-supplied user IDs.
- **Location**: [`migrations/000002_rls_policies.up.sql`](migrations/000002_rls_policies.up.sql), [`tests/rls_test.go`](tests/rls_test.go)

#### ADR-004: Dual-Tier Authentication Strategy
- **Status**: IMPLEMENTED (Phase 1)
- **Decision**:
  1. **User Tier**: Authenticated via Supabase JWT Bearer tokens (HS256) verifying `sub` claim.
  2. **Worker Tier**: Protected via secret `X-Worker-Key` header / bearer token for background runners and cloud worker instances.
- **Rationale**: Decouples human user sessions from high-frequency headless background execution workers without exposing user JWTs on long-lived runner processes.
- **Location**: [`internal/api/middleware/auth.go`](internal/api/middleware/auth.go)

#### ADR-005: 11-State Unified Task Lifecycle & DAG Dependency Management
- **Status**: IMPLEMENTED (Phase 1)
- **Decision**: Standardize on 11 explicit lifecycle states: `CREATED`, `ROUTED`, `QUEUED`, `RUNNING`, `WAITING_FOR_DEPENDENCY`, `VERIFYING`, `AWAITING_APPROVAL`, `RETRYING`, `COMPLETED`, `FAILED`, `CANCELLED`.
- **Rationale**: Prevents race conditions in complex multi-step workflows. Tasks with uncompleted parents block in `WAITING_FOR_DEPENDENCY` and automatically transition to `QUEUED` when prerequisites finish.
- **Location**: [`internal/domain/task.go`](internal/domain/task.go), [`internal/service/task_engine.go`](internal/service/task_engine.go)

#### ADR-006: Distributed Leases, Worker Heartbeats, and Orphan Reaper
- **Status**: IMPLEMENTED (Phase 1)
- **Decision**: Running tasks have time-bounded leases (default 60s) renewed by worker heartbeats every 15s. A background reaper daemon inspects expired leases and transitions abandoned tasks to `RETRYING` or `FAILED`.
- **Rationale**: Guarantees automatic recovery from worker crashes, network disconnections, or VM reboots without human intervention or data loss.
- **Location**: [`internal/worker/worker.go`](internal/worker/worker.go), [`internal/worker/reaper.go`](internal/worker/reaper.go)

#### ADR-007: Provider-Neutral Worker Interface with Deterministic Mock Executor
- **Status**: IMPLEMENTED (Phase 1)
- **Decision**: Define a generic `TaskExecutor` interface. In Phase 1, use a deterministic `MockExecutor` for 100% reproducible testing of claims, failures, retries, and approvals before binding to Antigravity CLI.
- **Rationale**: Enables comprehensive local automated testing without requiring live model credentials, network roundtrips, or API billing.
- **Location**: [`internal/worker/executor.go`](internal/worker/executor.go), [`internal/worker/mock_executor.go`](internal/worker/mock_executor.go)

#### ADR-008: Idempotent Database Migrations & In-Process Runner
- **Status**: IMPLEMENTED (Phase 1)
- **Decision**: SQL migrations are version-controlled in `migrations/` and executed programmatically by both the server on startup and the test suite during setup.
- **Rationale**: Guarantees database schemas are always synchronized between local development, testing, and production without requiring manual `psql` invocations.
- **Location**: [`internal/database/migrations.go`](internal/database/migrations.go)

---

### Compute & Provider Strategy

#### ADR-009: Antigravity CLI (`agy`) as PRIMARY Cloud Execution Engine
- **Status**: VALIDATED & STAGED (Phase 0.5; Implementation in Phase 2)
- **Decision**: Run the authenticated **Google Antigravity CLI (`agy`)** on AWS EC2 as Griffin's primary AI reasoning engine under the user's active Antigravity subscription, rather than paying per-token API fees.
- **Rationale**: Eliminates per-token billing for JJ and background workers; provides high-intelligence reasoning (Gemini 2.5 Pro / Flash) under an existing personal subscription.
- **Location**: [`docs/experiments/EC2_AGY_POC.md`](docs/experiments/EC2_AGY_POC.md), [`docs/architecture/ANTIGRAVITY_CLOUD_RUNNER.md`](docs/architecture/ANTIGRAVITY_CLOUD_RUNNER.md)

#### ADR-010: Remote SSH OAuth Authentication Flow via AWS SSM
- **Status**: ARCHITECTED & DOCUMENTED (Phase 0.5)
- **Decision**: EC2 instance runs completely headlessly without X11. Authentication uses Antigravity's native Remote SSH OAuth flow via AWS SSM Session Manager (`aws ssm start-session`).
- **Rationale**: Standard supported Google OAuth loopback; user signs in on laptop browser once, pastes the authorization code into the remote terminal, and credentials persist in `/home/griffin/.gemini/antigravity-cli/`.
- **Location**: [`docs/experiments/EC2_AUTH_PERSISTENCE.md`](docs/experiments/EC2_AUTH_PERSISTENCE.md)

#### ADR-011: AWS EC2 Graviton2 (`ap-south-1` Mumbai) Hosting
- **Status**: STAGED (Phase 0.5)
- **Decision**: Deploy on AWS EC2 `ap-south-1` using Graviton2 ARM64 (`t4g.medium`, 2 vCPU, 4 GiB RAM, 30 GB gp3 KMS-encrypted). Downscale to `t4g.small` (2 GiB RAM) after promotional credits expire.
- **Rationale**: Capitalizes on remaining promotional credits ($19.25–$22.90/mo covered 100%, net cash $0.00); provides persistent 24/7 compute so laptop can remain offline.
- **Location**: [`docs/experiments/EC2_RESOURCE_BENCHMARK.md`](docs/experiments/EC2_RESOURCE_BENCHMARK.md), [`infra/provision-ec2.sh`](infra/provision-ec2.sh)

#### ADR-012: Zero Inbound Ports & AWS SSM Administration
- **Status**: IMPLEMENTED IN INFRA SCRIPTS (Phase 0.5)
- **Decision**: Inbound port 22 (SSH) is completely closed. All terminal access and management occur via AWS Systems Manager Session Manager using IAM role `GriffinEC2SSMRole`.
- **Rationale**: Eliminates public SSH attack surface, brute-force scans, and bastion host costs.
- **Location**: [`infra/provision-ec2.sh`](infra/provision-ec2.sh)

#### ADR-013: Bounded Subprocess Concurrency (Pool = 2) & Swap Safety
- **Status**: VALIDATED & CONFIGURED (Phase 0.5 & Phase 1)
- **Decision**: Enforce a strict maximum concurrency of 2 parallel `agy` subprocesses on EC2, coupled with an automated 2 GiB swapfile.
- **Rationale**: Empirical profiling showed each worker peaks at ~235 MB RSS. A 2-worker pool consumes ~470 MB, leaving >2.6 GiB (64%) free memory buffer on `t4g.medium` to prevent Linux OOM kills.
- **Location**: [`docs/experiments/EC2_RESOURCE_BENCHMARK.md`](docs/experiments/EC2_RESOURCE_BENCHMARK.md), [`infra/user-data.sh`](infra/user-data.sh)

#### ADR-014: Strict Prohibition of Silent Paid Fallback
- **Status**: ARCHITECTED (Phase 0.5)
- **Decision**: Tasks default to `allow_paid_fallback = false`. If Antigravity CLI credentials expire or Google flags the session, tasks pause in `BLOCKED_AWAITING_PROVIDER` and dispatch an emergency notification.
- **Rationale**: Prevents accidental bill shock from silent per-token API charges.
- **Location**: [`docs/architecture/PROVIDER_ROUTING_POLICY.md`](docs/architecture/PROVIDER_ROUTING_POLICY.md)

---

## 2. Decisions Planned (Future Phases)

### Phase 2: Antigravity Primary Execution Provider & Runner Adapter

#### ADR-015: Subprocess Isolation & Scratch Workspace Eviction
- **Status**: PLANNED (Phase 2)
- **Decision**: Each task executed by `agy` runs inside an isolated scratch workspace `/var/griffin/workspaces/task-<task_id>/` with permissions `0700`. Upon task completion or failure, the Go runner recursively deletes the directory.
- **Rationale**: Prevents filesystem collisions between concurrent workers and bounds disk consumption on the 30 GiB EBS volume.

#### ADR-016: Structured Output via Native `--json-schema`
- **Status**: PLANNED (Phase 2)
- **Decision**: Griffin task prompts pass JSON schemas via `--json-schema` directly to `agy`, enforcing that LLM responses deserialize into typed Go domain structs without regex parsing or text extraction heuristics.
- **Rationale**: Empirically validated in Phase 0.5; guarantees deterministic JSON formatting from Gemini models.

#### ADR-017: 6-Hour Background Canary Health Probe
- **Status**: PLANNED (Phase 2 / Phase 5)
- **Decision**: A lightweight systemd timer executes `agy -p "ping" --output-format json` every 6 hours. If token refresh fails, it flags `provider_health` and triggers user alerts before scheduled user tasks run.
- **Rationale**: Proactively detects OAuth session expirations before critical morning briefing missions.

---

### Phase 3: JJ Orchestrator & 11 Specialist Domain Managers

#### ADR-018: Two-Level Agent Hierarchy (Orchestrator -> Managers -> Workers)
- **Status**: PLANNED (Phase 3)
- **Decision**: Griffin separates concerns into:
  1. **Top-Level Orchestrator (JJ)**: Intent classification, decomposition, delegation, and user synthesis.
  2. **11 Specialist Managers**: Domain experts (Career, Research, Engineering, Networking, Finance, etc.).
  3. **Ephemeral Workers**: Stateless task executors executing discrete tools.
- **Rationale**: Prevents single-prompt context pollution; allows domain-specific prompting, schemas, and tooling.

#### ADR-019: Asynchronous Google Drive Inbox/Outbox for Claude Cowork
- **Status**: PLANNED (Phase 3)
- **Decision**: Claude Cowork integration operates asynchronously via a synchronized Google Drive folder (`/Griffin/Cowork/Inbox/` and `/Outbox/`).
- **Rationale**: Leverages Cowork's deep reasoning and document capabilities without requiring fragile UI automation or continuous desktop presence.

---

### Phase 4: Frontend PWA & Real-Time Streaming

#### ADR-020: Next.js 15 PWA Hosted on Vercel Edge over EC2 Self-Hosting
- **Status**: PLANNED (Phase 4)
- **Decision**: The user interface will be built with Next.js 15, Tailwind CSS, and Web Push as a Progressive Web App (PWA) deployed on Vercel Edge.
- **Rationale**: Preserves 100% of EC2 compute and memory for the Go backend and `agy` runners; provides global CDN caching and free edge hosting.

#### ADR-021: Server-Sent Events (SSE) for Real-Time Streaming over WebSockets
- **Status**: PLANNED (Phase 4)
- **Decision**: Use HTTP/2 Server-Sent Events (SSE) for streaming LLM tokens, progress bars, and approval requests from backend to frontend; use standard REST for client actions.
- **Rationale**: Simpler connection lifecycle, automatic reconnection, native browser support, and seamless operation through Cloudflare Tunnels without WebSocket connection state issues.

#### ADR-022: Cloudflare Tunnel (`cloudflared`) for Ingress
- **Status**: PLANNED (Phase 4 / Phase 5)
- **Decision**: Ingress from Vercel PWA to the EC2 backend will route through an outbound-only Cloudflare Tunnel daemon.
- **Rationale**: Eliminates the need for an AWS Public IPv4 address ($3.65/mo savings), eliminates open inbound firewall ports (ports 80 and 443 remain closed), and provides free SSL termination.

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

#### ADR-015: Subprocess Isolation & Scratch Workspace Eviction
- **Status**: IMPLEMENTED (Phase 2)
- **Decision**: Each task executed by `agy` runs inside an isolated scratch workspace `<WorkspaceRoot>/task-<task_id>-<exec_id>/` with permissions `0700`. Input payloads are serialized as `task_input.json` with permissions `0600`. Upon task completion, the runner recursively removes the directory unless `RETAIN_WORKSPACES_ON_FAILURE=true` is set.
- **Rationale**: Prevents filesystem collisions between concurrent workers, contains temporary files, and bounds disk consumption on persistent storage.
- **Location**: [`internal/provider/antigravity/workspace.go`](file:///home/adhyan/Desktop/griffin/internal/provider/antigravity/workspace.go)

#### ADR-016: Structured Output via Native `--json-schema`
- **Status**: IMPLEMENTED (Phase 2)
- **Decision**: Griffin task prompts pass JSON schemas via `--json-schema` directly to `agy`, enforcing that LLM responses deserialize into typed Go domain structs and structured output JSON without regex heuristics.
- **Rationale**: Empirically validated in Phase 0.5 and Phase 2; guarantees deterministic JSON formatting from Gemini models.
- **Location**: [`internal/provider/antigravity/executor.go`](file:///home/adhyan/Desktop/griffin/internal/provider/antigravity/executor.go), [`internal/provider/antigravity/parser.go`](file:///home/adhyan/Desktop/griffin/internal/provider/antigravity/parser.go)

#### ADR-023: Process Group Containment (`Setpgid`) and Two-Stage Termination
- **Status**: IMPLEMENTED (Phase 2)
- **Decision**: Subprocesses are spawned in separate process groups (`cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}`). On timeout or cancellation, `SIGTERM` is sent to `-pgid`, followed by a 5-second graceful wait, and `SIGKILL` to `-pgid` if processes remain alive.
- **Rationale**: Completely eliminates zombie processes and orphan child tool processes spawned by `agy`.
- **Location**: [`internal/provider/antigravity/executor.go`](file:///home/adhyan/Desktop/griffin/internal/provider/antigravity/executor.go)

#### ADR-024: Pure `argv` Slice Subprocess Execution without Shell Interpolation
- **Status**: IMPLEMENTED (Phase 2)
- **Decision**: Commands are launched via `exec.Command(binaryPath, args...)` using discrete string slices. Shell execution (`sh -c`, `bash -c`, or string interpolation) is strictly forbidden.
- **Rationale**: Neutralizes shell injection vulnerabilities from user-provided prompts, schemas, and parameters.
- **Location**: [`internal/provider/antigravity/executor.go`](file:///home/adhyan/Desktop/griffin/internal/provider/antigravity/executor.go)

#### ADR-025: Path Traversal Defense on Workspace Operations
- **Status**: IMPLEMENTED (Phase 2)
- **Decision**: All workspace directory creation and cleanup operations enforce `filepath.Clean` and verify that the target directory strictly begins with `rootDir + filepath.Separator`. Violations return `ErrWorkspacePathTraversal`.
- **Rationale**: Prevents malicious or corrupted task identifiers from modifying or deleting directories outside the designated scratchpad area.
- **Location**: [`internal/provider/antigravity/workspace.go`](file:///home/adhyan/Desktop/griffin/internal/provider/antigravity/workspace.go)

#### ADR-026: Multi-Turn Conversation Continuation via `--conversation <id>`
- **Status**: IMPLEMENTED (Phase 2)
- **Decision**: The executor extracts `conversation_id` from task input envelopes and forwards it via `--conversation <id>` to `agy`. Upon task completion, new or returned conversation IDs are captured and stored with the execution result.
- **Rationale**: Enables multi-step missions and interactive conversation threads across discrete tasks while preserving full context in Google Antigravity.
- **Location**: [`internal/provider/antigravity/executor.go`](file:///home/adhyan/Desktop/griffin/internal/provider/antigravity/executor.go)

#### ADR-027: Typed Authentication Failure Detection & Immediate Halt
- **Status**: IMPLEMENTED (Phase 2)
- **Decision**: Subprocess outputs indicating authentication expiry ("authentication required", "unauthenticated", "token expired") are parsed into `ErrAuthenticationRequired`. The worker disables retries (`retryable: false`), logs a critical operator alert, records an `AUTHENTICATION_REQUIRED` event in PostgreSQL `task_events`, and halts execution without silent paid fallback.
- **Rationale**: Enforces Griffin's zero-paid-fallback guarantee and prevents wasteful, endless retry loops on invalid credentials.
- **Location**: [`internal/provider/antigravity/parser.go`](file:///home/adhyan/Desktop/griffin/internal/provider/antigravity/parser.go), [`internal/worker/worker.go`](file:///home/adhyan/Desktop/griffin/internal/worker/worker.go)

#### ADR-028: Two-Tier Subprocess Testing Strategy
- **Status**: IMPLEMENTED (Phase 2)
- **Decision**: AI provider testing is divided into:
  1. Hermetic controlled subprocess mock tests using temporary scripts (sub-second, runs in CI without tokens).
  2. Opt-in live integration probe against installed `/usr/bin/agy` gated behind `GRIFFIN_ENABLE_LIVE_AGY_TEST=true`.
- **Rationale**: Guarantees reproducible, fast automated testing in development and CI environments while retaining an automated verification probe for live binary compatibility.
- **Location**: [`internal/provider/antigravity/executor_test.go`](file:///home/adhyan/Desktop/griffin/internal/provider/antigravity/executor_test.go), [`tests/worker_test.go`](file:///home/adhyan/Desktop/griffin/tests/worker_test.go)

---

### Scale-to-Zero & On-Demand Cloud Runner Architecture

#### ADR-029: On-Demand Compute via Scale-to-Zero Architecture
- **Status**: IMPLEMENTED (Scale-to-Zero)
- **Decision**: Pivot from an always-on 24/7 EC2 compute model to on-demand execution where the EC2 runner stops during idle periods and wakes up on runnable work. Ingress API stays online 24/7 independently on serverless compute. Supabase PostgreSQL remains the durable source of truth.
- **Rationale**: Slashes monthly EC2 compute costs by over 90% (from ~$16.35/mo to ~$1.34/mo for typical 2hr/day workloads) while preserving unattended execution, encrypted EBS state, and zero-paid-fallback guarantees.
- **Location**: [`docs/SCALE_TO_ZERO_ARCHITECTURE.md`](file:///home/adhyan/Desktop/griffin/docs/SCALE_TO_ZERO_ARCHITECTURE.md), [`internal/runner/`](file:///home/adhyan/Desktop/griffin/internal/runner/)

#### ADR-030: Asynchronous 202 Accepted Task Ingestion
- **Status**: IMPLEMENTED (Scale-to-Zero)
- **Decision**: When `SCALE_TO_ZERO_ENABLED=true` and tasks are submitted in `QUEUED` state, the API persists the task durably in PostgreSQL, triggers runner wake-up asynchronously, and immediately responds to the client with `202 Accepted` and `X-Runner-Status: WAKEUP_SIGNALED`.
- **Rationale**: Decouples client HTTP latency (<20ms response time) from cloud VM cold-boot latencies (30–45s boot time), preventing HTTP timeouts on edge and mobile clients.
- **Location**: [`internal/api/handlers/tasks.go`](file:///home/adhyan/Desktop/griffin/internal/api/handlers/tasks.go)

#### ADR-031: Atomic Pre-Shutdown Database Recheck to Neutralize Shutdown Races
- **Status**: IMPLEMENTED (Scale-to-Zero)
- **Decision**: When the idle shutdown timer expires, the worker pool pauses task claims and enters `DRAINING`. Once in-flight executions finish, the monitor performs an atomic database recheck (`CountRunnableTasks(ctx)`). If ANY runnable task arrived during draining, or if the recheck fails, shutdown is aborted, workers resume, and the monitor resets to `ACTIVE`.
- **Rationale**: Prevents task starvation and race conditions where tasks arriving milliseconds before machine shutdown would otherwise remain stranded until an external trigger.
- **Location**: [`internal/runner/shutdown.go`](file:///home/adhyan/Desktop/griffin/internal/runner/shutdown.go)

#### ADR-032: Debounced Wake-Up Controller with Stopping-State Retry Backoff
- **Status**: IMPLEMENTED (Scale-to-Zero)
- **Decision**: The wake-up controller enforces a 30-second cooldown window between AWS `ec2:StartInstances` API calls, protecting against request stampedes. If the EC2 instance is currently in `stopping` state (which returns `InvalidInstanceState` on immediate `StartInstances`), the controller enters a background retry loop polling with exponential backoff until the instance reaches `stopped`, then issues `StartInstances`.
- **Rationale**: AWS EC2 rejects start commands on instances that have not yet fully stopped. Debouncing and retries guarantee reliable runner startup without race crashes.
- **Location**: [`internal/runner/wakeup.go`](file:///home/adhyan/Desktop/griffin/internal/runner/wakeup.go)

#### ADR-033: Decoupled Multi-Mode Binary Entrypoint (`GRIFFIN_MODE=all|api|worker`)
- **Status**: IMPLEMENTED (Scale-to-Zero)
- **Decision**: The single `griffin-server` binary supports three runtime modes:
  1. `all`: Monolithic mode running both API server and background workers (default for local dev).
  2. `api`: Ingress-only mode running HTTP endpoints and wake-up controller (for serverless edge or container hosting).
  3. `worker`: Headless runner mode running worker pools, task execution, and idle shutdown monitor on EC2.
- **Rationale**: Allows deploying identical, battle-tested Go code across serverless ingress and stoppable EC2 compute without maintaining separate codebases.
- **Location**: [`cmd/griffin-server/main.go`](file:///home/adhyan/Desktop/griffin/cmd/griffin-server/main.go), [`internal/config/config.go`](file:///home/adhyan/Desktop/griffin/internal/config/config.go)

#### ADR-034: External Periodic Scheduler Tick via EventBridge
- **Status**: IMPLEMENTED (Scale-to-Zero)
- **Decision**: Griffin exposes an authenticated `POST /api/v1/scheduler/tick` endpoint protected by `X-Worker-Key`. AWS EventBridge Scheduler triggers this endpoint every minute to inspect PostgreSQL for due cron tasks and trigger runner wake-up when runnable work exists.
- **Rationale**: Offloads periodic scheduling from the stoppable EC2 VM to free-tier cloud serverless infrastructure, ensuring scheduled tasks (such as morning briefings) wake the runner right on time.
- **Location**: [`internal/api/handlers/scheduler.go`](file:///home/adhyan/Desktop/griffin/internal/api/handlers/scheduler.go), [`docs/SERVERLESS_API_DESIGN.md`](file:///home/adhyan/Desktop/griffin/docs/SERVERLESS_API_DESIGN.md)

### Phase 3: JJ Orchestrator, Manager Framework & Agent Factory

#### ADR-018: Two-Level Agent Hierarchy (Orchestrator -> 11 Managers -> Specialist Workers)
- **Status**: IMPLEMENTED (Phase 3)
- **Decision**: Griffin separates concerns into:
  1. **Top-Level Orchestrator (JJ)**: User intent classification, structured DAG plan generation, delegation, monitoring, deliverable aggregation, and final response synthesis.
  2. **11 Specialist Managers**: Domain experts (`career`, `research`, `engineering`, `hackathon`, `freelance`, `networking`, `tech_intelligence`, `learning`, `personal_ops`, `finance`, `product_ideas`) with verification criteria.
  3. **Specialist Workers**: Focused task executors instantiated with injected system prompts, schemas, tools, and capability boundaries.
- **Rationale**: Prevents monolithic context degradation and prompt bloat. Enables clean separation of concerns and strict capability boundaries.
- **Location**: [`internal/service/orchestrator.go`](file:///home/adhyan/Desktop/griffin/internal/service/orchestrator.go), [`internal/manager/manager.go`](file:///home/adhyan/Desktop/griffin/internal/manager/manager.go), [`docs/JJ_ORCHESTRATOR.md`](file:///home/adhyan/Desktop/griffin/docs/JJ_ORCHESTRATOR.md)

#### ADR-035: Kahn's Algorithm DAG Plan Validation & Cyclic Dependency Defense
- **Status**: IMPLEMENTED (Phase 3)
- **Decision**: All plans generated by JJ (whether via AI or heuristic fallback) must be validated before execution using Kahn's topological sort algorithm to detect cycles and enforce strict DAG linearity.
- **Rationale**: Prevents deadlock in the task engine caused by circular dependency graphs generated by LLMs.
- **Location**: [`internal/domain/orchestration.go`](file:///home/adhyan/Desktop/griffin/internal/domain/orchestration.go), [`internal/domain/orchestration_test.go`](file:///home/adhyan/Desktop/griffin/internal/domain/orchestration_test.go)

#### ADR-036: Reusable Specialist Worker Definitions with Versioned Immutability & Audit Trail
- **Status**: IMPLEMENTED (Phase 3)
- **Decision**: Specialist workers are defined via `WorkerDefinition` models with strict immutability. Updating a worker definition never mutates existing database rows; it creates a new row with an incremented version number (`version + 1`). All mutations record audit records in `agent_factory_audits`.
- **Rationale**: Ensures complete auditability and guarantees that historical task executions maintain immutable references to the exact worker definition and prompts that executed them.
- **Location**: [`internal/domain/worker_definition.go`](file:///home/adhyan/Desktop/griffin/internal/domain/worker_definition.go), [`internal/service/agent_factory.go`](file:///home/adhyan/Desktop/griffin/internal/service/agent_factory.go), [`docs/AGENT_FACTORY.md`](file:///home/adhyan/Desktop/griffin/docs/AGENT_FACTORY.md)

#### ADR-037: Centralized Capability Taxonomy & Automated Approval Checkpoints
- **Status**: IMPLEMENTED (Phase 3)
- **Decision**: Define a centralized taxonomy of 11 explicit capabilities (`research:read`, `repo:read`, `repo:write`, `web:read`, `email:draft`, `email:send`, `calendar:read`, `calendar:write`, `financial:action`, `job:apply`, `infra:destructive`). Sensitive capabilities (`repo:write`, `email:send`, `financial:action`, `job:apply`, `infra:destructive`) automatically require human approval (`domain.RequiresApproval`). Tasks requesting sensitive actions pause in `AWAITING_APPROVAL` until signed off by the user.
- **Rationale**: Enforces zero-trust capability security and prevents autonomous AI models from taking destructive or irreversible real-world actions without human oversight.
- **Location**: [`internal/domain/capability.go`](file:///home/adhyan/Desktop/griffin/internal/domain/capability.go), [`docs/CAPABILITY_AND_APPROVAL_POLICIES.md`](file:///home/adhyan/Desktop/griffin/docs/CAPABILITY_AND_APPROVAL_POLICIES.md)

#### ADR-038: JJ Orchestration Lifecycle (REQUEST -> PLAN -> VALIDATE -> DELEGATE -> MONITOR -> COLLECT -> VERIFY -> RESPOND) & Restart Recovery
- **Status**: IMPLEMENTED (Phase 3)
- **Decision**: JJ manages orchestration across 8 explicit phases. Root tasks represent overall user intent (`manager_domain = 'JJ'`), while child tasks represent delegated work. JJ recovers active in-flight orchestrations on system startup via `ReconcileInFlight`.
- **Rationale**: Guarantees zero lost work, seamless crash recovery across server restarts, and non-blocking monitoring of worker leases.
- **Location**: [`internal/service/orchestrator.go`](file:///home/adhyan/Desktop/griffin/internal/service/orchestrator.go), [`docs/JJ_ORCHESTRATOR.md`](file:///home/adhyan/Desktop/griffin/docs/JJ_ORCHESTRATOR.md)

#### ADR-039: Manager Deliverable Verification Contract & Domain Governance
- **Status**: IMPLEMENTED (Phase 3)
- **Decision**: Each manager implements `VerifyDeliverable` to validate child task outputs before JJ synthesizes final responses. The manager registry enforces governance, rejecting any attempt to register unauthorized or hallucinated manager roles.
- **Rationale**: Prevents hallucinated agent personas and ensures output deliverables meet domain-specific completeness and quality standards.
- **Location**: [`internal/manager/manager.go`](file:///home/adhyan/Desktop/griffin/internal/manager/manager.go), [`internal/manager/registry.go`](file:///home/adhyan/Desktop/griffin/internal/manager/registry.go), [`docs/MANAGER_FRAMEWORK.md`](file:///home/adhyan/Desktop/griffin/docs/MANAGER_FRAMEWORK.md)

---

## 2. Decisions Planned (Future Phases)

### Phase 2 Operations & Background Maintenance

#### ADR-017: 6-Hour Background Canary Health Probe
- **Status**: PLANNED (Phase 2 Operations / Phase 5)
- **Decision**: A lightweight systemd timer executes `agy -p "ping" --output-format json` every 6 hours. If token refresh fails, it flags `provider_health` and triggers user alerts before scheduled user tasks run.
- **Rationale**: Proactively detects OAuth session expirations before critical morning briefing missions.

---

### Phase 3 Integrations

#### ADR-019: Asynchronous Google Drive Inbox/Outbox for Claude Cowork
- **Status**: SUPERSEDED & ABANDONED (Phase 4)
- **Decision**: Replaced by direct AWS S3 Object Storage Integration (ADR-040). Google Drive is explicitly not part of Griffin's architecture.
- **Rationale**: Direct S3 API integration provides deterministic programmatic access, IAM role authentication, presigned secure URLs, and removes fragile sync clients.

---

### Phase 4: AWS S3 Object Storage Integration

#### ADR-040: AWS S3 as Griffin's Primary Object Storage Backend
- **Status**: IMPLEMENTED (Phase 4)
- **Decision**: AWS S3 is designated as Griffin's primary object and file storage backend. Operational metadata, ownership, checksums, and tenant isolation policies remain stored in Supabase PostgreSQL, while file payloads (reports, generated documents, logs, diffs, images, and data exports) reside in private S3 buckets.
- **Rationale**: Eliminates local disk coupling, provides unlimited durable storage, supports automatic multipart streaming, and leverages EC2 IAM instance profiles (`GriffinEC2WorkerRole`) for zero-secret cloud security.
- **Location**: [`internal/storage/`](file:///home/adhyan/Desktop/griffin/internal/storage/), [`docs/S3_STORAGE_CONFIGURATION.md`](file:///home/adhyan/Desktop/griffin/docs/S3_STORAGE_CONFIGURATION.md), [`docs/STORAGE_ARCHITECTURE.md`](file:///home/adhyan/Desktop/griffin/docs/STORAGE_ARCHITECTURE.md)

#### ADR-041: Task-Scoped Object Hierarchy & Strict Private Access via Presigned URLs
- **Status**: IMPLEMENTED (Phase 4)
- **Decision**: All objects are stored under predictable hierarchical paths: `tasks/{task_id}/outputs/{sanitized_filename}` (or `artifacts/{uuid}/{sanitized_filename}`). S3 buckets block all public access. Client downloads are authenticated via short-lived presigned GET URLs (default 15 minutes) or authenticated streaming endpoints.
- **Rationale**: Guarantees strict multi-tenant isolation, protects sensitive agent outputs, prevents directory traversal attacks, and offloads heavy file download bandwidth directly to S3.
- **Location**: [`internal/storage/service.go`](file:///home/adhyan/Desktop/griffin/internal/storage/service.go), [`internal/api/handlers/artifacts.go`](file:///home/adhyan/Desktop/griffin/internal/api/handlers/artifacts.go)

#### ADR-042: Compensating Saga Pattern for S3 and PostgreSQL Consistency
- **Status**: IMPLEMENTED (Phase 4)
- **Decision**: To handle partial failures across AWS S3 and Supabase PostgreSQL without false distributed transaction claims, Griffin uses a Compensating Saga Pattern: payloads are uploaded to S3 first; if the PostgreSQL metadata insert fails, `StorageService` immediately executes a compensating `DeleteObject` against S3 using a detached background context.
- **Rationale**: Eliminates orphaned object accumulation in S3 during database transient errors or constraint failures while keeping both systems decoupled.
- **Location**: [`internal/storage/service.go`](file:///home/adhyan/Desktop/griffin/internal/storage/service.go), [`tests/storage_test.go`](file:///home/adhyan/Desktop/griffin/tests/storage_test.go)

---

### Phase 4: Frontend PWA & Real-Time Streaming

#### ADR-020: Next.js 15 PWA Hosted on Vercel Edge over EC2 Self-Hosting
- **Status**: IMPLEMENTED (Final Sprint)
- **Decision**: The user interface is built with Next.js 15, Tailwind CSS, TypeScript, and PWA capabilities located in `web/`.
- **Rationale**: Preserves 100% of EC2 compute and memory for the Go backend and `agy` runners; provides global CDN caching and free edge hosting.

#### ADR-021: Server-Sent Events (SSE) for Real-Time Streaming over WebSockets
- **Status**: PLANNED (Phase 5)
- **Decision**: Use HTTP/2 Server-Sent Events (SSE) for streaming LLM tokens, progress bars, and approval requests from backend to frontend; use standard REST for client actions.
- **Rationale**: Simpler connection lifecycle, automatic reconnection, native browser support, and seamless operation through Cloudflare Tunnels without WebSocket connection state issues.

#### ADR-022: Cloudflare Tunnel (`cloudflared`) for Ingress
- **Status**: PLANNED (Phase 5)
- **Decision**: Ingress from Vercel PWA to the EC2 backend will route through an outbound-only Cloudflare Tunnel daemon.
- **Rationale**: Eliminates the need for an AWS Public IPv4 address ($3.65/mo savings), eliminates open inbound firewall ports (ports 80 and 443 remain closed), and provides free SSL termination.

#### ADR-043: Next.js 15 TypeScript PWA Frontend Console Architecture
- **Status**: IMPLEMENTED (Final Sprint)
- **Decision**: Build a 10-screen responsive PWA in `web/` featuring Executive Dashboard, JJ Orchestrator Chat, Task DAG Hierarchy, Task Details & Attempts Inspector, Approvals Inbox, S3 Artifacts Browser, Workforce Specialist Directory, System Diagnostics, Operator Login, and Responsive Mobile Drawer/Navbar.
- **Rationale**: Provides operators with real-time operational control, manual approvals, and live DAG observability across desktop and mobile devices.
- **Location**: [`web/src/`](file:///home/adhyan/Desktop/griffin/web/src/)

#### ADR-044: Cross-Origin Resource Sharing (CORS) and Web Client Authentication
- **Status**: IMPLEMENTED (Final Sprint)
- **Decision**: Wrap the Go API ingress in a CORS middleware supporting preflight `OPTIONS` and standard authorization headers. Provide `/api/v1/auth/login` to issue signed Supabase JWTs with `OWNER` privileges and automatic foreign-key user synchronization.
- **Rationale**: Enables frictionless local and production web client integration without cross-origin blocking or manual token manipulation.
- **Location**: [`internal/api/middleware/cors.go`](file:///home/adhyan/Desktop/griffin/internal/api/middleware/cors.go), [`internal/api/handlers/auth.go`](file:///home/adhyan/Desktop/griffin/internal/api/handlers/auth.go)

#### ADR-045: Live AWS S3 Integration Testing and Integrity Verification
- **Status**: IMPLEMENTED & LIVE VERIFIED (Final Sprint)
- **Decision**: Implement opt-in end-to-end integration test (`tests/s3_live_test.go`) validating `Upload`, `Download`, `Head`, `PresignDownloadURL`, and `Delete` against real AWS S3 bucket `griffin-artifacts-123456789012-ap-south-1` using `griffin-bootstrap` credentials in `ap-south-1`.
- **Rationale**: Guarantees zero regression and genuine live cloud compatibility before production release.
- **Location**: [`tests/s3_live_test.go`](file:///home/adhyan/Desktop/griffin/tests/s3_live_test.go)

#### ADR-046: Cloud Runner Unattended Authentication and Lingering Session Pattern
- **Status**: IMPLEMENTED (Final Sprint)
- **Decision**: Configure EC2 cloud runners with systemd user lingering (`loginctl enable-linger griffin`) and persistent DBus session daemons to allow headless Antigravity CLI execution without interactive OAuth browser prompts.
- **Rationale**: Essential for scale-to-zero compute where workers start on demand and execute background AI workflows without active SSH/SSM sessions.
- **Location**: [`infra/user-data.sh`](file:///home/adhyan/Desktop/griffin/infra/user-data.sh), [`docs/ANTIGRAVITY_CLOUD_RUNNER.md`](file:///home/adhyan/Desktop/griffin/docs/ANTIGRAVITY_CLOUD_RUNNER.md)


### JJ-not-responding incident (2026-11) — decisions

#### ADR-047: Chat turns are tasks, with no schema migration
- **Status**: IMPLEMENTED (not yet deployed)
- **Decision**: `POST /api/v1/jj/messages` stores a conversational turn as a task (`manager_domain "JJ"`, `input_payload {kind:"chat", conversation_id, history}`). Its output is `{response, provider{name,version,model}}`. Only the last 10 turns (2000 chars each) are sent as history.
- **Rationale**: Chat reuses the existing lease, retry, RLS and reaper machinery and needs no migration. Before this there was no chat path, so every message became a repo-diagnosis task.
- **Trade-off**: The thread is not restored after a refresh (only `conversation_id` is kept in sessionStorage).
- **Location**: `internal/api/handlers/jj_chat.go`, `internal/worker/worker.go`, `docs/api/jj-messages.md`

#### ADR-048: Rule-based message classification (conversation vs objective)
- **Status**: IMPLEMENTED
- **Decision**: `service.ClassifyMessage` is a deterministic rule-based classifier. Conversation goes to a chat task; an objective goes through the orchestrator planner.
- **Rationale**: No model call is needed to decide routing, so routing works when the provider is down. `AIPlanner` is still not wired in.

#### ADR-049: Empty or tool-denied agy output is a failure, never a success
- **Status**: IMPLEMENTED
- **Decision**: agy exiting 0 with an empty response, or with `denied_actions`, becomes `ErrEmptyResult` or `ErrToolPermissionDenied` (retryable). The worker also fails any task whose output is empty, and fails a chat task whose response text is blank. The orchestrator's final response carries real step text and the task goes FAILED if a step produced nothing.
- **Rationale**: Root cause of the incident. A denied `RunCommand` produced exit 0, SUCCESS and an empty response, which the worker accepted as a completed task.
- **Rejected**: `--dangerously-skip-permissions` and allow-rules. The runner stays text-only with no tool approval, for security.

#### ADR-050: Runner wake failures are surfaced, not swallowed
- **Status**: IMPLEMENTED
- **Decision**: The wake outcome (signaled, skipped, failed, disabled) is returned with the response. `ErrInsufficientCapacity` yields a clear message ("AWS has no capacity for the worker instance right now; it will retry"). Wake is bounded to 8s.
- **Rationale**: t4g.medium in ap-south-1b repeatedly hit InsufficientInstanceCapacity and the user saw nothing. Changing the instance type or AZ is infrastructure and needs approval, so it was not done.

#### ADR-051: agy version drift is logged, not pinned
- **Status**: IMPLEMENTED (logging only)
- **Decision**: The executor logs the agy version, exit code and denied actions, and caps logged stderr. Auto-update (1.4.12 → 1.5.0 → 1.5.1) is not disabled.
- **Open**: Pinning needs an infra change.

#### ADR-052: Frontend polling is bounded and deduplicated
- **Status**: IMPLEMENTED & DEPLOYED (request-storm fix)
- **Decision**: All polling goes through `lib/poll.ts` and `use-polling.ts`. Identical in-flight GETs are deduplicated with ref-counted aborts. Polling backs off, stops on terminal states and pauses while the tab is hidden. `scripts/measure-polling` counts requests in a real browser.
- **Rationale**: A redesign bug caused about 1,674 requests per scenario; the fix brought it to 52.

#### ADR-053: Canonical-origin redirect and exact-origin CORS
- **Status**: IMPLEMENTED & DEPLOYED
- **Decision**: CORS allows only `https://griffin-pwa.vercel.app`. In production, non-canonical Vercel hosts redirect to it.

#### ADR-054: Backend deploys go through one dry-run-by-default script
- **Status**: IMPLEMENTED (script uncommitted until this entry), not run with `--apply`
- **Decision**: `infra/ops/deploy-backend.sh lambda|worker|all|rollback-worker [--apply]` refuses a dirty tree, smoke-tests the API after a Lambda deploy, and checks the sha256 of a worker binary, keeps a `.prev` copy and auto-restores on a failed health check.
- **Constraint**: It creates no infrastructure and never changes IAM or launches new instances. The Lambda call set (`GetFunctionConfiguration`, `UpdateFunctionCode --publish`) is already covered by policy `D`. The worker path depends on policy `A` (EC2/SSM/S3).
- **Rejected**: Widening IAM with `ListVersionsByFunction`, `ListAliases`, `GetAlias`, `GetPolicy` or `ListTags`. The script never calls them.

#### ADR-055: Task delete, resume and run-again
- **Status**: IMPLEMENTED (not yet deployed)
- **Decision**: `DELETE /api/v1/tasks/{id}` removes a task and its whole subtree in one statement, and only when every task in the tree is terminal (otherwise 409, cancel first). `POST /api/v1/tasks/{id}/resume` re-queues a FAILED or CANCELLED standalone task with a fresh retry budget (back to WAITING_FOR_DEPENDENCY if a prerequisite is unfinished) and wakes the runner. The UI offers "Run again" for plans and completed tasks by submitting the same request anew.
- **Rationale**: Subtasks use `parent_id ... ON DELETE SET NULL`, so deleting a parent alone would leave orphans. Re-opening a finished JJ plan is unsafe because `ReconcileOrchestration` treats a terminal orchestration as final, and resuming one subtask would leave its root FAILED. Resume is therefore refused for plan roots and subtasks (409); run again covers them.
- **Limits**: S3 objects of deleted tasks are not removed (artifact rows cascade, the files stay in storage). No migration was needed: `griffin_app` already has DELETE and the RLS policy is FOR ALL on the owner.
- **UI**: the Done tab has a finished-within window (today, week, month, 2 and 3 months, all time) and the list has a selection mode for bulk delete and resume. Task tabs no longer scroll: the base `TabsList` height is set per group variant, so `h-auto` alone never applied and the taller tabs overflowed.
- **Location**: `internal/repository/task_repo.go`, `internal/api/handlers/tasks.go`, `tests/task_lifecycle_test.go`, `web/src/lib/task-actions.ts`, `web/scripts/ui-tasks.mjs`

#### ADR-056: Immersive 3D layer ("the Aerie") with an adaptive quality switch
- **Status**: IMPLEMENTED (not yet deployed)
- **Decision**: One fixed WebGL canvas behind every screen (domain-warped nebula shader plus depth-layered dust) reacts to how many tasks run; the dashboard hero is an interactive JJ core (displaced, fresnel-lit brass sphere, one satellite per task, drag with inertia, hover for the title, tap to open the task); cards become frosted glass with a pointer-following highlight and a light tilt on desktop. Settings has Full / Lite / Off, remembered per device (`griffin_fx`); the default adapts to phones, low memory or core count and data saver.
- **Rationale**: A decorative asset in a card did not change how the product feels. One coherent system (field, core, glass) does, while the caps below keep it cheap: frame rate capped (30 fps in Lite), rendering paused while the tab is hidden, a single static frame under reduced motion (read live, not once), DPR capped, canvas fails closed (an error boundary renders nothing).
- **Gotcha found by testing**: R3F copies a `uniforms` prop, so mutating the original object never reached the GPU (the field ignored the theme and did not animate). Live values are written through material refs. File-level `react-hooks/immutability` disables on the two three.js files are deliberate for the same reason (per-frame writes are outside React state).
- **Not measured**: real-GPU and phone frame rates. Verification used software WebGL (SwiftShader) screenshots at 390 and 1280 px in both themes, with Off and Lite paths; no console errors.
- **Location**: `web/src/lib/fx.ts`, `web/src/components/fx/`, `web/src/components/hero-3d.tsx`

#### ADR-057: Identical running requests are refused, on the server
- **Status**: IMPLEMENTED (not yet deployed)
- **Decision**: `JJOrchestrator.SubmitRequest` returns a 409 with a plain message when the same user already has an active orchestration with exactly the same request text, and reserves each (user, text) pair in memory while it is being planned so simultaneous submits cannot both pass the check. Finished and cancelled runs do not count, so Run again keeps working. The UI also ignores a second tap while the first submit is pending.
- **Why**: A double tap on Run again queued the same paid plan several times (six running plans from two requests, 2026-11-06). Every plan costs agent time on a worker that runs one task at a time.
- **Limit**: The in-memory reservation covers one process; across Lambda instances only the database check applies, which leaves a very small window. A partial unique index would close it but needs a migration, which is held for approval.
- **Location**: `internal/service/orchestrator.go`, `internal/repository/orchestration_repo.go`, `tests/orchestration_test.go`

#### ADR-058: Plans finish when the task list is read, because the deployed worker is stale
- **Status**: IMPLEMENTED (not yet deployed); the worker replacement is still required
- **Finding**: Six plans stayed Running for 45+ minutes with every subtask completed. The EC2 worker binary dates from 2 Nov 17:00 UTC; the hook that finalizes a plan when its last subtask finishes (`cc1aa91`) landed on 4 Nov, and the old build's idle check counts a running plan root as work, so the runner also never reached its idle shutdown (it stayed up for 46+ minutes, all subtasks done).
- **Decision**: `GET /api/v1/tasks` reconciles the caller's active plans (throttled to once per 10 s per process, idempotent, bounded to 3 s, never fails the read). A plan whose subtasks are all done is finished whenever the user opens Tasks or Home, whichever worker is running.
- **Not a substitute**: Replacing the worker (`infra/ops/deploy-backend.sh worker`) is still the real fix, because only the new build stops counting plan roots as work and can reach its idle shutdown. A service restart finishes stuck plans on the old build too (it reconciles in-flight plans on startup). Both were left to the operator: the automation permission layer blocked remote writes and production deploys.
- **Location**: `internal/service/orchestrator.go` (`ReconcileActiveForUser`), `internal/api/handlers/tasks.go`, `tests/orchestration_test.go`

#### ADR-059: Steps are routed to a provider by what they need; Claude is wired in but off
- **Status**: IMPLEMENTED (not yet deployed); Claude disabled by default
- **Decision**: Each plan step carries `provider_hint` (`agy` or `claude`) inside `input_payload` (no schema change), chosen from the step text by capability, not difficulty: anything that touches the outside world (browse, log in, files, apps, commands) stays on agy; clearly language-only work (draft, summarise, compare, review) is planned for Claude; unclear defaults to agy; chat is always agy. A router in front of the worker honours the hint and runs the step on agy when Claude is disabled, unhealthy or not signed in. Claude runs through the signed-in `claude` CLI only, with every `ANTHROPIC_*` and cloud-provider variable stripped from the child process, so it can never bill an API key. `CLAUDE_ENABLED=true` and a signed-in CLI on the worker are both required.
- **Constraint kept**: no paid API fallback. Claude is not a production provider until the operator signs in and enables it.
- **Open**: agy's prompt currently says it has no tools (a deliberate earlier fix for hangs and fake completions), so both providers are text-only. Tool use (browse, apply, message) needs its own approval-gated design.
- **Location**: `internal/domain/providerhint.go`, `internal/provider/router/`, `internal/provider/claudecli/`, `cmd/griffin-server/main.go`

#### ADR-060: Finished answers are kept as files; living lists
- **Status**: IMPLEMENTED (not yet deployed)
- **Decision**: The worker saves every finished task's answer through the existing storage service as a Markdown (or JSON) artifact linked to the task; a failed save is logged and never fails the task. A living list is a Markdown artifact with `metadata.kind = "list"`: `POST /api/v1/lists` creates one and `PUT /api/v1/artifacts/{id}/text` replaces or appends to it (owner only, text files only, bounded by the upload limit, rewritten in place so the link is stable). The UI has a Lists page and reader, and every task result has Save to list (a dated section is appended).
- **Why**: Some work is a growing record (an internship tracker), not a one-off answer. Using the artifact store needs no migration and no new infrastructure.
- **Limit**: Today the list is grown by you (or by Save to list). A task cannot append on its own yet; that, and remembered playbooks, depend on agy getting tools.
- **Location**: `internal/worker/worker.go`, `internal/storage/service.go`, `internal/api/handlers/artifacts.go`, `web/src/app/lists/`, `web/src/components/result-view.tsx`

#### ADR-061: Motion and display system; results read as documents
- **Status**: IMPLEMENTED and DEPLOYED for the motion layer (`ca3e790`); the rest not yet deployed
- **Decision**: GSAP (SplitText, ScrollTrigger) and Lenis give one easing vocabulary: kinetic headlines, count-up numerals, a route entrance, magnetic buttons, a trailing cursor ring (mouse only), inertial scroll (mouse only) and a once-per-session boot curtain. Syne is the display face. The hero is a full-bleed scene with type laid over it; the JJ core is dark glass with molten brass veins and running tasks glow while finished ones fade to dust. Results render as formatted Markdown (reader card with copy, download, raw and fold) instead of JSON.
- **Safeguards**: everything respects reduced motion and the Full/Lite/Off switch; touch devices get none of the pointer effects; automation skips the boot curtain. The reveal mask is removed after the motion ends because it clipped descenders on tight lines (found by the real-stack run).
- **Not verified**: frame rate on a real GPU or phone; screenshots are software-rendered.
- **Location**: `web/src/lib/motion.ts`, `web/src/components/fx/`, `web/src/components/result-view.tsx`

#### ADR-062: A real-stack end-to-end check, and request caching by choice
- **Status**: IMPLEMENTED
- **Decision**: `web/scripts/e2e-real.mjs` drives the PWA in a real browser against the real Go server, a real Postgres and the real worker loop (mock AI and storage providers; development mode, because production refuses mock providers). 21 checks cover sign-in, task creation and completion, the result reader, saved files, duplicate prevention under a tap and a simultaneous burst, filters, bulk delete and the Lists flow. The new-task dialog shows a notice when the same goal finished in the last 24 hours, with a link to that result, and still lets the user run it again.
- **Why a notice, not silent reuse**: some requests are time-sensitive (live openings, prices); reusing a stale answer without asking would be worse than paying for a rerun.
- **Flake recorded**: the Go integration suite shares one test database and truncates it in setup with a 5 s deadline; under CPU contention (a dev server and browser running) that times out. It passes on a quiet machine. Tests that start a worker now wait for it to stop before teardown.
- **Location**: `web/scripts/e2e-real.mjs`, `web/src/lib/task-actions.ts` (`findRecentResult`), `web/src/components/new-task-dialog.tsx`

#### ADR-063: agy tool tiers behind an approval gate (off by default)
- **Status**: IMPLEMENTED and tested; **disabled** (`AGY_TOOLS_ENABLED=false`); not deployed. Chosen by the operator: "Yes, with approvals".
- **Decision**: A step carries `tool_mode` in `input_payload`: `none` (today: text only), `read` (read-only research) or `act` (may act for the user). The planner suggests `read` for work that touches the outside world and never suggests `act`; `act` is an explicit choice. Chat is always `none`, and an unrecognised value is `none`, so a bad payload can only make a task less capable.
- **Gate**: Before the provider runs, the worker checks whether the owner approved this exact task for `tool_act`. If not, it creates a HIGH-risk approval ("Allow Griffin to act for you on: <title>"), pauses the task (AWAITING_APPROVAL) and does not call the provider; approval releases it, rejection cancels it. If the approval check itself fails, the task does not run. With tools disabled there is nothing to approve and the task runs text-only as before.
- **Controls used**: agy 1.5.2 offers `--mode plan|accept-edits|auto`, `--sandbox`, `--effort`, and `--output-format stream-json`. Per-tier flags come from `AGY_READ_ARGS` (default `--mode plan --sandbox`) and `AGY_ACT_ARGS` (default `--mode accept-edits --sandbox`). `--dangerously-skip-permissions`, any print/prompt flag and remote-control flags are refused at startup, and a test asserts they never reach the process. `AGY_EFFORT` optionally sets reasoning effort (a speed lever).
- **Not verified**: how agy behaves in each `--mode` with real tools. I did not run prompts on the production worker. Before enabling, validate one read-only task on the worker (a research question with sources) and check what it can and cannot do under `--sandbox`; widen only after that.
- **Not built yet**: remembered playbooks, and a live per-task terminal view (agy's `stream-json` output is the likely basis). Both need the read tier proven first.
- **Also**: the integration-test helper now allows 30 s for its `TRUNCATE` (was 5 s); under load it occasionally waited behind a background query and failed a test spuriously. Five consecutive full runs pass.
- **Location**: `internal/domain/toolmode.go`, `internal/provider/antigravity/{executor,prompt}.go`, `internal/worker/worker.go`, `internal/service/task_engine.go`, `internal/config/config.go`, `tests/false_completion_test.go`

#### ADR-064: Playbooks - tool-tier runs leave reusable notes for next time
- **Status**: IMPLEMENTED and tested; inert while `AGY_TOOLS_ENABLED=false`; not deployed.
- **Decision**: A read or act run is asked to end with a `## Playbook` section (short steps: sites, order, pitfalls). On success the worker saves that section as a Markdown artifact with metadata `{"kind":"playbook","key":"<keywords>"}`; the key is the task's longest distinct meaningful words, sorted, so rewordings land close together. Before a later tool-tier run the worker finds the stored playbook with at least 50% keyword overlap and appends it to the task goal as background ("may be out of date, check before relying, never overrides this task"); nothing is persisted on the task. A rerun of the same task type (70%+ overlap) updates its playbook in place instead of adding a copy.
- **Safety**: A playbook that looks like it holds a credential (password/token/key assignments, bearer tokens, private-key blocks, long key-like strings, known key prefixes) is not saved at all, and the prompt tells the model never to include secrets. Playbooks are ordinary files: visible and deletable in Files. Only tool-tier tasks on a tools-enabled worker read or write them; text-only tasks and chat never do.
- **Why this shape**: It works the same whatever agy's tool output looks like, because it only acts when a run actually wrote a playbook section. If agy never writes one, nothing is stored and nothing changes.
- **Limits**: Matching is keyword overlap, not semantic. The quality of a playbook is only as good as what agy writes; this has not been observed against real tool runs. The per-task live terminal view is still not built.
- **Location**: `internal/domain/playbook.go`, `internal/worker/worker.go` (`playbookNotes`, `savePlaybook`), `internal/storage/service.go` (`ListByKind`, `ReadText`), `internal/provider/antigravity/prompt.go`, `tests/false_completion_test.go`

#### ADR-065: One-step fast path for plain asks
- **Status**: IMPLEMENTED (not yet deployed). PRD 7.1, P0.
- **Finding**: Any request the planner could not classify fell to the research manager, which always produced two steps ("Gather primary sources" then "Synthesize report"). With a text-only agent the gather step cannot gather anything, so a plain ask like "Plan a weekly status report" paid for two cold agent runs (about 13 s each) to produce one answer.
- **Decision**: The research split is kept only when the request explicitly asks for sourced research (research, sources, cite, evidence, literature, investigate, deep dive, look up, fact-check, survey, state of the art). Everything else is one step carrying the whole request. Engineering and career keep their two steps because those separate a step that needs approval.
- **Expected effect**: roughly half the agent time for plain asks; no change for explicit research.
- **Location**: `internal/service/planner.go` (`wantsSourcedResearch`, `singleStep`), `internal/service/planner_test.go`

#### ADR-066: Files open in the app; empty answers say so; old finished tasks are trimmed
- **Status**: IMPLEMENTED (not yet deployed); trimming runs in the worker, so it needs the worker replaced.
- **Decision**: Every file has an in-app page (`/artifacts/[id]`): Markdown formatted, JSON and text readable, images and PDFs inline, other types a download button. A run whose answer is empty shows "No information was found for this" instead of an empty card or raw data, and the agent prompts ask it to say so in a sentence. Finished tasks older than 7 days (`TASK_COMPACT_AFTER_DAYS`, 0 disables) lose their activity and attempt history, and an answer longer than ~600 characters is shortened **only when the full answer is stored as a file for that task**; title, goal, status, timestamps and playbooks stay; running and recent tasks are never touched. The task page says when an answer was shortened.
- **Why**: Files were saved but could only be downloaded, so they were invisible in practice. Old tasks piled up long text nobody reads again, while the file already holds the full answer.
- **Limit**: There is no per-task "keep forever" flag yet; a result worth reusing should be saved to a list (or become a playbook), which are separate files that trimming never touches.
- **Location**: `web/src/app/artifacts/[id]/page.tsx`, `web/src/lib/result-text.ts`, `internal/repository/task_repo.go` (`CompactFinishedTasks`), `cmd/griffin-server/main.go`, `tests/task_lifecycle_test.go`

#### ADR-067: The owner is the authority; trust follows the source
- **Status**: PLANNED (PRD 2, 7.8); supersedes the earlier "approval gates stay on" framing in ADR-059/063 where they read as vetoes.
- **Trigger**: The owner asked Griffin to message a friend with strong language. It answered that it had no WhatsApp access and would not send the wording. The owner's position: an assistant should listen and advise only when necessary, and must not have the authority to deny their wishes.
- **Decision**: (1) Instructions from the owner are never second-guessed or refused by Griffin. (2) Every confirmation before an action (send, submit, buy, delete) is the owner's setting: ask, auto or off per action class and per contact, including auto for everything; it is not a veto. (3) Anything Griffin *reads from outside* (web pages, emails, other people's messages, files) is untrusted data and can never by itself trigger an action; this is what keeps full access safe against hijacking, and it never applies to the owner's own instructions. (4) A hosted model's own refusals cannot be changed from inside Griffin, so wording a hosted engine declines is routed to a voice engine that does not refuse (a self-hosted model), and the owner is told which engine ran.
- **What the refusal actually was**: two causes. The agent has no tools, so it truly could not reach WhatsApp. And the hosted model's own policy declined the wording. Griffin controls the first (tools) and can only route around the second.
- **Boundary kept**: nothing built here targets mass harassment of third parties; the owner's own messages to their own contacts are theirs to write.
- **Not built yet**: the trust settings screen (plan 3.7), the voice engine (2.8) and the refusal fallback (2.9).
- **Location**: `docs/PRD.md` sections 2 and 7.8

#### ADR-068: Parallel sub-agents, network-safe tools and reusable app skills (planned)
- **Status**: PLANNED (PRD 7.10 to 7.12, plan 2.7, 2.10, 2.11).
- **Decision**: JJ chooses per task how many parallel sub-agents to run (bounded by runner capacity, extra ones queue) and joins their results with a visible list of who did what. Web search and an HTTP fetch tool are read-tier tools and ship only with network safety (private and link-local addresses, including the instance metadata service, are refused; scheme, size and time limits), because the worker's cloud identity would otherwise be reachable from a malicious page. App skills extend playbooks: the first run explores and saves how the app works (never what was said or any credential); later runs replay it and re-explore only when a step stops matching, marking the skill stale and updating it in place.
- **Unknown, to measure first**: how `agy`'s own sub-agents behave under our workspace isolation (capture one real run before deciding to use them).
- **Location**: `docs/PRD.md` sections 7.10 to 7.12

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
- **Location**: [`internal/provider/antigravity/workspace.go`](./internal/provider/antigravity/workspace.go)

#### ADR-016: Structured Output via Native `--json-schema`
- **Status**: IMPLEMENTED (Phase 2)
- **Decision**: Griffin task prompts pass JSON schemas via `--json-schema` directly to `agy`, enforcing that LLM responses deserialize into typed Go domain structs and structured output JSON without regex heuristics.
- **Rationale**: Empirically validated in Phase 0.5 and Phase 2; guarantees deterministic JSON formatting from Gemini models.
- **Location**: [`internal/provider/antigravity/executor.go`](./internal/provider/antigravity/executor.go), [`internal/provider/antigravity/parser.go`](./internal/provider/antigravity/parser.go)

#### ADR-023: Process Group Containment (`Setpgid`) and Two-Stage Termination
- **Status**: IMPLEMENTED (Phase 2)
- **Decision**: Subprocesses are spawned in separate process groups (`cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}`). On timeout or cancellation, `SIGTERM` is sent to `-pgid`, followed by a 5-second graceful wait, and `SIGKILL` to `-pgid` if processes remain alive.
- **Rationale**: Completely eliminates zombie processes and orphan child tool processes spawned by `agy`.
- **Location**: [`internal/provider/antigravity/executor.go`](./internal/provider/antigravity/executor.go)

#### ADR-024: Pure `argv` Slice Subprocess Execution without Shell Interpolation
- **Status**: IMPLEMENTED (Phase 2)
- **Decision**: Commands are launched via `exec.Command(binaryPath, args...)` using discrete string slices. Shell execution (`sh -c`, `bash -c`, or string interpolation) is strictly forbidden.
- **Rationale**: Neutralizes shell injection vulnerabilities from user-provided prompts, schemas, and parameters.
- **Location**: [`internal/provider/antigravity/executor.go`](./internal/provider/antigravity/executor.go)

#### ADR-025: Path Traversal Defense on Workspace Operations
- **Status**: IMPLEMENTED (Phase 2)
- **Decision**: All workspace directory creation and cleanup operations enforce `filepath.Clean` and verify that the target directory strictly begins with `rootDir + filepath.Separator`. Violations return `ErrWorkspacePathTraversal`.
- **Rationale**: Prevents malicious or corrupted task identifiers from modifying or deleting directories outside the designated scratchpad area.
- **Location**: [`internal/provider/antigravity/workspace.go`](./internal/provider/antigravity/workspace.go)

#### ADR-026: Multi-Turn Conversation Continuation via `--conversation <id>`
- **Status**: IMPLEMENTED (Phase 2)
- **Decision**: The executor extracts `conversation_id` from task input envelopes and forwards it via `--conversation <id>` to `agy`. Upon task completion, new or returned conversation IDs are captured and stored with the execution result.
- **Rationale**: Enables multi-step missions and interactive conversation threads across discrete tasks while preserving full context in Google Antigravity.
- **Location**: [`internal/provider/antigravity/executor.go`](./internal/provider/antigravity/executor.go)

#### ADR-027: Typed Authentication Failure Detection & Immediate Halt
- **Status**: IMPLEMENTED (Phase 2)
- **Decision**: Subprocess outputs indicating authentication expiry ("authentication required", "unauthenticated", "token expired") are parsed into `ErrAuthenticationRequired`. The worker disables retries (`retryable: false`), logs a critical operator alert, records an `AUTHENTICATION_REQUIRED` event in PostgreSQL `task_events`, and halts execution without silent paid fallback.
- **Rationale**: Enforces Griffin's zero-paid-fallback guarantee and prevents wasteful, endless retry loops on invalid credentials.
- **Location**: [`internal/provider/antigravity/parser.go`](./internal/provider/antigravity/parser.go), [`internal/worker/worker.go`](./internal/worker/worker.go)

#### ADR-028: Two-Tier Subprocess Testing Strategy
- **Status**: IMPLEMENTED (Phase 2)
- **Decision**: AI provider testing is divided into:
  1. Hermetic controlled subprocess mock tests using temporary scripts (sub-second, runs in CI without tokens).
  2. Opt-in live integration probe against installed `/usr/bin/agy` gated behind `GRIFFIN_ENABLE_LIVE_AGY_TEST=true`.
- **Rationale**: Guarantees reproducible, fast automated testing in development and CI environments while retaining an automated verification probe for live binary compatibility.
- **Location**: [`internal/provider/antigravity/executor_test.go`](./internal/provider/antigravity/executor_test.go), [`tests/worker_test.go`](./tests/worker_test.go)

---

### Scale-to-Zero & On-Demand Cloud Runner Architecture

#### ADR-029: On-Demand Compute via Scale-to-Zero Architecture
- **Status**: IMPLEMENTED (Scale-to-Zero)
- **Decision**: Pivot from an always-on 24/7 EC2 compute model to on-demand execution where the EC2 runner stops during idle periods and wakes up on runnable work. Ingress API stays online 24/7 independently on serverless compute. Supabase PostgreSQL remains the durable source of truth.
- **Rationale**: Slashes monthly EC2 compute costs by over 90% (from ~$16.35/mo to ~$1.34/mo for typical 2hr/day workloads) while preserving unattended execution, encrypted EBS state, and zero-paid-fallback guarantees.
- **Location**: [`docs/SCALE_TO_ZERO_ARCHITECTURE.md`](./docs/SCALE_TO_ZERO_ARCHITECTURE.md), [`internal/runner/`](./internal/runner/)

#### ADR-030: Asynchronous 202 Accepted Task Ingestion
- **Status**: IMPLEMENTED (Scale-to-Zero)
- **Decision**: When `SCALE_TO_ZERO_ENABLED=true` and tasks are submitted in `QUEUED` state, the API persists the task durably in PostgreSQL, triggers runner wake-up asynchronously, and immediately responds to the client with `202 Accepted` and `X-Runner-Status: WAKEUP_SIGNALED`.
- **Rationale**: Decouples client HTTP latency (<20ms response time) from cloud VM cold-boot latencies (30–45s boot time), preventing HTTP timeouts on edge and mobile clients.
- **Location**: [`internal/api/handlers/tasks.go`](./internal/api/handlers/tasks.go)

#### ADR-031: Atomic Pre-Shutdown Database Recheck to Neutralize Shutdown Races
- **Status**: IMPLEMENTED (Scale-to-Zero)
- **Decision**: When the idle shutdown timer expires, the worker pool pauses task claims and enters `DRAINING`. Once in-flight executions finish, the monitor performs an atomic database recheck (`CountRunnableTasks(ctx)`). If ANY runnable task arrived during draining, or if the recheck fails, shutdown is aborted, workers resume, and the monitor resets to `ACTIVE`.
- **Rationale**: Prevents task starvation and race conditions where tasks arriving milliseconds before machine shutdown would otherwise remain stranded until an external trigger.
- **Location**: [`internal/runner/shutdown.go`](./internal/runner/shutdown.go)

#### ADR-032: Debounced Wake-Up Controller with Stopping-State Retry Backoff
- **Status**: IMPLEMENTED (Scale-to-Zero)
- **Decision**: The wake-up controller enforces a 30-second cooldown window between AWS `ec2:StartInstances` API calls, protecting against request stampedes. If the EC2 instance is currently in `stopping` state (which returns `InvalidInstanceState` on immediate `StartInstances`), the controller enters a background retry loop polling with exponential backoff until the instance reaches `stopped`, then issues `StartInstances`.
- **Rationale**: AWS EC2 rejects start commands on instances that have not yet fully stopped. Debouncing and retries guarantee reliable runner startup without race crashes.
- **Location**: [`internal/runner/wakeup.go`](./internal/runner/wakeup.go)

#### ADR-033: Decoupled Multi-Mode Binary Entrypoint (`GRIFFIN_MODE=all|api|worker`)
- **Status**: IMPLEMENTED (Scale-to-Zero)
- **Decision**: The single `griffin-server` binary supports three runtime modes:
  1. `all`: Monolithic mode running both API server and background workers (default for local dev).
  2. `api`: Ingress-only mode running HTTP endpoints and wake-up controller (for serverless edge or container hosting).
  3. `worker`: Headless runner mode running worker pools, task execution, and idle shutdown monitor on EC2.
- **Rationale**: Allows deploying identical, battle-tested Go code across serverless ingress and stoppable EC2 compute without maintaining separate codebases.
- **Location**: [`cmd/griffin-server/main.go`](./cmd/griffin-server/main.go), [`internal/config/config.go`](./internal/config/config.go)

#### ADR-034: External Periodic Scheduler Tick via EventBridge
- **Status**: IMPLEMENTED (Scale-to-Zero)
- **Decision**: Griffin exposes an authenticated `POST /api/v1/scheduler/tick` endpoint protected by `X-Worker-Key`. AWS EventBridge Scheduler triggers this endpoint every minute to inspect PostgreSQL for due cron tasks and trigger runner wake-up when runnable work exists.
- **Rationale**: Offloads periodic scheduling from the stoppable EC2 VM to free-tier cloud serverless infrastructure, ensuring scheduled tasks (such as morning briefings) wake the runner right on time.
- **Location**: [`internal/api/handlers/scheduler.go`](./internal/api/handlers/scheduler.go), [`docs/SERVERLESS_API_DESIGN.md`](./docs/SERVERLESS_API_DESIGN.md)

### Phase 3: JJ Orchestrator, Manager Framework & Agent Factory

#### ADR-018: Two-Level Agent Hierarchy (Orchestrator -> 11 Managers -> Specialist Workers)
- **Status**: IMPLEMENTED (Phase 3)
- **Decision**: Griffin separates concerns into:
  1. **Top-Level Orchestrator (JJ)**: User intent classification, structured DAG plan generation, delegation, monitoring, deliverable aggregation, and final response synthesis.
  2. **11 Specialist Managers**: Domain experts (`career`, `research`, `engineering`, `hackathon`, `freelance`, `networking`, `tech_intelligence`, `learning`, `personal_ops`, `finance`, `product_ideas`) with verification criteria.
  3. **Specialist Workers**: Focused task executors instantiated with injected system prompts, schemas, tools, and capability boundaries.
- **Rationale**: Prevents monolithic context degradation and prompt bloat. Enables clean separation of concerns and strict capability boundaries.
- **Location**: [`internal/service/orchestrator.go`](./internal/service/orchestrator.go), [`internal/manager/manager.go`](./internal/manager/manager.go), [`docs/JJ_ORCHESTRATOR.md`](./docs/JJ_ORCHESTRATOR.md)

#### ADR-035: Kahn's Algorithm DAG Plan Validation & Cyclic Dependency Defense
- **Status**: IMPLEMENTED (Phase 3)
- **Decision**: All plans generated by JJ (whether via AI or heuristic fallback) must be validated before execution using Kahn's topological sort algorithm to detect cycles and enforce strict DAG linearity.
- **Rationale**: Prevents deadlock in the task engine caused by circular dependency graphs generated by LLMs.
- **Location**: [`internal/domain/orchestration.go`](./internal/domain/orchestration.go), [`internal/domain/orchestration_test.go`](./internal/domain/orchestration_test.go)

#### ADR-036: Reusable Specialist Worker Definitions with Versioned Immutability & Audit Trail
- **Status**: IMPLEMENTED (Phase 3)
- **Decision**: Specialist workers are defined via `WorkerDefinition` models with strict immutability. Updating a worker definition never mutates existing database rows; it creates a new row with an incremented version number (`version + 1`). All mutations record audit records in `agent_factory_audits`.
- **Rationale**: Ensures complete auditability and guarantees that historical task executions maintain immutable references to the exact worker definition and prompts that executed them.
- **Location**: [`internal/domain/worker_definition.go`](./internal/domain/worker_definition.go), [`internal/service/agent_factory.go`](./internal/service/agent_factory.go), [`docs/AGENT_FACTORY.md`](./docs/AGENT_FACTORY.md)

#### ADR-037: Centralized Capability Taxonomy & Automated Approval Checkpoints
- **Status**: IMPLEMENTED (Phase 3)
- **Decision**: Define a centralized taxonomy of 11 explicit capabilities (`research:read`, `repo:read`, `repo:write`, `web:read`, `email:draft`, `email:send`, `calendar:read`, `calendar:write`, `financial:action`, `job:apply`, `infra:destructive`). Sensitive capabilities (`repo:write`, `email:send`, `financial:action`, `job:apply`, `infra:destructive`) automatically require human approval (`domain.RequiresApproval`). Tasks requesting sensitive actions pause in `AWAITING_APPROVAL` until signed off by the user.
- **Rationale**: Enforces zero-trust capability security and prevents autonomous AI models from taking destructive or irreversible real-world actions without human oversight.
- **Location**: [`internal/domain/capability.go`](./internal/domain/capability.go), [`docs/CAPABILITY_AND_APPROVAL_POLICIES.md`](./docs/CAPABILITY_AND_APPROVAL_POLICIES.md)

#### ADR-038: JJ Orchestration Lifecycle (REQUEST -> PLAN -> VALIDATE -> DELEGATE -> MONITOR -> COLLECT -> VERIFY -> RESPOND) & Restart Recovery
- **Status**: IMPLEMENTED (Phase 3)
- **Decision**: JJ manages orchestration across 8 explicit phases. Root tasks represent overall user intent (`manager_domain = 'JJ'`), while child tasks represent delegated work. JJ recovers active in-flight orchestrations on system startup via `ReconcileInFlight`.
- **Rationale**: Guarantees zero lost work, seamless crash recovery across server restarts, and non-blocking monitoring of worker leases.
- **Location**: [`internal/service/orchestrator.go`](./internal/service/orchestrator.go), [`docs/JJ_ORCHESTRATOR.md`](./docs/JJ_ORCHESTRATOR.md)

#### ADR-039: Manager Deliverable Verification Contract & Domain Governance
- **Status**: IMPLEMENTED (Phase 3)
- **Decision**: Each manager implements `VerifyDeliverable` to validate child task outputs before JJ synthesizes final responses. The manager registry enforces governance, rejecting any attempt to register unauthorized or hallucinated manager roles.
- **Rationale**: Prevents hallucinated agent personas and ensures output deliverables meet domain-specific completeness and quality standards.
- **Location**: [`internal/manager/manager.go`](./internal/manager/manager.go), [`internal/manager/registry.go`](./internal/manager/registry.go), [`docs/MANAGER_FRAMEWORK.md`](./docs/MANAGER_FRAMEWORK.md)

---

#### ADR-112: The speech listener is one pure state machine, and nothing touches the phone's volume
- **Status**: IMPLEMENTED (`android/.../core/ListenerMachine.kt`, `Listener.kt`, tests in `ListenerMachineTest.kt`).
- **Decision**: Every decision the always-on listener makes (restart after silence, error handling, falling back from the on-device recogniser, pause while speaking) is one pure function, `ListenerMachine.reduce`. `Listener` owns only the Android recogniser and the clock. The earlier mute/restore of the system and notification volume around each listen cycle is removed; the on-device recogniser is tried first because it has no start chime on most phones, and where a chime remains it is left alone.
- **Why**: Muting `STREAM_SYSTEM`/`STREAM_NOTIFICATION` (aliased to the ring stream on most phones) on every restart flipped the ringer between vibrate and normal. The seven loose flags also hid a real bug: stopping with a restart pending left `restartPending` set, so every later restart was silently ignored.
- **Rule**: no code in the app may write a ring-aliased volume stream to hide a sound.

#### ADR-113: The phone registers itself as a device
- **Status**: IMPLEMENTED (`ApiClient.phoneHeartbeat`, `AssistantService.deviceBeat`); not yet confirmed on a physical phone.
- **Decision**: After sign-in the Android app registers as a `phone` host (`POST /api/v1/hosts`), keeps the host token in the keystore-backed `SecretStore`, and heartbeats every 30 s (a host counts as live for 45 s). Revocation (HTTP 401) drops the token so the next beat registers a fresh one; signing out forgets it. It advertises `phone_apps, sms, calls, messaging`.
- **Why**: The Devices page promised a token field in the app that did not exist, so only the laptop ever appeared.

#### ADR-114: An approval can be edited, and the edit re-binds the digest
- **Status**: IMPLEMENTED (`PATCH /api/v1/approvals/{id}`, `domain.WithEditedText`, `ApprovalRepository.EditPendingApproval`, `web/src/app/approvals/page.tsx`). Completes the "Edit Draft" promised by ADR-110.
- **Decision**: The owner can change the text of a pending message approval. In one transaction the task's messaging action, the approval's action type (a digest of channel, op, recipient and text), its summary and its payload are rewritten together, so the owner approves the digest of the text they actually read. The web approvals page now shows the draft and an inline editor. Every approval, including steps inside a plan, now sends a push (`ApprovalNotifier`) that opens the approvals page.
- **Why**: Before this the draft was not even displayed on the page, and the only way to change a message was to reject it and ask again.

#### ADR-115: Discuss first, then "go ahead"
- **Status**: IMPLEMENTED (`internal/service/discuss.go`, `handlers/jj_chat.go`). Implements the intent of ADR-076 without a new task state.
- **Decision**: "Let's discuss/tweak/plan it first" keeps a request a conversation in which the manager proposes a plan and waits. A later bare "go ahead / run it" submits what the owner said in that discussion (their turns only, assistant turns excluded) as one objective. Without one of those phrases nothing changes; a "go ahead" with no discussion in the history does nothing.
- **Why**: Jobs such as an internship filter need tuning before they run, and objectives otherwise start the moment they are understood.

#### ADR-116: Outreach is rows, not a document
- **Status**: IMPLEMENTED (`migrations/000007_outreach`, `domain/outreach.go`, `repository/outreach_repo.go`, `handlers/outreach.go`, `web/src/app/outreach`).
- **Decision**: A campaign has people (`outreach_targets`) whose status moves only along `domain.outreachTransitions` (found, drafted, approved, sent, replied, followup_due, closed), enforced in the SQL `WHERE` so two agents cannot both win. Every write touches one row. Sending is paced by limits kept on the campaign (daily cap, minimum gap, quiet hours; conservative per-channel defaults). `ClaimNextToSend` locks the campaign row to check the limits and leases the person for five minutes, so concurrent senders cannot exceed the cap or message anyone twice. Replies arrive through `POST /outreach/replies` (inbox check or notification) and move the person to `replied`; quiet senders become `followup_due` when listed. CSV export neutralises spreadsheet formulas.
- **The one rule**: a host (an agent's device) can add people, write drafts, claim, and report sent or replied, but **only the owner's sign-in can move a draft to `approved`** or change limits (tested). This is the mandatory approval gate of ADR-110, made structural.
- **Why**: A shared Markdown list loses concurrent writes and has no per-person status; rate limits and follow-ups need rows.

#### ADR-117: Use any site through the owner's own browser, and stop before anything is sent
- **Status**: IMPLEMENTED for the laptop (`domain/browseraction.go`, `hands/browser`, `hands/browserjob.go`, `cmd/griffin-host/browser.go`); verified against a real headless Chromium and a local page, **not yet against Gmail or LinkedIn**. The phone-side equivalent (an Android accessibility service) is NOT built.
- **Decision**: Instead of per-site code, a `browser` job gives a goal to a model that works the owner's signed-in browser profile step by step (a numbered view of the page's controls). The run stops BEFORE any step that sends, submits, pays, publishes or deletes, and before pressing Enter; that is decided by the model and again by the code (`irreversible`), so a hostile page cannot talk it into a harmless-looking send. The whole script (every typed word) is stored on the host under its digest, the owner approves that digest, and the next run replays exactly that script with no model. Each recorded step carries the element's role and name and the browser refuses if the page no longer matches. Only http/https addresses are allowed; password values are never read; page text is labelled untrusted. Sign-in happens once, by hand, with `griffin-host browser-login`; Griffin never sees passwords. Off unless `HOST_BROWSER=on`.
- **Honest limits**: sites restrict automated use of accounts (LinkedIn especially), so the outreach limits are conservative and approval is mandatory; the model can misread a page; a script that was approved on a changed page fails instead of guessing.

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
- **Location**: [`internal/storage/`](./internal/storage/), [`docs/S3_STORAGE_CONFIGURATION.md`](./docs/S3_STORAGE_CONFIGURATION.md), [`docs/STORAGE_ARCHITECTURE.md`](./docs/STORAGE_ARCHITECTURE.md)

#### ADR-041: Task-Scoped Object Hierarchy & Strict Private Access via Presigned URLs
- **Status**: IMPLEMENTED (Phase 4)
- **Decision**: All objects are stored under predictable hierarchical paths: `tasks/{task_id}/outputs/{sanitized_filename}` (or `artifacts/{uuid}/{sanitized_filename}`). S3 buckets block all public access. Client downloads are authenticated via short-lived presigned GET URLs (default 15 minutes) or authenticated streaming endpoints.
- **Rationale**: Guarantees strict multi-tenant isolation, protects sensitive agent outputs, prevents directory traversal attacks, and offloads heavy file download bandwidth directly to S3.
- **Location**: [`internal/storage/service.go`](./internal/storage/service.go), [`internal/api/handlers/artifacts.go`](./internal/api/handlers/artifacts.go)

#### ADR-042: Compensating Saga Pattern for S3 and PostgreSQL Consistency
- **Status**: IMPLEMENTED (Phase 4)
- **Decision**: To handle partial failures across AWS S3 and Supabase PostgreSQL without false distributed transaction claims, Griffin uses a Compensating Saga Pattern: payloads are uploaded to S3 first; if the PostgreSQL metadata insert fails, `StorageService` immediately executes a compensating `DeleteObject` against S3 using a detached background context.
- **Rationale**: Eliminates orphaned object accumulation in S3 during database transient errors or constraint failures while keeping both systems decoupled.
- **Location**: [`internal/storage/service.go`](./internal/storage/service.go), [`tests/storage_test.go`](./tests/storage_test.go)

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
- **Location**: [`web/src/`](./web/src/)

#### ADR-044: Cross-Origin Resource Sharing (CORS) and Web Client Authentication
- **Status**: IMPLEMENTED (Final Sprint)
- **Decision**: Wrap the Go API ingress in a CORS middleware supporting preflight `OPTIONS` and standard authorization headers. Provide `/api/v1/auth/login` to issue signed Supabase JWTs with `OWNER` privileges and automatic foreign-key user synchronization.
- **Rationale**: Enables frictionless local and production web client integration without cross-origin blocking or manual token manipulation.
- **Location**: [`internal/api/middleware/cors.go`](./internal/api/middleware/cors.go), [`internal/api/handlers/auth.go`](./internal/api/handlers/auth.go)

#### ADR-045: Live AWS S3 Integration Testing and Integrity Verification
- **Status**: IMPLEMENTED & LIVE VERIFIED (Final Sprint)
- **Decision**: Implement opt-in end-to-end integration test (`tests/s3_live_test.go`) validating `Upload`, `Download`, `Head`, `PresignDownloadURL`, and `Delete` against real AWS S3 bucket `griffin-artifacts-<AWS_ACCOUNT_ID>-ap-south-1` using `griffin-bootstrap` credentials in `ap-south-1`.
- **Rationale**: Guarantees zero regression and genuine live cloud compatibility before production release.
- **Location**: [`tests/s3_live_test.go`](./tests/s3_live_test.go)

#### ADR-046: Cloud Runner Unattended Authentication and Lingering Session Pattern
- **Status**: IMPLEMENTED (Final Sprint)
- **Decision**: Configure EC2 cloud runners with systemd user lingering (`loginctl enable-linger griffin`) and persistent DBus session daemons to allow headless Antigravity CLI execution without interactive OAuth browser prompts.
- **Rationale**: Essential for scale-to-zero compute where workers start on demand and execute background AI workflows without active SSH/SSM sessions.
- **Location**: [`infra/user-data.sh`](./infra/user-data.sh), [`docs/ANTIGRAVITY_CLOUD_RUNNER.md`](./docs/ANTIGRAVITY_CLOUD_RUNNER.md)


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
- **Decision**: CORS allows only `https://<your-app>.vercel.app`. In production, non-canonical Vercel hosts redirect to it.

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

#### ADR-069: Agent hierarchy - managers are brains, workers do all labor (planned)
- **Status**: PLANNED (PRD 7.13, plan 2.12, 2.13).
- **Decision**: JJ understands the ask, picks sub-managers and talks to the owner; each sub-manager only plans its slice, assigns tasks to workers, tracks them, asks a worker to summarize and passes the summary up; workers do every piece of labor, including summaries and JJ's final answer. Managers get no executor and no tools, enforced in code with a test. Messages are task records, so the chain is auditable. Every worker has its own terminal (live view) and every sub-manager its own decision log. Task tabs follow the kind of task: managers show Overview, Plan and Log; workers show Result, Terminal, Attempts and Files, which removes the old overlap between Activity and Subtasks.
- **Today**: the 11 managers already only route, interpret and verify; JJ's final answer is stitched in code (`buildFinalResponse`) and there is no manager log or live terminal yet.
- **Trade-off**: a worker-run summary costs one more agent run (about 13 s warm); skipped for one-step plans and eased by conversation reuse.

#### ADR-070: Voice output and phone calls (planned)
- **Status**: PLANNED (PRD 7.14, plan 1.9, 2.14, 4.4 to 4.6, 3.8).
- **Decision**: A Listen button speaks results sentence by sentence (device voices first, neural voices on the worker later), with a Settings picker that previews each voice. Character and anime voices are not shipped (copyright and a real voice actor's likeness); a bring-your-own voice slot is offered instead, and a real person's voice is never cloned for calls. Calls run through the Android bridge, always with approval (who, what will be said, maximum length), and approvals can be given in chat: cards plus typed replies bound to the specific pending approvals, accepted only from the owner's session.
- **Limits stated**: one SIM cannot place several independent calls at once (about one active plus one on hold, or a conference); truly parallel calls need a cloud telephony number (a new paid service); a live two-way conversation needs call audio that Android restricts, so a scripted call comes first. Rules on automated or AI-voiced calls and recording differ by country; the owner is responsible.

#### ADR-071: Execution hosts - laptop first, EC2 as backup; documents in S3 (planned)
- **Status**: PLANNED (PRD 7.15, plan 2.15, 2.16, 3.9, 5.1, 5.5).
- **Decision**: An optional host agent on the owner's laptop connects out to Griffin through a new Host API using a scoped device token (no database access) and runs `agy` and Claude Code with the owner's own logins, each task in its own scratch folder limited to folders the owner allows. While its heartbeat is fresh it gets the work and the EC2 is not woken; otherwise the EC2 worker runs `agy`. Claude steps run on the laptop, and when it is off they queue or fall back to `agy` by the owner's policy. A phone running Claude Code in a terminal app is an experimental extra host. The owner's resume context and role-specific resume drafts live in a Documents library in their own S3 area, tagged, chosen per task by tags and keywords, never written to logs or playbooks.
- **Why**: keeps logins off the cloud, removes cold-start waits and EC2 cost while the laptop is on, and keeps the always-on backup.
- **Risk**: a task on a personal machine can touch personal files, so scoping and logging are part of the design, not optional.

#### ADR-072: Devices that are ready do the work; the EC2 only thinks (approved plan v4)
- **Status**: APPROVED; M1 partly built (Listen, voice picker, tabs by kind, manager-purity test). Supersedes the EC2-centric parts of ADR-059, ADR-063 and ADR-071.
- **Decision**: Tools, browser, logins and app control live on the laptop and the Android phone, where everything is already set up. The EC2 and API keep the queue, planner, approvals, storage and a text-only `agy` backup, and never hold tools or credentials. Steps declare what they require; online machines advertise what they can do; a step is claimed by a machine that covers it, laptop first, and waits (not fails) when its device is off. Host presence needs no migration (an `agents` row). Hosts talk to a small Host API with per-host keys, never to the database.
- **Why**: setting up and authenticating tools on the EC2 is slow and puts credentials in the cloud; the phone's apps and SIM are unreachable from a datacenter. Companies that automate calls rent numbers from telephony providers instead of using SIM cards, so calls became an optional paid milestone (M6) rather than SIM automation.
- **Alternatives researched for every rejected idea**: PRD section 9. Sources are vendor pages and bug trackers, not regulators; prices and India telecom rules must be confirmed before spending money.
- **Deferred**: a worker-run summarize step (extra ~13 s for little gain since multi-step plans already end in a worker synthesis); revisit with live Terminals.
- **Built so far**: `web/src/lib/speech.ts`, the Voice section in Settings, Listen on results, tabs by task kind (`web/src/app/tasks/[id]/page.tsx`), `internal/manager/purity_test.go` (managers may not import worker, provider, runner, storage, os, net or process packages; verified to fail on a violation).

#### ADR-073: Agent concurrency follows the machine, not a fixed number
- **Status**: BUILT (`internal/provider/antigravity/limiter.go`); ships with the next worker deploy.
- **Decision**: `MAX_CONCURRENT_ANTIGRAVITY` is now a ceiling (default 4), not a fixed cap of 2. A limiter admits one more `agy` run only while free memory minus a reserve covers a learned per-agent size (plus 20%) and the 1-minute load is under CPUs x `AGY_LOAD_FACTOR`. The per-agent size starts at `AGY_AGENT_MB` and is updated from the resident memory of each finished run's process group. One agent is always admitted when none is running, so work never stalls. The worker asks the executor for capacity before claiming a task, so a task is never claimed and then left waiting.
- **Why**: a fixed 2 wasted the machine on light tasks and could not grow with a burst of sub-agents; a fixed high number risks out-of-memory kills on the 4 GB instance. Measuring what runs really cost adapts to both.
- **Knobs**: `AGY_RESERVE_MB` (900), `AGY_AGENT_MB` (500), `AGY_LOAD_FACTOR` (1.5). Non-Linux hosts without `/proc` fall back to the ceiling alone.
- **Limit**: the ceiling still bounds a very large fan-out; hosts (laptop, M2) carry their own limiter.


#### ADR-074: Two modes - brain and hands are separate; laptop-primary, phone-primary
- **Status**: M2 BUILT (host protocol, laptop host, mode election, wake suppression; see ADR-077); phone host (M5) PLANNED (PRD 7.15). Refines ADR-071 and ADR-072.
- **Trigger**: The owner's target: with the laptop on, everything runs from it with its full setup (`agy`, Claude Code, all tools); with it off, the phone is primary and reaches apps such as WhatsApp and Instagram within limits the owner sets; tasks accept follow-ups; the career-context files are mirrored in S3.
- **Decision**: Split **brain** (where the agent session reasons) from **hands** (devices that perform a capability). Laptop live (heartbeat under 45 s): laptop is brain and hands, the EC2 is not woken. Laptop off, phone live: the EC2 `agy` is a text-only brain and the Android phone host is the hands. Neither live: text steps run, hands steps wait. The EC2 still never holds tools or logins. The phone host is an Android app that connects out with its own token; iOS cannot be a host.
- **Outward actions are never auto-retried** when a host drops mid-action; the task goes to *Needs you* with the last screenshot. Read and text steps requeue.
- **Why**: it keeps credentials off the cloud, removes cold starts and EC2 cost while the laptop is on, and makes laptop-off mode possible without putting apps in a datacenter.
- **Known cost**: in phone mode each UI step would round-trip to a cold EC2 `agy` (12-16 s). Required mitigations: skill replay, batched actions with checkpoints, warm sessions.
- **Rejected**: a cloud emulator that the EC2 controls (needs the owner's logged-in apps in the cloud).

#### ADR-075: Permissions are enforced below the agent; the bypass flag is allowed only inside a verified sandbox on the laptop host
- **Status**: PARTLY BUILT. Built: the sandbox package and canary self-test (shown in `griffin-host --check`), the policy engine with floors and intent check, the dashboard and API, the host-side gateway with a local audit log (ADR-079). Not built: any tool that calls the gateway, the network allow-list, audit upload, and any use of the bypass flag (PRD 7.16; plan M3). Narrows ADR-049's and ADR-063's rejection of `--dangerously-skip-permissions`: it stays refused on the EC2 worker (the startup check and its test are unchanged) and becomes allowed on the laptop host only.
- **Decision**: With the flag the agent no longer prompts, so a dashboard cannot rely on agent prompts. Enforcement is (1) an OS sandbox exposing only the task scratch folder and allow-listed folders, (2) a startup canary self-test that refuses the flag if the agent can read a file outside the allow-list, (3) a tool gateway that checks each outward action against policy and against the declared intent of its plan step (an action outside the intent asks even under *auto*), (4) a network egress allow-list, (5) an audit log with screenshots. Policy lives in the cloud (Settings → Permissions) and is cached signed on the host; hosts and agents cannot edit it. A **Full** mode exists but is time-boxed (default 2 h).
- **Why**: an agent with an unsandboxed shell that browses a page can be steered by that page. The intent check makes ADR-067's "outside text can never trigger an action" mechanical.
- **Rejected**: a localhost-only dashboard (fails when the laptop is off; reachable by an agent on that laptop); the bypass flag with no sandbox.

#### ADR-076: Every task is a conversation
- **Status**: PLANNED (PRD 7.17; plan M2).
- **Decision**: A task has a thread. A step can pause as *Waiting on you* with a question, not only an approval. A reply resumes it. A finished task can be continued, creating a linked step that starts with its transcript and files. Thread context lives in the database; a warm `agy` or Claude session is only a speed optimization, so switching between laptop and phone mode mid-thread loses nothing but speed. Approvals in chat (ADR-070) use the same mechanism.

#### ADR-077: Host protocol - a hosts table, per-host tokens, capabilities granted by the owner (M2)
- **Status**: BUILT, not shipped. Off until `HOSTS_ENABLED=true`. Migration 000005 must be applied before the new binary starts (the server checks the schema).
- **Decision**: (1) Hosts live in their own `hosts` table with RLS (owner policy); the earlier plan to keep presence on an `agents` row (ADR-072) was dropped because that table has no place for a token hash, a heartbeat or per-device grants. (2) A host authenticates with a bearer token (`ghk_` plus 32 random bytes); only its SHA-256 is stored; the plaintext is shown once at registration. Host routes grant the system scope themselves after authenticating, and every query they run filters by the host's own user id. (3) `capabilities` are granted by the owner, `advertised` is reported by the host, and the effective set is the intersection, so a host cannot escalate. (4) A step declares `input_payload.requires`; unknown names are rejected at task creation (silently dropping one would run a browser step as plain text). A step that requires nothing needs the `agent` capability. (5) The cloud runner's claim excludes steps that declare `requires`; `CountRunnableTasks` excludes them and excludes work leased to a host (`leased_by` prefix `host:`), so neither wakes nor holds awake the runner. (6) The wake path is wrapped by `HostAwareWakeup`: no wake while a live agent host exists; if presence cannot be read it wakes (a needless wake is cheap, a stuck task is not). The manual Wake button keeps the raw controller. (7) A live host is one seen within 45 s that is not paused or revoked; the laptop host beats every 15 s, and refuses a presence interval at or over the window. (8) Only the lease holder can renew, complete or fail a step. (9) A host interrupted mid-step reports nothing, so the lease expires and the step is requeued, not failed for good. (10) `griffin-host` advertises only capabilities this build backs (`agent`), runs `agy` text-only with tool tiers off and the bypass flags still refused, and refuses an `http` API URL except for localhost.
- **Verified**: unit tests (presence, mode election, capability matching), integration tests against Postgres (claim rules, cross-user isolation, revocation, pause, lease ownership, runner exclusion), the same under the production `griffin_app`/`griffin_system` roles, the agent loop against a fake API (five runs), and an end-to-end run of the real binaries.
- **Known limits**: the host cannot handle approval-gated steps yet (it fails them, non-retryable, rather than act); no live Terminal; no UI for hosts or waiting steps; a live but saturated host can leave plain steps queued while the runner sleeps (the next scheduler tick wakes it once the host goes quiet); `worker.InterpretResult` repeats `ProcessNext`'s result rules for HTTP-reporting hosts and should be merged with it later.
- **Location**: `migrations/000005_hosts.*`, `internal/domain/host.go`, `internal/repository/host_repo.go`, `internal/service/host_service.go`, `internal/api/handlers/hosts.go`, `internal/api/middleware/host_auth.go`, `internal/hostagent/`, `cmd/griffin-host/`, `internal/worker/outcome.go`, `tests/host_test.go`, `tests/host_rls_test.go`
- **Addendum (attempts and UI)**: a host's claim opens attempt N (N = retry count + 1) under a stable id derived from the task and N, and the complete or fail call closes it, so claim and finish update one record; the attempt is read before the transition (a retryable failure changes the retry count) and written only after the transition succeeded, so a refused completion leaves nothing behind. The web app gained a Devices page and plain-language device states (PRD 4.2); the page offers only capabilities the current laptop build can back.

#### ADR-078: Redeploy from the AWS console through a CodeBuild project, master-only
- **Status**: WRITTEN AND LOCALLY TESTED, not applied to AWS.
- **Trigger**: the owner wants to redeploy from the AWS console without a laptop and CLI.
- **Decision**: one CodeBuild project (`infra/aws/redeploy.yaml`), started by hand, no webhook. Its inline build step clones the repo, **refuses any commit that is not an ancestor of `origin/master`** (checked in the inline step, not in a repo script, so a changed script cannot bypass it), then runs `infra/aws/redeploy-build.sh`: migrations first (the schema is expanded before the new binaries start, ADR-077), then `deploy-backend.sh`. Default `MODE=plan`; `apply` is an explicit override. The migration URL is read from one Secrets Manager secret at run time and never printed. The role can update the API Lambda's code, start the runner, send the install script to that one instance, put release files and read that one secret. It cannot update the Lambda's configuration (that environment holds database URLs and API keys), touch IAM or create resources, so `HOSTS_ENABLED` is set by the owner in the console. Rollback is a build with `COMMIT` set to an earlier master commit.
- **Why not CodePipeline or a webhook**: a push-triggered deploy with credentials on a public repo is the larger risk; a manual build keeps a person in the loop and the plan step shows what will change first.
- **Why an x86 build host**: ARM build hosts are not available in every region; the Lambda and worker are cross-compiled to arm64 and the packaging scripts check the result.
- **Risk accepted**: whoever can merge to `master` can deploy once someone starts the build; merging is already the trust boundary (it also deploys the web app).

#### ADR-079: One pure decision point for permissions, a gateway that fails closed, policy stored in the cloud
- **Status**: BUILT (engine, API, sync, gateway, page); no tool calls the gateway yet.
- **Decision**: (1) `internal/policy.Decide` is the only place a decision is made; it is pure and has no I/O. Rules narrow by six dimensions; the most specific wins, ties go to the stricter, so a "never" cannot be undone by a same-level "allow". (2) Floors that no rule can lift: credential locations, buying and calling never automatic, and anything unrecognized is off. Full access never relaxes them. (3) The intent check turns an "auto" into an "ask" for any action outside what the plan step declared; it is the mechanical form of ADR-067. (4) The policy lives in the cloud (`permission_policies`, RLS) with a version; a save must name the version it read, so a phone and a laptop cannot silently overwrite each other. (5) Hosts read it through their token, cannot write it, refetch only when the heartbeat's version changes, ignore a policy that does not validate, and treat "no policy received yet" as deny-everything. (6) The gateway stamps its own device on every action (a tool cannot borrow the laptop's permissions) and audits every decision without message bodies. (7) A stored policy that no longer validates is not trusted: the default (ask) is served instead.
- **Why in the cloud, not a laptop-only dashboard**: a localhost dashboard fails when the laptop is off and could be edited by an agent running on it (ADR-075).
- **Verified**: 13 policy tests, each safety rule mutation-tested; API tests for defaults, versioning, validation, host read-only, heartbeat version, routes absent when hosts are off; the same under the production RLS roles; host sync tests (fetch once, refetch on change, ignore malformed, deny before sync); web helper tests; an end-to-end run with the real binaries.
- **Known limits**: no hands call the gateway, so nothing in production is enforced yet; "ask" is a stop until approvals from a host exist; Full access is stored and time-boxed but does not change the sandbox yet; folders are matched on the path the caller resolved (the tool must pass the real path after following symlinks); the audit log is local only.
- **Location**: `internal/policy/`, `internal/gateway/`, `internal/repository/policy_repo.go`, `internal/service/policy_service.go`, `internal/api/handlers/permissions.go`, `migrations/000006_*`, `internal/hostagent/` (sync), `web/src/app/permissions/`, `web/src/lib/permissions.ts`

#### ADR-080: WhatsApp hands - the laptop's WhatsApp Web first, the phone's app as the fallback, approval at the device
- **Status**: BUILT and tested against a stand-in WhatsApp page (real headless browser) and a fake phone; NOT yet run against the owner's real account or phone. Ships with migration-free code (no schema change).
- **Decision**: A step may carry `input_payload.messaging` (`op` send, read_recent or unread; contact or phone; text; optional `prefer` laptop or phone) and must require the `messaging` capability (refused at creation otherwise). A host with `messaging` runs it through `internal/hands`: (1) the action goes to the permission gateway; (2) "ask" pauses the step with an approval whose action type is `host_action:` plus a digest of the exact channel, op, recipient and text, so an approval cannot cover a different message; (3) the claim after the owner approves lists the approved action types and the executor proceeds only on a matching digest; (4) "off" and the floors cannot be overridden by an approval. Drivers: WhatsApp Web in a local Chrome, Chromium or Brave profile (`internal/hands/whatsapp/web.go`, chromedp), then the Android app over adb and uiautomator (`android.go`). `prefer` reorders them; the default is laptop first.
- **The rule that matters**: a second driver runs only when the first provably did nothing (`ErrNotReady`). After Enter or the tap on Send, any failure is `ErrUnconfirmed` and nothing is retried or tried elsewhere, because a second attempt could message the contact twice. Contacts match by exact name only (case and spacing aside); two matches is an error; a phone number is the reliable way to address someone.
- **Agent-composed actions**: a plan turns "text mom on whatsapp that I am late" into two steps: an agent writes the action as strict JSON, then the device step carries it out. That action is text produced by a model, so it is intended only for the contact the owner's own words named (`intent_contact`, found after verbs like to, tell, text); otherwise it asks even under an auto rule.
- **Why not**: the official WhatsApp Business API cannot message personal contacts; a Node/Playwright runtime would add an install for no gain over the Chrome already on the laptop.
- **Risk accepted and stated**: WhatsApp can change its page (selectors are one struct and `griffin-host whatsapp-check` reports which stopped matching) and can limit accounts that automate (volume stays low and human-paced; every send needs approval by default). The Android path reads only messages visible on screen and finds buttons by WhatsApp's resource ids, which differ by version.
- **Location**: `internal/domain/messaging.go`, `internal/hands/`, `internal/hands/whatsapp/`, `internal/hostagent` (approval request, approved actions), `internal/api/handlers/hosts.go` (`POST /api/v1/host/tasks/{id}/approval`, `approved_actions` on claim), `internal/service/planner_messaging.go`, `cmd/griffin-host/hands.go`.
- **Verified**: domain, driver (browser and phone), messenger-routing and executor unit tests; mutation checks on exact-name matching, no-retry after send, approval binding, composed-action intent and never-over-approval; API integration tests for the approval round trip; an end-to-end test with the real API, Postgres, host agent, gateway and executor (ask, approve, send exactly once; decline; auto rule; never rule).

#### ADR-081: A read-only web worker (search and read pages) as a host capability
- **Status**: BUILT and tested; search against live engines not exercised.
- **Decision**: Capability `web`. A step may carry `input_payload.web` (`op` search with a query, or fetch with a URL, or fetch `from_search` to read the top results of the previous step) and must require `web`. Reading goes through the same gateway (class read, app web), so the owner can set web to ask or off. The fetcher refuses private, loopback, link-local and cloud-metadata addresses at connect time (so redirects and DNS tricks cannot reach them), refuses non-HTTP schemes and embedded credentials, caps size and redirects, returns only text, and labels everything it returns as untrusted content. Search uses Brave's API when `BRAVE_SEARCH_API_KEY` is set, your own SearXNG when `SEARXNG_URL` is set, and DuckDuckGo's plain HTML as a keyless best effort; if every provider fails the step says how to fix it. The planner uses it only when a request explicitly asks to search the web or gives a URL: search, then read the top pages, then an agent step that answers from them and names its sources.
- **Why on a host**: it needs outbound internet from a real machine and no credentials; the EC2 worker stays text-only. When no device with `web` is online the step waits (the same "waiting for a device" state as any hands step) and the answer step runs once it finishes.
- **Rejected**: browsing with the owner's logged-in browser for plain search (mixes the owner's sessions into untrusted pages); a keyless scraper presented as reliable.
- **Location**: `internal/domain/webaction.go`, `internal/hands/web/`, `internal/hands/webjob.go`, `internal/service/planner_web.go`.


#### ADR-082: The Siri job needs a native Android app; the web app stays
- **Status**: PLANNED. `HOSTS_ENABLED=true` was set on the production Lambda on 2026-11-09 (migrations 000005 and 000006 applied first).
- **Decision**: Wake-by-name listening, caller announcements, answering and placing calls and reading the call log are done by a small Kotlin Android app installed from an APK. Each Siri feature maps to a standard Android mechanism (PRD 7.18); there is no reverse engineering of Siri.
- **Why**: a web page cannot listen in the background or see calls. An installed APK may use the call-log permission that a store app may not.
- **Limit**: Android blocks apps from recording or speaking into a normal call, so Griffin cannot converse with the person on the line over the SIM. That needs a telephony provider (M6).
- **Rejected**: a web-only voice mode (cannot run with the screen off); store distribution (call-log permission is restricted there).

#### ADR-083: The manager's name is one setting; the listener is built from it, not trained
- **Status**: the setting is BUILT (stored in `profiles.settings`, no migration; letters, digits, spaces, hyphens and apostrophes, at most 30, no control characters, so a name cannot carry an instruction into a prompt). Chat, task titles, prompts and web labels use it; a request that opens with the name is classified without it but stored as the owner wrote it. The phone listener is PLANNED.
- **Decision**: `manager_name` (default "JJ") is the only source of the name for chat labels, prompts, titles, greetings and the wake name. The phone listener is a recognizer limited to the name (plus "hey"), so a rename needs no training.
- **Trade-off**: a limited-vocabulary recognizer is less accurate than a trained wake-word engine and short names mis-fire. A trained engine would need a new model per name. Revisit if false triggers are too high on the real phone.

#### ADR-084: A fast hosted model covers chat while the worker is cold, with a handoff of the conversation
- **Status**: BUILT (`internal/provider/gemini`, `internal/api/handlers/voice.go`); key set on the Lambda. The route never creates a task: it answers small talk, says "On it." for work, and returns `handoff: true` so the client posts the text and the conversation to `/api/v1/jj/messages`, where history now also reaches plan steps. On any Gemini failure (no key, rate limit, declined, error) the turn is handed to the worker instead of failing.
- **Decision**: A voice route answers chat and small questions from a fast hosted model; work becomes a normal task that carries the conversation so far. Calls, timers and texts are parsed on the phone and never wait for a model. The key lives only on the server.
- **Cost**: spoken text goes to the model provider; a setting turns the fast path off.

#### ADR-085: The Vault: S3 files that appear by rule, by scan and by learning from misses; sensitive ones encrypted on the device
- **Status**: BUILT and tested (server rules mutation-tested; Go and browser encryption checked against one vector; the laptop client run against the real handler over HTTP). Not yet used from a phone or against production. Built on artifacts (metadata kind `vault`), so there is no migration; the miss list lives in `profiles.settings`. A wrong-document bug found in testing ("card" matched the wrong ID) was fixed by dropping filler words and refusing to guess between equally good matches.
- **Decision**: See PRD 7.18. Mirrors only allowed folders, one-time filename-only scan with the owner ticking, misses recorded and resolved on the laptop's next run, camera scan from the phone. PAN, Aadhaar, passport and similar are encrypted before upload with a passphrase key the owner holds.
- **Rejected**: syncing whole folders (leaks everything on one mistake); server-side encryption alone for identity documents (the cloud account would hold both lock and key).
- **Open**: coding with no laptop reverses "the EC2 stays text-only" and waits for the owner's decision.

#### ADR-086: Low-battery handover saves open work to a new branch and hands tasks back, in three stages
- **Status**: BUILT (`internal/handover`, `griffin-host` with `HOST_HANDOVER=1`); tested against real git repositories and a bare remote, including breaking the secret filter and the fast-forward rule on purpose to confirm the tests fail. Not yet observed on a real draining battery.
- **Decision**: 15% saves locally, 5% hands running tasks back (retryable, with the reason) and pushes, 2% saves and pushes once more. A snapshot uses a temporary git index and `commit-tree`, so it cannot change the owner's branch, index or files; it only creates or continues `griffin/handover-<time>` and pushes that one branch without force. Secret-like and large files are skipped.
- **Why not only at 2%**: a push needs the network and seconds, and a laptop can die from 2% almost at once. 2% is kept as the last call, and the thresholds are configurable.
- **Rejected**: committing on the owner's current branch (it rewrites their history and index); `git stash` (it touches the working tree); pushing everything with `git add -A` into a real index (a secret could ride along).
- **Known limits**: Linux only; unsaved editor buffers are not covered; `--no-verify` is used on the throwaway branch's push so a slow hook cannot block it; a task handed back uses one of its retries.

#### ADR-087: Build the Android app with the SDK command-line tools and Gradle, not Android Studio
- **Status**: toolchain BUILT and proven (a minimal APK builds in about a minute); the real app is not written.
- **Decision**: JDK 17, the SDK command-line tools and Gradle 8.9 under `~/Android`, driven by `scripts/android-env.sh`; the project is `android/` (Kotlin, AGP 8.7.3, minSdk 29, compileSdk 35). Tests that need no device run on the JVM. The app is installed on the owner's OnePlus over adb.
- **Why not Android Studio**: it is an IDE around the same three tools; the command line does the build and VS Code edits Kotlin. The SDK is a few hundred MB against several GB.
- **Why a real phone first**: an emulator has no real calls, SIM or microphone, which are the point of this app.
- **Limit**: no IDE layout editor or profiler; the UI is written in code.

#### ADR-088: A follow-up joins the thread of the task it continues
- **Status**: BUILT (`internal/domain/thread.go`, `internal/api/handlers/follow_up.go`, `web/src/components/thread.tsx`); mutation-tested on the server rules; not yet used in the live app.
- **Decision**: continuing a finished task creates a linked step (payload `thread_id` = the root task, `follow_up_of` = the task it continues) that starts with the thread's transcript. The thread root is the first task; a plan step's follow-up belongs to its plan. Follow-ups are listed inside the thread, not as separate tasks. A running task cannot be continued.
- **Why no schema change**: the thread is two payload fields; collecting a thread filters the owner's recent tasks (at most 500), which is fine at one owner's scale.
- **Rejected**: appending to the finished task itself (it would rewrite a result the owner already saw and break the attempt history); a separate threads table (a migration for something two fields can say).
- **Known limits**: the transcript is the requests and the readable answers, not files; a thread's history keeps the first request and the latest turns once it passes ten; chat-page messages are still separate tasks.

#### ADR-089: Use Claude's own sub-agents inside a step, and an LLM planner across steps
- **Status**: BUILT (`internal/provider/claudecli` lead mode, `internal/service/dynamic_planner.go`, `internal/domain/claude_options.go`); budget cap, read-only tool list, plan validation and the "never rewrite plans with rules" rule are mutation-tested. Owner's decision: Claude's sub-agents replace Griffin-made workers whenever Claude runs a job; with `agy`, the manager writes ephemeral role workers that imitate it. `CLAUDE_LEAD` is off until a Claude login exists on a worker. Facts checked: `claude` 2.1.319 on this machine offers `--agents`, `--model`, `--effort`, `--permission-mode`, `--allowedTools` and `--max-budget-usd`.
- **Decision**: see PRD 7.10. Griffin sets tools, budget and effort ceilings; Claude chooses the sub-agents and their models within them. The keyword planner stays as the fallback for the LLM planner.
- **Why**: the model is better than a word list at splitting work and picking a cheap model for easy parts, and the CLI exposes the controls needed to bound it.
- **Risks**: cost (hence a hard budget per step and a daily cap), a planner that over-splits (a step limit and a measured comparison against the keyword planner), sub-agents with tools the owner did not intend (allow-list by tier), and unverified behaviour (capture real runs first).

#### ADR-090: Finish notifications through ntfy, with a spoken summary on tap
- **Status**: BUILT (`internal/notify`, `internal/service/spoken.go`, `finish_notifier.go`, `/listen/[id]`); not yet received on a phone.
- **Decision**: the task engine tells a notifier when a top-level task reaches a final state; the notifier publishes a short, case-specific summary to the owner's ntfy topic, with the Listen page as the tap target. Ntfy was already the plan's choice over Web Push (no service worker, no keys, no subscription storage).
- **Why a page and not audio in the notification**: a notification cannot play sound by itself; the page speaks it, and a browser needs a touch first, hence the Play button fallback.
- **Rejected**: Web Push (more moving parts for the same result); a long notification body (it should be heard in seconds).

#### ADR-091: Decide new task versus continuing one by explainable signals, and say why
- **Status**: BUILT (`internal/service/thread_match.go`, `internal/api/handlers/auto_thread.go`); tested on made-up phrasings only.
- **Decision**: continuation words, editing words about existing things, back-references, topic overlap, and recency are added up; fresh-request wording subtracts; at 0.60 or more the message joins the thread, from 0.35 a new task is made and the earlier one offered. A chat conversation the page holds is one thread unless it is still answering. The response always carries the reason.
- **Why not a model**: it would add a call and a cold-start to every message, and a score can be explained and tuned; a model tie-break for the middle band is a possible later step.
- **Known limits**: the weights are guesses until used for real; the chat page does not show the notes or an override yet.

#### ADR-092: The Android assistant listens with the system recogniser and acts through standard Android services
- **Status**: BUILT as v0.1, installed on the phone; voice behaviour awaiting the owner's permission grants and a trial.
- **Decision**: a foreground microphone service runs the on-device speech recogniser continuously and matches the manager's name on its text (no trained wake-word model, so a rename needs no training). Calls go through the telephony service, texts need a spoken yes, timers and alarms are the app's own, and callers are announced through the call-screening role (the only reliable way Android shows an app who is calling). Real conversation goes to the existing voice route; work is handed to the manager with the conversation so far.
- **Why not a trained wake-word engine**: it needs a model per name; the owner wants to rename the manager freely. Revisit if false triggers are too frequent.
- **Why the app schedules its own alarms**: Android 15 blocks starting the Clock app from a background service.
- **Testing**: `cmd/griffin-devstub` is a local stand-in for the API (real voice handler, optional Gemini) used through `adb reverse`; debug builds accept a sign-in and test sentences by broadcast, release builds do not.
- **Limits**: see PRD 7.18. Permission consent is the owner's: adb on this phone cannot grant runtime permissions, and the app does not try to work around that.

#### ADR-093: "Hey" plus the name wakes it; nothing is kept unless it woke; the phone offers tools instead of taking over roles
- **Status**: wake phrase, privacy rules and animation BUILT and tested on the phone. The phone-as-host (tools) design is PLANNED.
- **Decision**: the wake phrase is "hey <name>", so a short name is not triggered by ordinary talk. Non-wake audio is dropped in memory (no log, no file, no network). Always-on listening uses the on-device recogniser only. Active listening is shown by a tone and an animation.
- **Why the indicator stays**: a third-party app cannot listen without the OS microphone indicator and a foreground notification. Siri and "Hey Google" avoid them because a dedicated low-power chip inside the OS does the listening, which is closed to other apps. Hiding or bypassing the indicator would defeat a security feature and is not done.
- **Phone as tools (planned)**: the app registers as a host of kind phone (the existing host protocol) and advertises `calls`, `sms` and `phone_apps`; the brain (the manager, Claude or agy) emits steps such as "call X", the phone claims and runs them with the same actions the voice commands use, and the permission engine and approvals apply. No default-assistant or default-dialer role is needed for that. A literal MCP endpoint in front of the same queue is possible for Claude Code. Opening apps from the background still needs an exemption (assistant role or an accessibility service).
- **Triggers that need no always-on microphone (planned)**: a Quick Settings tile, a widget, a notification action, the headset button, and modes (always, only when the screen is on or charging, only on a trigger).

#### ADR-094: Make a state machine explicit only where it is hidden, wrong, or untested; fix the task table; rebuild the assistant's
- **Status**: BUILT (`domain.taskTransitions`, `core/AssistantMachine.kt`); the full catalogue is PRD 7.19.
- **Decision**: the review found one machine that was documented but wrong (the task table disagreed with its SQL) and one hidden across four flags (the assistant). Both were made explicit with tests written from the existing behaviour first, so nothing changed for the owner except two small gains: the animation stays while the assistant waits for "send", and a pending text is cancelled when a new request starts. The other machines were documented, not rewritten.
- **Why the SQL stays the enforcement**: the repository's `WHERE status IN (...)` is what is atomic under concurrent workers; a Go-side check would add a race. The table is the written form and is tested for completeness.
- **Rejected**: rewriting every flag into a machine (risk without payoff); generating the SQL from the table (a large change to the most sensitive code).

#### ADR-095: Message through the chat apps' notification replies, not by opening them
- **Status**: BUILT in the Android app; awaiting a real-notification trial.
- **Decision**: read and reply through a notification listener and each notification's own Reply action, with spoken confirmation before any send, ducking audio focus, and a non-touchable overlay, so nothing interrupts a game. New chats with no notification go to Griffin's WhatsApp Web hands (laptop, with approval) for WhatsApp and are refused elsewhere.
- **Why**: it uses the apps as they are built (their own sending, their own accounts) and needs no screen, no accessibility service and no default role.
- **Rejected for now**: driving each app's screen with an accessibility service (takes over the screen, brittle, a far more powerful permission); opening the app with a prefilled message (interrupts whatever is on screen).

#### ADR-096: Voice task and approval commands live on the server, with signed confirmations and an unlocked-device rule
- **Status**: BUILT and tested (parser, listing, confirmation security, ambiguity, route integration, Android handling); not deployed or tried on a device.
- **Decision**: one implementation (`service.VoiceCommands`) behind the existing voice route, used by the phone and the web app. Changes are two-step: the server answers with a question and an HMAC-signed token (owner, action, exact ids, two-minute expiry; no server state), and acts only when the token returns with a yes and `unlocked: true`.
- **Why**: a spoken "yes" cannot prove who spoke, so the unlocked-device rule and the exact-ids token stand in for authentication; and one copy of the rules means the two apps cannot drift.
- **Rejected**: client-side command parsing in each app (two copies); a server-stored pending action (state to expire and clean); allowing approvals while locked.
- **Known limits**: the parser is strict and phrase-based, so unusual wording falls through to conversation; the token secret is the Supabase JWT secret (a random per-process key when it is absent, as in local runs).

#### ADR-097: Smart Model Allocation between AGY Pro and Claude Pro CLI
- **Status**: IMPLEMENTED (`internal/domain/providerhint.go`, `internal/service/planner.go`, `internal/provider/router/router.go`).
- **Decision**: Differentiate AI provider allocation by capability and task characteristics:
  - **Google Antigravity CLI (`agy`)**: Assigned to all machine-side tasks, web search, page crawling, multi-company data gathering (`career_research`), and simple daily drafting (such as WhatsApp/Instagram greetings and messaging action formulation `wa_compose`, `insta_compose`).
  - **Claude Pro CLI (`claude`)**: Assigned to high-depth language reasoning tasks, specifically keyword-optimized resume and CV tailoring (`career_tailor`), building upon prior resume versions and target company requirements.
  - **Fallback Invariant**: If Claude is disabled, unauthenticated, or unhealthy, tasks gracefully fall back to `agy` without losing work or incurring API billing fees.

#### ADR-098: Client-Side Document Usage Tracking & Encrypted S3 Vault Auto-Caching
- **Status**: IMPLEMENTED (`internal/vault/tracker.go`, `cmd/griffin-host/main.go`, `cmd/griffin-host/vault.go`).
- **Decision**: Treat local desktop storage as a blank slate initially. As independent document files (`.pdf`, `.docx`, `.doc`, `.md`, `.txt`) are referenced across tasks:
  - Track file access frequency persistently in `~/.griffin-host/doc_usage.json` (chmod 0600).
  - Strict Exclusion: Never track or upload source code repositories, build artifacts, or dependency trees (`.git`, `go.mod`, `package.json`, `node_modules`, `vendor`, `target`, `dist`, `.venv`).
  - Auto-Cache Trigger: When an independent document is accessed more than once (`count > 1`), automatically evaluate and cache it to the user's S3 Vault:
    - Fails closed on symlinks, empty files, or files > 5 MB (`MaxFileBytes`).
    - Computes SHA-256 fingerprint for deduplication.
    - Encrypts client-side using AES-256-GCM (PBKDF2-HMAC-SHA256, 600,000 iterations) if classified as sensitive (`VaultID` or `VaultFinance`) or if a passphrase is provided.
    - Authenticated upload via `/api/v1/vault` into tenant-isolated S3 storage (`users/<user_id>/artifacts/`).

#### ADR-099: Dual-Interface Topology (Web PWA as Primary Console vs. Android Companion App)
- **Status**: IMPLEMENTED & DOCUMENTED.
- **Decision**: Clarify roles between the Web Application and the Android Native Companion:
  - **Web Application (Progressive Web App at `<your-app>.vercel.app`)**: The single primary user interface for all devices (laptop, desktop, phone). Provides full chat with JJ, task tree inspection, manual approvals, device status, file/vault explorer, and audio TTS. Can be installed directly on Android via Chrome ("Add to Home screen").
  - **Android Native App (`android/`)**: Built strictly as a background device bridge for voice wake-word listening ("Hey Griffin"), Quick Settings tile, call screening, and notification reply hooks. Users do NOT need the Android native app for standard task and chat interactions.

#### ADR-100: Multi-Channel Messaging Device Hands (WhatsApp & Instagram over Android ADB and Web Fallback)
- **Status**: IMPLEMENTED (`internal/domain/messaging.go`, `internal/hands/instagram/android.go`, `internal/hands/whatsapp/`, `cmd/griffin-host/hands.go`).
- **Decision**: Messaging tasks (`personal_ops` manager) route outward communication through user-owned devices:
  - **Priority 1 (Android Phone via ADB)**: `griffin-host` auto-detects connected Android devices via ADB (`findADB` discovers SDK toolchains at `$HOME/Android/Sdk/platform-tools/adb` and `$HOME/.local/share/axis-toolchain/android-sdk/platform-tools/adb`). Sends via WhatsApp (`com.whatsapp`) or Instagram Direct (`com.instagram.android` via `https://ig.me/m/<username>`).
  - **Priority 2 (Web Browser Fallback)**: For WhatsApp, falls back to WhatsApp Web on the laptop if no phone is connected.
  - **Security Gate**: Outward sends require human approval unless explicitly auto-approved by the owner's policy in the Permissions dashboard.
  - **Orchestrator Routing Invariant**: Top-level tasks submitted via chat, "New task" modal, or "Run again" with objective goals (including mobile keyboard slips such as `sned`, `snd`, `wapp`, `watsp`) are automatically routed to JJ Orchestrator (`whatsAppSteps`), generating `wa_compose` and `wa_do` (`requires: ["messaging"]`). Cloud workers (`griffin-worker`) are strictly prevented from claiming `wa_do` (`needsHandsSQL`), ensuring that messaging is never answered with a text-only "cannot send" on EC2.

#### ADR-101: Personal Lingo & Implicit Messaging Resolution with habit.md Profile
- **Status**: IMPLEMENTED (`habit.md`, `internal/domain/habit.go`, `internal/domain/habit_test.go`, `internal/service/planner_messaging.go`, `internal/service/planner_messaging_test.go`, `internal/service/classify_test.go`).
- **Decision**: Users communicate crudely, make keyboard slips (`sned`, `snd`, `wapp`, `watsp`), and use implicit shorthand without explicitly naming apps (e.g. "message bhai wassup", "tell bhai I will be late", "ping bhai wassup", "text bhai wassup"):
  - Root [`habit.md`](./habit.md) documents user lingo, behavioral habits, common contact aliases (`bhai` -> default brother contact, `maa`/`mom`, `papa`/`dad`), implicit messaging defaults, model allocation preferences (`agy` for research/search/composing vs `claude` for resumes/deep language), locked phone posture, and auto-caching rules.
  - `internal/domain/habit.go` defines `HabitProfile` with normalization routines (`NormalizeLingo`, `ResolveContact`).
  - `internal/service/planner_messaging.go` introduces `isImplicitMessaging` and layered contact extraction (`intentContactFrom` with `to <person>` prioritized before direct verbs and WhatsApp references). When a user asks to message/text/ping/tell a person, Griffin defaults to WhatsApp for personal messaging delivery while vetoing deliverables (reports, invoices, code, emails).
- **Why**: Eliminates friction and UX failure where informal voice/text inputs were misclassified or rejected due to lack of the exact word "whatsapp" or proper syntax.

#### ADR-102: Locked Phone Keyguard Dismissal and Seamless WhatsApp Web Fallback
- **Status**: IMPLEMENTED (`internal/hands/whatsapp/android.go`, `internal/hands/whatsapp/android_test.go`, `internal/hands/whatsapp/e2e_verification_test.go`).
- **Decision**: Real-world phone usage requires operating while the phone is locked with screen off, connected only via mobile data or Wi-Fi (the user will not keep the phone screen permanently unlocked or awake):
  - In `android.go` `ready(ctx)`, Griffin detects whether the device is locked (`mDnsKeyguardShowing=true`, `isStatusBarKeyguard=true`, or `mShowing=true`).
  - Attempt 1: Automatically wake and dismiss standard keyguards using `adb shell wm dismiss-keyguard` and `input keyevent 82` (KEYCODE_MENU). If `GRIFFIN_PHONE_PIN` is configured, automatically unlock via secure PIN input.
  - Fallback 2: If the screen remains locked behind secure biometrics/credentials, Android hand returns `ErrNotReady` with explanation. `whatsapp.Messenger` seamlessly falls back to WhatsApp Web on the laptop. Since WhatsApp multi-device syncs over cloud WebSockets, messages send reliably and headlessly without requiring the phone screen to be on or unlocked.
  - Automated Verification: End-to-end hermetic tests (`e2e_verification_test.go`) confirm that sent messages (`MessagingSend`) are verified and read back via `MessagingReadRecent` to guarantee delivery across both Android ADB and Web fallback paths.

#### ADR-103: Android SpeechRecognizer Resilience and Package Visibility Queries
- **Status**: IMPLEMENTED (`android/app/src/main/AndroidManifest.xml`, `android/app/src/main/kotlin/com/griffin/assistant/Listener.kt`).
- **Decision**: Speech recognition on Android companion app targeting API 35 (`targetSdk = 35`) encountered continuous retry loops with `SpeechRecognizer.ERROR_CLIENT` (code `5`):
  - **Manifest Package Visibility Queries**: On Android 11+ (API 30+), apps must declare `<intent><action android:name="android.speech.RecognitionService" /></intent>` and `<intent><action android:name="android.intent.action.TTS_SERVICE" /></intent>` inside `<queries>` in `AndroidManifest.xml`. Without this, package visibility prevents discovering or binding to speech recognition engines (e.g. Google Speech Services, Samsung Voice), failing immediately with client error 5.
  - **On-Device to Standard Fallback**: `SpeechRecognizer.isOnDeviceRecognitionAvailable(context)` returns true on API 33+ devices even when offline speech recognition packs have not been downloaded. Previously, this caused an unrecoverable crash/retry loop on `createOnDeviceSpeechRecognizer`. `Listener.kt` now automatically falls back:
    1. Try on-device recognition (`createOnDeviceSpeechRecognizer`) with offline preference.
    2. If `ERROR_CLIENT` (5) occurs, fall back to standard `createSpeechRecognizer(context)` with offline preference.
    3. If offline recognition still fails, drop `EXTRA_PREFER_OFFLINE` to enable online/system recognition.
    4. Consecutive client error counter applies exponential backoff (up to 10s) and displays clear diagnostics ("check Google app mic permission & offline speech pack") instead of aggressively spinning every 2 seconds.
- **Why**: Ensures voice assistant listening works universally across all Android OEM implementations (Pixel, OnePlus, Samsung, Xiaomi) regardless of whether offline language packs are pre-installed.

#### ADR-104: Mid-Flight Task Steering and In-Conversation Thread Deduplication
- **Status**: IMPLEMENTED (`internal/api/handlers/auto_thread.go`, `internal/api/handlers/follow_up.go`, `internal/service/task_engine.go`).
- **Decision**: When users speak or message while an earlier task is running (e.g. "give me the draft first", "also mention Friday", "wait don't send yet"), previously the system treated non-terminal tasks as uncontinuable (`g.settled() == nil`) and spawned disjoint, competing top-level tasks:
  - **Mid-Flight Steering on Active Tasks**: `JJChatHandler` now inspects in-flight tasks (`g.inFlight()`) in the current conversation. When a steering command is detected:
    - If requesting *"draft first"* / *"don't send yet"*, downstream outward messaging steps (`wa_do`, `send`) are intercepted and tagged with `[Draft Only]`, `draft_only: true`, and `requires_approval: true`.
    - If providing mid-flight additions (*"also mention..."*, *"make sure to say..."*), notes are appended directly to the active task's `steering_notes` in `InputPayload`.
    - Returns an instant, conversational acknowledgement without spawning a duplicate task.
  - **Thread Deduplication**: Ongoing conversation messages automatically link to `thread_id` and `follow_up_of` rather than scattering into 100s of separate task cards on the dashboard.
- **Why**: Eliminates user frustration when refining commands mid-stream and keeps the task dashboard organized into coherent, singular conversation threads.

#### ADR-105: Dual Wake Words ("oye bhosadike" / "hey <manager>"), Voice Draft Readout, and Mic Audio Suppression
- **Status**: IMPLEMENTED (`android/.../core/WakeMatcher.kt`, `android/.../core/AssistantMachine.kt`, `android/.../Listener.kt`, `internal/service/voice_commands.go`, `internal/api/handlers/voice.go`).
- **Decision**:
  1. **Dual Wake Phrases**: Added support for Hindi/colloquial wake triggers ("oye bhosadike", "oye bhosdike", "oye bsdk", "bhosadike") alongside standard "hey <manager>" and "hey griffin".
  2. **Voice Approvals & Draft Readout**:
     - Users can verbally inspect pending drafts before approving by saying "read out the draft" / "read draft" / "what's the draft". The assistant reads the draft text aloud and stays in the confirmation window.
     - Saying "approve" / "approve it" / "yes approve" / "send" confirms and fires the action.
     - Saying "ok, I'll check" / "check later" / "leave it" / "whatever" acknowledges and holds the task pending without failing or rejecting.
  3. **Open Tasks Query**: Added support for queries like "are there any open tasks", "any open tasks", "what are the open tasks", "list open tasks", returning and reading out active running tasks.
  4. **Speech Recognizer Beep & Silence Thrashing Suppression**:
     - System speech recognition start/restart sound muted via `AudioManager.STREAM_SYSTEM` mute/unmute during listening loops.
     - Silence timeout increased from 1.3s to 5s to eliminate rapid continuous restarting and audio cue pings.
- **Why**: Provides a seamless, hands-free voice experience with verbal approval safety and eliminates annoying audio artifacts from continuous speech recognition.

#### ADR-106: Zero-Chime Speech Recognition & Android Status Bar Icon Suppression
- **Status**: IMPLEMENTED (`android/app/src/main/AndroidManifest.xml`, `android/app/src/main/kotlin/com/griffin/assistant/AssistantService.kt`, `android/app/src/main/kotlin/com/griffin/assistant/Listener.kt`).
- **Decision**:
  1. **Status Bar Icon & Notification Channel Cleanup**:
     - Deleted the previous noisy `"assistant"` notification channel to wipe cached OS notification alerts.
     - Recreated an invisible `"assistant_silent"` notification channel with `IMPORTANCE_MIN`, `lockscreenVisibility = VISIBILITY_SECRET`, no sound, no vibration, and no lights.
     - Built the foreground service notification using `android.R.color.transparent` small icon, neutral `"Griffin"` title, `PRIORITY_MIN`, and `setSilent(true)`. This eliminates persistent mic status bar icons on OnePlus / OxygenOS and modern Android releases.
  2. **Elimination of Google SpeechRecognizer Start Chime ("Ting" Sound)**:
     - Standard Android speech recognition engines emit a 350ms chime on start or restart.
     - Added `<uses-permission android:name="android.permission.ACCESS_NOTIFICATION_POLICY" />` in `AndroidManifest.xml` and granted notification policy access to allow muting system and notification streams.
     - `muteChime()` zeroes and mutes `STREAM_SYSTEM`, `STREAM_NOTIFICATION`, and `STREAM_MUSIC`.
     - Delayed stream unmuting by 1000ms (`main.postDelayed({ restoreVolume() }, 1000L)`) past `onReadyForSpeech()` so the initial recognition chime is 100% inaudible before streams restore on user speech or turn completion.
- **Why**: Eliminates audio disruptions and status bar clutter, providing an unobtrusive and quiet background voice assistant on the user's phone.

#### ADR-107: Fast Intent Classification & Conversational Task Routing with Gemini Flash Lite
- **Status**: IMPLEMENTED (`internal/service/intent_classifier.go`, `internal/service/voice_commands.go`, `internal/api/handlers/voice.go`, `internal/api/handlers/jj_chat.go`, `internal/api/server.go`).
- **Decision**:
  - **Problem**: Brittle regex matching caused colloquial queries, Hindi/Hinglish turns ("give me the list of task", "kya tasks chal rahe hai", "tasks batao", "mere approvals kya hai", "pehle draft dikha dena abhi mat bhej") to slip past command detectors. They were routed to JJ Orchestrator as multi-step objectives, causing EC2 AGY worker cold-starts taking 8+ minutes and hallucinating synthetic task catalogs.
  - **Three-Tier Intent Architecture**:
    1. **Deterministic Fast Path (0ms)**: Strict regex evaluation (`ParseTaskCommand` and `ClassifyMessageWithReason`) immediately catches unambiguous commands and obvious objectives with zero latency and zero token cost.
    2. **Smart LLM Intent Classifier (~300ms)**: For natural language nuances, Hinglish, steering requests, or ambiguous utterances, Griffin invokes Gemini Flash Lite (`gemini-flash-lite-latest` / `gemini-3.5-flash-lite`) via `GEMINI_API_KEY`. It parses input into structured JSON intents: `list_tasks`, `list_approvals`, `approve`, `reject`, `cancel_tasks`, `delete_tasks`, `steer_task`, `conversation`, and `objective`.
    3. **Deterministic Conversation Fallback**: Ensures system resilience if external networks or models are temporarily unreachable.
  - **Direct Conversational Execution**:
    - Both `VoiceHandler` (`POST /api/v1/voice/turn`) and `JJChatHandler` (`POST /api/v1/jj/messages`) route classified task commands directly through `VoiceCommands.Run()`, returning instant conversational replies (e.g. "You have 2 tasks. Running: ... 1 finished.") in milliseconds without creating unnecessary EC2 worker tasks or spinning orchestrators.
- **Why**: Prevents unwanted task creation, eliminates hallucinated workforce catalogs on query commands, and provides robust multilingual comprehension across English, Hindi, and Hinglish.

#### ADR-108: Mid-Flight Task Steering and Real-Time Draft Interception
- **Status**: IMPLEMENTED (`internal/service/voice_commands.go`, `internal/api/handlers/auto_thread.go`, `internal/domain/thread.go`).
- **Decision**:
  - When users issue mid-flight directives on an ongoing task ("give me the draft first", "pehle draft dikha", "don't send yet", "also mention X", "change the message to Y"):
    - **Draft Request on Completed/Settled Work**: If the task has already produced draft content (`domain.ResultText(target.OutputPayload)`), Griffin immediately extracts the draft, presents it to the user ("Here is the draft: ... Would you like me to approve and send it?"), and sets `Draft` on the response, keeping the confirmation prompt active.
    - **Draft Request on In-Flight Work**: If the task is still running, Griffin marks `draft_only: true` and `requires_approval: true` on the target and any downstream messaging/sending children, and records `"User requested draft first: prepare draft only and hold outward send for approval."` in `steering_notes`. Outward transmissions are paused for approval without cancelling or restarting the task.
    - **Mid-Flight Additions**: Notes are appended to `steering_notes` in `InputPayload` and persisted via `Tasks.UpdateTask()`.
    - **Thread Auto-Steering**: In `auto_thread.go`, steering commands automatically latch onto active in-flight tasks even if the client omitted `conversation_id`, preventing hundreds of disjoint task cards from cluttering the dashboard.
- **Why**: Gives the user full real-time control over in-flight tasks, prevents unintended outward messages, and allows seamless iterative refinement without restarting tasks from scratch.

#### ADR-109: Headless Background Device Execution, System Controls, and Phone-First Invariant
- **Status**: IMPLEMENTED (`android/app/src/main/kotlin/com/griffin/assistant/Actions.kt`, `android/app/src/main/kotlin/com/griffin/assistant/core/Commands.kt`, `android/app/src/main/kotlin/com/griffin/assistant/AssistantService.kt`, `agents.md`, `habit.md`).
- **Decision**:
  1. **Phone-First Default Invariant**:
     - Any task involving device actions, notifications, communications, or system utilities defaults to the user's mobile device (`phone`) unless the user explicitly mentions `"laptop"`, `"desktop"`, or `"computer"`.
  2. **Headless Background Execution for Messaging**:
     - External chat automation (WhatsApp, Instagram, LinkedIn, SMS) must never aggressively launch foreground applications or hijack the user's phone screen.
     - In-bound notifications are answered headlessly via Android notification `PendingIntent` quick-reply (`Inbox.book.replyTarget`).
     - Fresh messages with no active notification session are routed headlessly to background web workers (e.g. WhatsApp Web automation on the laptop host agent) or queued with required draft approval.
  3. **Expanded Phone System Controls**:
     - Added voice and intent actions for device maintenance:
       - Ringtone: `openRingtoneSettings()` (`RingtoneManager.ACTION_RINGTONE_PICKER` / `Settings.ACTION_SOUND_SETTINGS`).
       - Wallpaper: `openWallpaperSettings()` (`Intent.ACTION_SET_WALLPAPER` / `Settings.ACTION_DISPLAY_SETTINGS`).
       - Developer Mode: `openDeveloperOptions()` (`Settings.ACTION_APPLICATION_DEVELOPMENT_SETTINGS`).
       - Browser Search: `searchWeb()` (`Intent.ACTION_VIEW` targeting Google Web Search).
- **Why**: Ensures frictionless device automation that respects the user's screen state, minimizes jarring app popups, and extends voice assistant control over everyday device settings.

#### ADR-110: Mandatory Pre-Application & External Submission Human Approval Gate
- **Status**: IMPLEMENTED (`habit.md`, `decisions.md`, `internal/domain/task.go`, `internal/api/handlers/auto_thread.go`).
- **Decision**:
  - **Zero Unapproved Outward Submissions**: Griffin is strictly prohibited from auto-submitting job or internship applications, sending LinkedIn cold outreach, or dispatching cold emails autonomously.
  - **Pre-Submission Staging Contract**:
    1. The worker parses the job opening, selects the appropriate resume variant, and tailors the bullets/cover letter using Claude Pro CLI.
    2. Instead of completing and executing the submission, the task transitions to `AWAITING_APPROVAL` with `requires_approval = true` and `draft_only = true`.
    3. An approval payload is generated containing:
       - Company Name & Role Title
       - Verified Stipend & Location / Remote Status
       - Application URL & Deadline
       - Tailored Resume Diff & Selected Base Resume File
       - Form / Questionnaire Answers & Cover Pitch Draft
    4. Griffin delivers an interactive approval card to the user's phone / dashboard with "Approve & Submit", "Edit Draft", or "Reject" buttons.
    5. Transmission or submission is blocked until explicit user confirmation is received via UI button click or voice command ("approve", "ha bhej do").
- **Why**: Protects the user's professional reputation, eliminates accidental submissions to mismatched roles, and ensures the user retains 100% control over their job search and personal brand.

#### ADR-111: Bulk Approvals Gate & Interactive List/File Action Interface
- **Status**: IMPLEMENTED (`internal/api/handlers/approvals.go`, `internal/api/server.go`, `internal/service/voice_commands.go`, `web/src/app/approvals/page.tsx`, `web/src/app/artifacts/page.tsx`, `web/src/app/lists/[id]/page.tsx`, `web/src/lib/api.ts`).
- **Decision**:
  1. **"Allow All" Bulk Approval Protocol**:
     - **API**: Introduced `POST /api/v1/approvals/batch` supporting atomic approval or rejection of all pending actions (or a specific array of IDs).
     - **Web UI**: Added an interactive "Allow All Pending Actions" card at the top of the Approvals dashboard (`/approvals`). Shows total pending count, single-click "Allow All ({count})" button, and "Reject All" button.
     - **Voice & Chat**: Recognizes "allow all", "approve all", "all approvals", "sab approve karo", and "saare allow karo", signing a unified confirmation token and executing batch approval upon the user's confirmation.
  2. **Interactive Files Interface (`/artifacts`)**:
     - Added filter tabs: `All Files`, `Trackers & Lists`, `Resumes & Career`, and `Task Outputs`.
     - In-line interactive buttons:
       - **View**: Launches an in-page quick-preview modal with live markdown/document rendering without leaving the page.
       - **Action**: Launches an interactive task runner with contextual presets (e.g. "Tailor resume with Claude", "Scrape open 30k+ internships", "Verify emails") and custom prompts, directly creating Griffin tasks linked to the artifact.
  3. **Interactive Lists Interface (`/lists/[id]`)**:
     - Added an interactive "Run Task on List" action bar with presets for refreshing internship trackers, tailoring resumes for listed openings, and monitoring outreach logs.
- **Why**: Eliminates repetitive one-by-one approval friction when managing dozens of job applications or outreach drafts, while turning static lists and file dumps into actionable execution hubs.





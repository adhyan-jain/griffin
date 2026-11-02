# Griffin Implementation Status Tracker (Antigravity Primary)

## 1. Project Phase
- **Current Phase**: **Phase 3 — JJ Orchestrator, Agent Factory & Manager Framework (COMPLETED & VERIFIED)**
- **Status**: JJ Orchestrator (8-stage lifecycle `REQUEST` -> `PLAN` -> `VALIDATE` -> `DELEGATE` -> `MONITOR` -> `COLLECT` -> `VERIFY` -> `RESPOND`), Kahn's algorithm DAG cycle detection, 11 Specialist Domain Managers (`career`, `research`, `engineering`, `hackathon`, `freelance`, `networking`, `tech_intelligence`, `learning`, `personal_ops`, `finance`, `product_ideas`) with deliverable verification and anti-rogue governance, Agent Factory with immutable versioning (`version + 1`) and audit logging, centralized capability taxonomy with automated approval gates, and process restart recovery (`ReconcileInFlight`) fully implemented and verified with 100% passing automated tests (62/62 tests passing across all packages).
- **Next Phase**: **Phase 4 — Next.js 15 PWA Interface & Real-Time SSE Streaming** (or **Phase 0.5 EC2 Cloud Runner Activation** upon user authorization).

---

## 2. Completed Implementation Deliverables

### Phase 3: JJ Orchestrator, Agent Factory & Manager Framework (`internal/service/`, `internal/manager/`, `internal/domain/`)
- **JJ Orchestrator Core Service**: [`internal/service/orchestrator.go`](file:///home/adhyan/Desktop/griffin/internal/service/orchestrator.go) — Full 8-stage lifecycle (`REQUEST` -> `PLAN` -> `VALIDATE` -> `DELEGATE` -> `MONITOR` -> `COLLECT` -> `VERIFY` -> `RESPOND`), root task creation (`manager_domain = 'JJ'`), DAG dependency linking, sensitive capability approval gating, deliverable collection, manager output verification, and process restart recovery (`ReconcileInFlight`).
- **DAG Plan Synthesis & Kahn's Cycle Validation**: [`internal/service/planner.go`](file:///home/adhyan/Desktop/griffin/internal/service/planner.go), [`internal/domain/orchestration.go`](file:///home/adhyan/Desktop/griffin/internal/domain/orchestration.go) — `AIPlanner` with JSON schema enforcement fallback to `RuleBasedPlanner`. Kahn's topological sort algorithm detects cyclic dependencies in generated plans and rejects invalid DAGs before database insertion.
- **Plan Adapter (Decoupled Dependency)**: [`internal/worker/plan_adapter.go`](file:///home/adhyan/Desktop/griffin/internal/worker/plan_adapter.go) — Adapts `worker.TaskExecutor` to consumer-side `service.PlanExecutor`, neutralizing Go circular package import cycles.
- **Manager Framework & Registry**: [`internal/manager/manager.go`](file:///home/adhyan/Desktop/griffin/internal/manager/manager.go), [`internal/manager/registry.go`](file:///home/adhyan/Desktop/griffin/internal/manager/registry.go) — Thread-safe registry initialized with all 11 standard specialist managers (`career`, `research`, `engineering`, `hackathon`, `freelance`, `networking`, `tech_intelligence`, `learning`, `personal_ops`, `finance`, `product_ideas`). Implements deliverable verification (`VerifyDeliverable`) and governance gating that rejects unauthorized manager registrations (`ErrUnauthorizedManagerRole`).
- **Agent Factory Service & Immutable Versioning**: [`internal/service/agent_factory.go`](file:///home/adhyan/Desktop/griffin/internal/service/agent_factory.go), [`internal/domain/worker_definition.go`](file:///home/adhyan/Desktop/griffin/internal/domain/worker_definition.go), [`internal/repository/worker_definition_repo.go`](file:///home/adhyan/Desktop/griffin/internal/repository/worker_definition_repo.go) — Specialist worker definitions with append-only versioning (`version + 1`), active state toggling, actor audit logging (`agent_factory_audits`), and dynamic task instantiation (`InstantiateTask`) injecting worker prompts, schemas, tools, and capabilities.
- **Centralized Capability Taxonomy & Approval Policy**: [`internal/domain/capability.go`](file:///home/adhyan/Desktop/griffin/internal/domain/capability.go) — 11 explicit capabilities with deterministic sensitive action classification (`repo:write`, `email:send`, `financial:action`, `job:apply`, `infra:destructive`). Automatically flags approval requirement and halts tasks in `AWAITING_APPROVAL`.
- **Database Migrations (Phase 3)**: [`migrations/000003_orchestration_and_agent_factory.up.sql`](file:///home/adhyan/Desktop/griffin/migrations/000003_orchestration_and_agent_factory.up.sql), [`migrations/000003_orchestration_and_agent_factory.down.sql`](file:///home/adhyan/Desktop/griffin/migrations/000003_orchestration_and_agent_factory.down.sql) — `worker_definitions`, `agent_factory_audits`, `orchestrations`, `orchestration_plans`, `delegation_records` tables with `FORCE ROW LEVEL SECURITY`.
- **REST API Handlers**: [`internal/api/handlers/orchestrator.go`](file:///home/adhyan/Desktop/griffin/internal/api/handlers/orchestrator.go), [`internal/api/handlers/worker_definitions.go`](file:///home/adhyan/Desktop/griffin/internal/api/handlers/worker_definitions.go), [`internal/api/server.go`](file:///home/adhyan/Desktop/griffin/internal/api/server.go) — Exposes `/api/v1/orchestrator/*`, `/api/v1/managers/*`, and `/api/v1/workers/definitions/*`.
- **Phase 3 Documentation**:
  - [`docs/JJ_ORCHESTRATOR.md`](file:///home/adhyan/Desktop/griffin/docs/JJ_ORCHESTRATOR.md)
  - [`docs/MANAGER_FRAMEWORK.md`](file:///home/adhyan/Desktop/griffin/docs/MANAGER_FRAMEWORK.md)
  - [`docs/AGENT_FACTORY.md`](file:///home/adhyan/Desktop/griffin/docs/AGENT_FACTORY.md)
  - [`docs/CAPABILITY_AND_APPROVAL_POLICIES.md`](file:///home/adhyan/Desktop/griffin/docs/CAPABILITY_AND_APPROVAL_POLICIES.md)
  - [`docs/PHASE_3_TEST_REPORT.md`](file:///home/adhyan/Desktop/griffin/docs/PHASE_3_TEST_REPORT.md)

### Phase 2: Antigravity Primary Execution Provider (`internal/provider/antigravity/`)
- **Antigravity Executor**: [`internal/provider/antigravity/executor.go`](file:///home/adhyan/Desktop/griffin/internal/provider/antigravity/executor.go) — Implements `worker.TaskExecutor` contract with bounded concurrency semaphore (1-2 max), pure `argv` slice argument assembly (no shell interpolation), multi-turn conversation continuation via `--conversation <id>`, model routing, schema enforcement (`--json-schema`), process group isolation (`Setpgid: true`), and two-stage graceful termination (`SIGTERM` followed by `SIGKILL` to `-pgid`).
- **Workspace Manager**: [`internal/provider/antigravity/workspace.go`](file:///home/adhyan/Desktop/griffin/internal/provider/antigravity/workspace.go) — Isolated scratch directory provisioning (`<WorkspaceRoot>/task-<id>-<exec>`), 0700/0600 permissions, path traversal rejection (`ErrWorkspacePathTraversal`), input serialization (`task_input.json`), and retention on failure logic.
- **Output Parser & Telemetry**: [`internal/provider/antigravity/parser.go`](file:///home/adhyan/Desktop/griffin/internal/provider/antigravity/parser.go) — Extracts structured JSON, extracts `structured_output` conforming to JSON schemas, captures token usage telemetry (`input_tokens`, `output_tokens`, `total_tokens`), detects banner logs, and maps errors (`ErrAuthenticationRequired`, `ErrRateLimitExceeded`, `ErrInvalidOutput`, `ErrExecutionFailed`).
- **Sentinel Domain Errors**: [`internal/provider/antigravity/errors.go`](file:///home/adhyan/Desktop/griffin/internal/provider/antigravity/errors.go), [`internal/domain/errors.go`](file:///home/adhyan/Desktop/griffin/internal/domain/errors.go) — Typed errors with explicit classification.
- **Worker & Server Integration**: [`cmd/griffin-server/main.go`](file:///home/adhyan/Desktop/griffin/cmd/griffin-server/main.go), [`internal/worker/worker.go`](file:///home/adhyan/Desktop/griffin/internal/worker/worker.go) — Dynamic provider selection based on `cfg.AIProvider` ("antigravity" vs "mock"); worker detects `ErrAuthenticationRequired`, halts retries (`retryable: false`), logs critical operator alert, and records `AUTHENTICATION_REQUIRED` event in PostgreSQL audit ledger.
- **Documentation**:
  - [`docs/ANTIGRAVITY_EXECUTOR.md`](file:///home/adhyan/Desktop/griffin/docs/ANTIGRAVITY_EXECUTOR.md) — Subprocess execution architecture, CLI flags, schema extraction, and conversation continuation.
  - [`docs/ANTIGRAVITY_SECURITY.md`](file:///home/adhyan/Desktop/griffin/docs/ANTIGRAVITY_SECURITY.md) — Security model, scratchpad isolation, path traversal defense, and zero-paid-fallback policy.
  - [`docs/ANTIGRAVITY_INTEGRATION_TESTS.md`](file:///home/adhyan/Desktop/griffin/docs/ANTIGRAVITY_INTEGRATION_TESTS.md) — Test taxonomy, hermetic mock subprocess tests, and live CLI integration probe.

### Phase 1: Go Application Foundation (`cmd/`, `internal/`)
- **Main Server Entrypoint**: [`cmd/griffin-server/main.go`](file:///home/adhyan/Desktop/griffin/cmd/griffin-server/main.go) — Clean startup, graceful shutdown on `SIGINT`/`SIGTERM`, background worker pools and lease reaper supervision.
- **Configuration Management**: [`internal/config/config.go`](file:///home/adhyan/Desktop/griffin/internal/config/config.go) — Environment-driven configuration with validated defaults for AI provider, binary, timeouts, and workspaces.
- **Domain Models & Enums**: [`internal/domain/task.go`](file:///home/adhyan/Desktop/griffin/internal/domain/task.go), [`internal/domain/entities.go`](file:///home/adhyan/Desktop/griffin/internal/domain/entities.go), [`internal/domain/errors.go`](file:///home/adhyan/Desktop/griffin/internal/domain/errors.go) — 11-state task lifecycle, priority tiers, validation, and sentinel domain errors.
- **Database & Migration Layer**: [`internal/database/postgres.go`](file:///home/adhyan/Desktop/griffin/internal/database/postgres.go), [`internal/database/migrations.go`](file:///home/adhyan/Desktop/griffin/internal/database/migrations.go) — Connection pooling via `pgxpool`, transaction helpers, and programmatic migration runner.
- **Repositories**: [`internal/repository/task_repo.go`](file:///home/adhyan/Desktop/griffin/internal/repository/task_repo.go), [`internal/repository/approval_repo.go`](file:///home/adhyan/Desktop/griffin/internal/repository/approval_repo.go), [`internal/repository/agent_repo.go`](file:///home/adhyan/Desktop/griffin/internal/repository/agent_repo.go), [`internal/repository/artifact_repo.go`](file:///home/adhyan/Desktop/griffin/internal/repository/artifact_repo.go).
- **Task Engine Core Service**: [`internal/service/task_engine.go`](file:///home/adhyan/Desktop/griffin/internal/service/task_engine.go) — Atomic task claiming, DAG dependency resolution, cascading cancellation, exponential backoff retries, and approval gates.
- **Worker Execution Foundation**: [`internal/worker/worker.go`](file:///home/adhyan/Desktop/griffin/internal/worker/worker.go), [`internal/worker/executor.go`](file:///home/adhyan/Desktop/griffin/internal/worker/executor.go), [`internal/worker/mock_executor.go`](file:///home/adhyan/Desktop/griffin/internal/worker/mock_executor.go), [`internal/worker/reaper.go`](file:///home/adhyan/Desktop/griffin/internal/worker/reaper.go).
- **HTTP REST API & Middleware**: [`internal/api/server.go`](file:///home/adhyan/Desktop/griffin/internal/api/server.go), [`internal/api/middleware/auth.go`](file:///home/adhyan/Desktop/griffin/internal/api/middleware/auth.go), [`internal/api/middleware/logger.go`](file:///home/adhyan/Desktop/griffin/internal/api/middleware/logger.go), [`internal/api/middleware/recovery.go`](file:///home/adhyan/Desktop/griffin/internal/api/middleware/recovery.go), [`internal/api/middleware/request_id.go`](file:///home/adhyan/Desktop/griffin/internal/api/middleware/request_id.go), and handlers for tasks, approvals, agents, artifacts, and workers.
- **Version-Controlled Migrations**: `migrations/000001_init_schema.*`, `migrations/000002_rls_policies.*`.

### Scale-to-Zero Execution & Runner Architecture (`internal/runner/`)
- **Wakeup Controller**: [`internal/runner/wakeup.go`](file:///home/adhyan/Desktop/griffin/internal/runner/wakeup.go) — Idempotent startup signaling, 30s debouncing cooldown window, stopping-transition backoff retries, and instance state monitoring.
- **Instance Manager**: [`internal/runner/instance.go`](file:///home/adhyan/Desktop/griffin/internal/runner/instance.go) — `InstanceManager` abstraction with `MockInstanceManager` and production `EC2InstanceManager`.
- **Idle Shutdown Monitor**: [`internal/runner/shutdown.go`](file:///home/adhyan/Desktop/griffin/internal/runner/shutdown.go) — Monitors queue via `CountRunnableTasks`, triggers graceful worker draining (`WorkerDrainer`), and performs atomic pre-shutdown verification to neutralize simultaneous task arrival shutdown races.
- **Asynchronous 202 Accepted & Scheduler Ingress**: [`internal/api/handlers/tasks.go`](file:///home/adhyan/Desktop/griffin/internal/api/handlers/tasks.go), [`internal/api/handlers/scheduler.go`](file:///home/adhyan/Desktop/griffin/internal/api/handlers/scheduler.go) — Returns `202 Accepted` with `X-Runner-Status: WAKEUP_SIGNALED` for tasks requiring sleeping runner boot; `/api/v1/scheduler/tick` supports AWS EventBridge Scheduler invocations.
- **Multi-Mode Entrypoint**: [`cmd/griffin-server/main.go`](file:///home/adhyan/Desktop/griffin/cmd/griffin-server/main.go) — Supports `GRIFFIN_MODE=all|api|worker` to cleanly separate serverless API ingress from EC2 worker host.
- **Scale-to-Zero Documentation**:
  - [`docs/SCALE_TO_ZERO_ARCHITECTURE.md`](file:///home/adhyan/Desktop/griffin/docs/SCALE_TO_ZERO_ARCHITECTURE.md) — Architectural blueprint, decoupled ingress, lifecycle, and cost models.
  - [`docs/EC2_WAKEUP_AND_IDLE_SHUTDOWN.md`](file:///home/adhyan/Desktop/griffin/docs/EC2_WAKEUP_AND_IDLE_SHUTDOWN.md) — Wakeup controller, idle shutdown, draining, and IAM policies.
  - [`docs/SERVERLESS_API_DESIGN.md`](file:///home/adhyan/Desktop/griffin/docs/SERVERLESS_API_DESIGN.md) — Serverless API deployment options, connection pooling, and EventBridge Scheduler.
  - [`docs/SCALE_TO_ZERO_FAILURE_MODES.md`](file:///home/adhyan/Desktop/griffin/docs/SCALE_TO_ZERO_FAILURE_MODES.md) — Edge cases, race condition mitigations, and unresolved live risks.

---

## 3. Automated Test Suite Verification Matrix

All tests run against a real Supabase/PostgreSQL 17 database instance:

| Test Name | File | Result | Verified Behaviors |
| :--- | :--- | :--- | :--- |
| `TestTaskStatusTransitions` | `internal/domain/task_test.go` | **PASS** | Validates all 11 lifecycle state transitions, rejects invalid leaps, protects terminal states. |
| `TestTaskValidation` | `internal/domain/task_test.go` | **PASS** | Required field validations, priority defaults, domain normalization. |
| `TestConfigLoadDefaults` | `internal/config/config_test.go` | **PASS** | Default configuration parsing, port checks, concurrency limits, AI provider defaults. |
| `TestConfigOverrideEnv` | `internal/config/config_test.go` | **PASS** | Environment variable overrides for AI provider, workspace roots, timeouts, scale-to-zero. |
| `TestWorkspaceManager_CreateAndCleanup` | `internal/provider/antigravity/workspace_test.go` | **PASS** | Scratchpad creation, 0700 mode, input payload writing, cleanup on success. |
| `TestWorkspaceManager_RetainOnFailure` | `internal/provider/antigravity/workspace_test.go` | **PASS** | Workspaces retained when `retainOnFailure: true` and task fails; purged on success. |
| `TestWorkspaceManager_PathTraversalDefense` | `internal/provider/antigravity/workspace_test.go` | **PASS** | Rejects directory traversal attempts (`../`) outside workspace root with `ErrWorkspacePathTraversal`. |
| `TestParseOutput_SuccessStructured` | `internal/provider/antigravity/parser_test.go` | **PASS** | Structured output JSON parsing, token telemetry mapping, duration capture. |
| `TestParseOutput_BannerLogsBeforeJSON` | `internal/provider/antigravity/parser_test.go` | **PASS** | Strips CLI banner logs and extracts valid JSON payload. |
| `TestParseOutput_AuthFailure` | `internal/provider/antigravity/parser_test.go` | **PASS** | Detects authentication failure keywords across stdout/stderr, maps to `ErrAuthenticationRequired`. |
| `TestParseOutput_RateLimitExceeded` | `internal/provider/antigravity/parser_test.go` | **PASS** | Detects rate limit / quota exhaustion, maps to `ErrRateLimitExceeded`. |
| `TestParseOutput_CLIErrorStatus` | `internal/provider/antigravity/parser_test.go` | **PASS** | Detects `status: "ERROR"`, maps to `ErrExecutionFailed`. |
| `TestParseOutput_MalformedJSON` | `internal/provider/antigravity/parser_test.go` | **PASS** | Non-JSON output mapped to `ErrInvalidOutput`. |
| `TestAntigravityExecutor_Success` | `internal/provider/antigravity/executor_test.go` | **PASS** | End-to-end executor invocation with mock script, healthy status, structured result. |
| `TestAntigravityExecutor_ArgumentPassing` | `internal/provider/antigravity/executor_test.go` | **PASS** | Verifies pure `argv` construction without shell injection; passes conversation, model, schema. |
| `TestAntigravityExecutor_AuthFailure` | `internal/provider/antigravity/executor_test.go` | **PASS** | Subprocess auth failure halts execution, disables retries, marks executor unhealthy. |
| `TestAntigravityExecutor_ProcessTimeoutAndKill` | `internal/provider/antigravity/executor_test.go` | **PASS** | SIGTERM followed by SIGKILL for unresponsive process group; returns `ErrProcessTimeout`. |
| `TestAntigravityExecutor_ProcessCancellation` | `internal/provider/antigravity/executor_test.go` | **PASS** | Context cancellation aborts subprocess group cleanly, returns `ErrProcessCancelled`. |
| `TestAntigravityExecutor_BoundedConcurrency` | `internal/provider/antigravity/executor_test.go` | **PASS** | Semaphore pool enforces strict task serialization (limit 1) with zero concurrency overlap. |
| `TestAntigravityExecutor_BinaryNotFound` | `internal/provider/antigravity/executor_test.go` | **PASS** | Missing binary caught immediately with `ErrBinaryNotFound` without panic. |
| `TestAntigravityLiveIntegration` | `internal/provider/antigravity/executor_test.go` | **PASS** (Opt-in) | Verified live against `/usr/bin/agy` (v0.1.0-alpha) with prompt, JSON schema, and token telemetry (~29.5k tokens). |
| `TestWakeupController_StartStoppedInstance` | `internal/runner/wakeup_test.go` | **PASS** | Starts stopped runner instance upon receiving trigger. |
| `TestWakeupController_NoOpWhenAlreadyRunning`| `internal/runner/wakeup_test.go` | **PASS** | Idempotent no-op when runner is running or pending. |
| `TestWakeupController_DebounceCooldown` | `internal/runner/wakeup_test.go` | **PASS** | Debounces rapid successive triggers within cooldown window. |
| `TestWakeupController_StoppingTransitionRetry`| `internal/runner/wakeup_test.go` | **PASS** | Retries during stopping transition until stopped, then starts. |
| `TestWakeupController_StoppingTimeout` | `internal/runner/wakeup_test.go` | **PASS** | Returns ErrRunnerStopping if transition fails to complete. |
| `TestWakeupController_ConcurrentTriggers` | `internal/runner/wakeup_test.go` | **PASS** | 10 concurrent goroutines trigger exactly 1 StartRunner call. |
| `TestIdleShutdown_CountdownAndStop` | `internal/runner/shutdown_test.go` | **PASS** | ACTIVE -> IDLE -> DRAINING -> STOPPED on idle timeout with 0 in-flight tasks. |
| `TestIdleShutdown_ResetOnActiveTasks` | `internal/runner/shutdown_test.go` | **PASS** | Resets idle countdown to ACTIVE if task arrives before timeout. |
| `TestIdleShutdown_DrainingWaitsForInFlightTasks`| `internal/runner/shutdown_test.go` | **PASS** | Waits for in-flight tasks to drain before initiating stop. |
| `TestIdleShutdown_SimultaneousTaskArrivalDuringDrainingAbortsShutdown`| `internal/runner/shutdown_test.go` | **PASS** | Pre-shutdown recheck detects simultaneous task arrival, aborts shutdown, resumes workers. |
| `TestIdleShutdown_ErrorOnRecheckAbortsShutdownForSafety`| `internal/runner/shutdown_test.go` | **PASS** | Aborts shutdown and resumes workers if database recheck query fails. |
| `TestTaskCreationAndRetrieval` | `tests/task_engine_test.go` | **PASS** | Persistent task creation, default status QUEUED, event logging. |
| `TestTaskIdempotency` | `tests/task_engine_test.go` | **PASS** | Unique idempotency key enforcement per user; duplicate submissions return `ErrIdempotentConflict`. |
| `TestConcurrentWorkerClaims` | `tests/task_engine_test.go` | **PASS** | 10 concurrent workers competing via `SELECT FOR UPDATE SKIP LOCKED`; zero duplicate claims, zero lock contention. |
| `TestHeartbeatAndLeaseRenewal` | `tests/task_engine_test.go` | **PASS** | Extends `lease_expires_at` during execution; rejects heartbeats from unauthorized workers. |
| `TestLeaseExpiryAndReaperRecovery`| `tests/task_engine_test.go` | **PASS** | Background reaper detects expired leases, increments `retry_count`, transitions to `RETRYING`. |
| `TestDependencyBlockingAndUnblocking`| `tests/task_engine_test.go` | **PASS** | Child task blocked in `WAITING_FOR_DEPENDENCY` while parent runs; automatically unblocks to `QUEUED` when parent completes. |
| `TestCancellationCascadesToDependencies`| `tests/task_engine_test.go` | **PASS** | Cancelling parent cascades cancellation to blocked children; rejects cancel on terminal tasks. |
| `TestApprovalCheckpointFlow` | `tests/task_engine_test.go` | **PASS** | Sensitive tasks transition to `AWAITING_APPROVAL`; approval resumes to `QUEUED`; rejection cancels. |
| `TestCountRunnableTasks` | `tests/task_engine_test.go` | **PASS** | Counts queued, running, unblocked child tasks, excludes completed and blocked dependencies. |
| `TestRowLevelSecurityIsolation`| `tests/rls_test.go` | **PASS** | PostgreSQL RLS prevents User B from reading, listing, updating, deleting User A's tasks or forging user_id. |
| `TestWorkerEndToEndExecution`| `tests/worker_test.go` | **PASS** | Worker claims task, runs MockExecutor, records attempt, and completes task end-to-end. |
| `TestWorkerFailureAndRetryAttempt`| `tests/worker_test.go` | **PASS** | Worker captures executor failure, marks attempt FAILED, schedules retry with exponential backoff. |
| `TestWorkerApprovalPause` | `tests/worker_test.go` | **PASS** | Worker intercepts approval requirement and transitions task to `AWAITING_APPROVAL`. |
| `TestWorkerAntigravityAuthFailureEvent` | `tests/worker_test.go` | **PASS** | Worker captures Antigravity auth failure, disables retries, marks FAILED, records `AUTHENTICATION_REQUIRED` event in PostgreSQL audit ledger. |
| `TestWorkerAntigravitySuccess` | `tests/worker_test.go` | **PASS** | Worker executes task via Antigravity provider, persists output payload and attempt telemetry in PostgreSQL. |
| `TestScaleToZero_TaskSubmissionReturns202AndWakesRunner` | `tests/scale_to_zero_test.go` | **PASS** | Task creation returns 202 Accepted, sets X-Runner-Status header, and triggers wake-up. |
| `TestScaleToZero_SchedulerTickEndpoint` | `tests/scale_to_zero_test.go` | **PASS** | POST /api/v1/scheduler/tick evaluates queue state and triggers wake-up if runnable tasks exist. |
| `TestScaleToZero_IdleShutdownAndDrainingWithRealDB` | `tests/scale_to_zero_test.go` | **PASS** | Real database queue draining, idle countdown, and graceful stop execution. |
| `TestScaleToZero_TaskArrivalDuringDrainingAbortsShutdownWithRealDB`| `tests/scale_to_zero_test.go` | **PASS** | Task arriving during idle period aborts shutdown and resumes worker execution with real DB. |
| `TestValidatePlan_ValidLinearDAG` | `internal/domain/orchestration_test.go` | **PASS** | Validates correct 2-node dependency graph, verifies clean validation error slice. |
| `TestValidatePlan_CyclicDependencyDetected` | `internal/domain/orchestration_test.go` | **PASS** | Kahn's algorithm detects cycles (`node-a` <-> `node-b`), returns `Cyclic dependency detected` error. |
| `TestValidatePlan_UnknownManager` | `internal/domain/orchestration_test.go` | **PASS** | Rejects plans referencing unregistered manager domains outside the 11 recognized domains. |
| `TestValidatePlan_SensitiveCapabilityRequiresApproval` | `internal/domain/orchestration_test.go` | **PASS** | Enforces that sensitive capabilities (`repo:write`) without `ApprovalRequired: true` fail validation. |
| `TestWorkerDefinition_Validation` | `internal/domain/orchestration_test.go` | **PASS** | Validates required worker fields (`worker_key`, `name`, `system_prompt`, `manager_id`). |
| `TestDefaultRegistry_ContainsAll11Managers` | `internal/manager/manager_test.go` | **PASS** | Confirms all 11 standard managers are registered on initialization. |
| `TestRegistry_GovernanceRejectsUnauthorizedManager` | `internal/manager/manager_test.go` | **PASS** | Rejects registration of arbitrary rogue manager IDs (`rogue_hacker`) with `ErrUnauthorizedManagerRole`. |
| `TestStandardManager_InterpretAndVerify` | `internal/manager/manager_test.go` | **PASS** | Tests task type classification, priority assignment, and deliverable verification criteria. |
| `TestAgentFactory_CreateAndRetrieveWorkerDefinition` | `tests/agent_factory_test.go` | **PASS** | Persists worker definition, initializes `version = 1`, and retrieves by ID and key. |
| `TestAgentFactory_WorkerVersioningAndImmutability` | `tests/agent_factory_test.go` | **PASS** | Updating a worker definition creates an immutable row with `version = 2`, preserving version 1 intact. |
| `TestAgentFactory_ToggleActive` | `tests/agent_factory_test.go` | **PASS** | Enables and disables worker definitions, logging audit entries for each action. |
| `TestAgentFactory_GovernanceAndActorValidation` | `tests/agent_factory_test.go` | **PASS** | Enforces user tenant boundaries and actor attribution in audit trail. |
| `TestAgentFactory_InstantiateTask` | `tests/agent_factory_test.go` | **PASS** | Generates executable `domain.Task` embedding worker prompt, schemas, tools, and capabilities into input payload. |
| `TestJJ_SubmitRequest_ProducesPlanAndDelegates` | `tests/orchestration_test.go` | **PASS** | End-to-end request ingestion: produces plan, creates root task, and delegates 2 child tasks with dependency ordering. |
| `TestJJ_ReconcileOrchestration_CompletesLifecycle` | `tests/orchestration_test.go` | **PASS** | Worker execution of both child tasks unblocks dependencies; final reconciliation synthesizes deliverables and marks root task `COMPLETED`. |
| `TestJJ_ReconcileOrchestration_ChildFailureAbortsOrchestration` | `tests/orchestration_test.go` | **PASS** | Permanent failure of a child task transitions orchestration to `FAILED` and fails the root task with failure cause. |
| `TestJJ_ApprovalRequirement_PausesOrchestrationAndResumesOnApproval` | `tests/orchestration_test.go` | **PASS** | Sensitive engineering task triggers `AWAITING_APPROVAL`; orchestration pauses; user approval unblocks task; execution finishes `COMPLETED`. |
| `TestJJ_ReconcileInFlight_ProcessRestartRecovery` | `tests/orchestration_test.go` | **PASS** | Simulates server crash with active orchestrations; `ReconcileInFlight` detects in-flight work and completes finished orchestrations without human intervention. |
| `TestJJ_InvalidPlanRejection` | `tests/orchestration_test.go` | **PASS** | Submitting a cyclic request returns plan rejection and does not delegate child tasks. |
| `TestJJ_ConcurrentOrchestrations` | `tests/orchestration_test.go` | **PASS** | 8 concurrent goroutines submit separate requests simultaneously; all 8 successfully plan, delegate, and maintain tenant isolation without lock contention. |
| `TestAPIEndpoints` | `tests/api_test.go` | **PASS** | Health/readiness, Supabase JWT auth, worker API key auth, task CRUD, approvals, artifacts, agents, JJ Orchestrator, Manager registry, and Worker definitions. |
| `TestGracefulShutdown` | `tests/lifecycle_test.go` | **PASS** | HTTP server drains active connections and shuts down cleanly within timeout window. |
| `TestDatabaseTransactionRollbackOnFailure`| `tests/lifecycle_test.go` | **PASS** | Mid-transaction errors abort cleanly; rolled back mutations do not persist. |

---

## 4. Staged Cloud Runner Assets & Live Readiness Gates

- Specifications: [`docs/SCALE_TO_ZERO_ARCHITECTURE.md`](file:///home/adhyan/Desktop/griffin/docs/SCALE_TO_ZERO_ARCHITECTURE.md), [`docs/EC2_WAKEUP_AND_IDLE_SHUTDOWN.md`](file:///home/adhyan/Desktop/griffin/docs/EC2_WAKEUP_AND_IDLE_SHUTDOWN.md), [`docs/SERVERLESS_API_DESIGN.md`](file:///home/adhyan/Desktop/griffin/docs/SERVERLESS_API_DESIGN.md), [`docs/SCALE_TO_ZERO_FAILURE_MODES.md`](file:///home/adhyan/Desktop/griffin/docs/SCALE_TO_ZERO_FAILURE_MODES.md).
- Automated Provisioning: [`infra/provision-ec2.sh`](file:///home/adhyan/Desktop/griffin/infra/provision-ec2.sh), [`infra/user-data.sh`](file:///home/adhyan/Desktop/griffin/infra/user-data.sh).
- **Readiness Gate**: Scale-to-zero is architected, tested in code, and staged. Per engineering policy, it will NOT be claimed as "production-ready" until multi-day live testing on AWS EC2 validates real keyring unlock and live EventBridge triggers.


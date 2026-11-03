# Incident 2026-11-03: orchestrated tasks stall while the runner sleeps

## Timeline

- A user submitted one objective via `POST /api/v1/orchestrator/requests`.
- JJ created the root task (`RUNNING`, `leased_by = JJ_ORCHESTRATOR`, `max_retries 0`) and two research
  children: child 1 `QUEUED`, child 2 `WAITING_FOR_DEPENDENCY`.
- The EC2 runner was asleep (scale-to-zero). Nothing woke it.
- About 8 minutes later the runner was started manually and the queue drained.
- The UI showed "Worker: Not assigned yet" for the root, because the root is JJ's coordination
  record and is never claimed by a worker.

## Root cause

1. `OrchestratorHandler.SubmitRequest` never called `RunnerWakeup.TriggerWakeup`. Only
   `POST /tasks`, approvals, the scheduler tick and the manual wake endpoint did.
2. No scheduler tick (`POST /api/v1/scheduler/tick`, EventBridge) was running to compensate.
3. Orchestrated children had `max_retries 0`, so a single failed attempt was terminal.

## Fix (branch `fix/orchestrator-wake-and-retries`)

- `SubmitRequest` now wakes the runner (bounded 8s, synchronous, best-effort) when scale-to-zero is
  enabled and the plan created at least one `QUEUED` child. A wake failure is logged (`orchestrator:
  runner wake failed`) and does not fail the request. Rejected/failed plans never wake. The 30s
  cooldown in `internal/runner/wakeup.go` de-duplicates rapid submissions.
- Orchestrated children use `domain.DefaultMaxRetries` (3), the same constant as `POST /tasks`.
  The root stays at 0 (never executed by a worker).
- Cancelling a task now cancels its non-terminal children (parent link); cancelling the JJ root marks
  the orchestration `CANCELLED`.
- `GET /orchestrator/requests/{id}` reconciles child state, but no longer rewrites the orchestration
  row when the status is unchanged (previously every poll bumped `updated_at`). It still writes on
  real state transitions (derived-state sync). All other PWA GET routes are verified read-only by
  `tests/get_readonly_test.go`.
- Verified, no change needed: the root cannot be claimed (`ClaimNextTask` only takes `QUEUED`/`RETRYING`)
  nor reaped (`lease_expires_at` is NULL); it fails/completes via `ReconcileOrchestration`.

## Recovery for a stuck task

Do not edit database rows.

1. Wake the runner: `POST /api/v1/runner/wake` or the PWA wake pill. Queued children are then claimed.
2. Or cancel the work: `POST /api/v1/tasks/{root_id}/cancel`. With this fix the children are
   cancelled too (for orchestrations created before the fix, cancel the children individually).
3. Recommended follow-up (infra, not in this change): confirm the EventBridge schedule that calls
   `POST /api/v1/scheduler/tick` exists, so a failed wake is retried automatically.

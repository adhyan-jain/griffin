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

---

# Follow-up 2026-11-04: false completion (empty result) and orchestration gaps

Branch `fix/false-completion`.

## What was observed (JJ orchestration, from read-only evidence)

- Root task `RUNNING`, `leased_by = JJ_ORCHESTRATOR`; step 1 "Gather primary sources" and step 2
  "Synthesize markdown report and evidence table" (depends on step 1).
- Step 1 ran on `griffin-worker-1` and was `COMPLETED`. Its `output_payload` was
  `{"response":"... saved to the task artifact: ai_agent_ecosystem_summary.md ..."}` (1039 bytes);
  the `artifacts` table had 0 rows.
- Step 2 transitions: created -> unblocked to `QUEUED` -> claimed by `griffin-worker-1` ->
  "Task execution completed successfully"; attempt #1 `COMPLETED` (13.9 s, ~7k tokens), but
  `tasks.output_payload = {}` (2 bytes). A model run that spent tokens persisted no result and was
  marked `COMPLETED`.
- The root task and orchestration stayed `RUNNING`; `delegation_records` stayed `RUNNING` /
  `WAITING_FOR_DEPENDENCY` after both children were `COMPLETED`.
- The EC2 runner never powered off (6h+ up, expected about 5 min).

## Root causes

a. `antigravity.ParseOutput` accepted exit 0 + valid JSON with an empty `response` and no
   `structured_output`. The executor left the payload nil but returned `SUCCESS`; `worker.go` called
   `CompleteTask` without checking the output, and the repository substituted `{}` for nil.
b. Nothing passed a dependency's output to the dependent task: step 2's prompt was only its own goal.
c. The agy workspace is deleted after a successful run and nothing turns agy-written files into
   artifacts, so step 1's "saved to the task artifact" file was lost (see Unfixed gap).
d. `ReconcileOrchestration` only ran from `GET /orchestrator/requests/{id}` and at startup, so parents
   were not closed when children finished; `delegation_records.status` is only refreshed there.
e. Attempt recording in `worker.go` was guarded by `taskRepo != nil` and `RecordAttempt` errors were
   discarded.
f. `CountRunnableTasks` counted every `RUNNING` row, including the JJ root (never run by a worker),
   so an unreconciled root kept the idle monitor and the scheduler tick seeing "work" forever; this
   is why the runner never shut down.

## What is fixed

1. No false success: the Antigravity executor returns `ErrEmptyResult` ("agy exited 0 but returned no
   result", retryable); the worker treats nil/`{}`/`null`/`""` output as a retryable failure (bounded
   by the retry budget, attempt recorded `FAILED`); `TaskEngine.CompleteTask` rejects an empty output
   with a validation error for every caller except `JJ_ORCHESTRATOR` (so
   `POST /api/v1/workers/{id}/complete` with an empty body now returns 400).
2. Dependency output flow: `GetDependencyOutputs` returns `COMPLETED` prerequisites; the worker adds a
   "Results from earlier steps" section (cap 20 KB, truncation marker) to the prompt for that run only.
   The stored goal and input payload are not modified.
3. `RecordAttempt` failures are logged; a worker built without a `TaskRepo` logs a warning.
4. `Worker.OnTaskFinished` is wired to `JJOrchestrator.ReconcileByChildTask`, which finds the
   orchestration through `delegation_records.child_task_id` and runs the existing (idempotent)
   `ReconcileOrchestration`, closing the root and refreshing delegation records without a GET.
5. `CountRunnableTasks` no longer counts `RUNNING` rows leased by `JJ_ORCHESTRATOR`; their children
   count on their own while they have work, so the idle monitor and scheduler tick can power the
   runner down.

## Unfixed gap: artifact persistence of agy-written files

Files an agent writes in its workspace (for example `ai_agent_ecosystem_summary.md`) are lost when
`WorkspaceManager.CleanupWorkspace` runs after a successful run; the `artifacts` table gets no rows.
This change deliberately does not implement it. Design note (needs an approved design): before
cleanup, enumerate files in the task workspace (size/count/type limits, no symlinks, path-traversal
checks), upload them through the artifacts service (`StorageService`) as `artifacts` rows owned by the
task's user, reference the artifact IDs in the task output, and only then delete the workspace. Until
then, a model that says "saved to the task artifact" has not produced a retrievable artifact.

## Other notes

- Once an orchestration is terminal (for example failed because one child failed), later changes of
  sibling delegation records are not synced, because `ReconcileOrchestration` returns early.


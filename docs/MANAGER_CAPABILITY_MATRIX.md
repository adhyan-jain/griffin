# Manager Capability Matrix (R4 audit)

Scope: the 11 managers in `internal/manager/registry.go`. Evidence is code plus tests run on this branch:
`go test ./internal/...` (all ok) and `GRIFFIN_TEST_DB_URL=... go test ./tests/...` against a throwaway Postgres 16 (ok, 48s).
New proving tests: `internal/service/planner_matrix_test.go`
(`TestManagerMatrix_EveryManagerRoutesAndYieldsValidPlan`, `TestManagerMatrix_ApprovalOperationsAreGated`, `TestManagerMatrix_WorkerKeysUniqueAndUnseeded`).

## Common pipeline (identical for all 11)

| Stage | Evidence |
|---|---|
| Registered | All 11 built by `NewDefaultRegistry` as `StandardManager` (`registry.go:20-27`); `TestRegistry_Has11StandardManagers` (`internal/manager/manager_test.go:11`). |
| Accepts tasks | `POST /api/v1/orchestrator/requests` -> `JJOrchestrator.SubmitRequest` (`handlers/orchestrator.go:39,64`; `orchestrator.go:62`). `StandardManager.Interpret` (`manager.go:46`) is **never called in production** (only in tests); routing is done solely by the planner. Direct `POST /tasks` only stores `manager_domain` as a label (`handlers/tasks.go:76`); it does no manager logic. |
| Planner in prod | `main.go:151` passes `nil` planner, so `RuleBasedPlanner` is used (`orchestrator.go:49-51`). `AIPlanner` is only wired in the opt-in live test (`tests/orchestration_test.go:479`). |
| Routing | `classifyIntent` (`planner.go:271-294`): first-match keyword switch, default `research`. |
| Delegation | `delegatePlan` (`orchestrator.go:339-440`): look up `WorkerKey` in `worker_definitions` for that user; if found + active -> `InstantiateTask` (embeds `_system_prompt`, `_capabilities`, `_output_schema`; `agent_factory.go:262-270`); **otherwise silently falls back to a plain task** with no system prompt (`orchestrator.go:371-384`). |
| Execution | Generic worker -> `TaskExecutor`; Antigravity uses `task.Goal`/payload as prompt (`antigravity/executor.go:168-177`). No manager-specific code in `internal/worker` or `internal/provider/antigravity`. `MockExecutor` is the default when `AI_PROVIDER` is not `antigravity` (`main.go:213-215`). |
| Result persistence | `engine.CompleteTask(output)` (`worker/worker.go:207`) -> `tasks.output_payload`; `ReconcileOrchestration` copies to delegation summary and `FinalResponse` (`orchestrator.go:197-262`). Test: `TestWorkerEndToEndExecution`, `TestJJ_ReconcileOrchestration_CompletesLifecycle`. |
| Failure reporting | `engine.FailTask` (`worker.go:176`); child failure fails orchestration + root task (`orchestrator.go:247-255`). Tests: `TestWorkerFailureAndRetryAttempt`, `TestJJ_ReconcileOrchestration_ChildFailureAbortsOrchestration`, `TestWorkerAntigravityAuthFailureEvent`. |
| Approvals | Planner marks child `ApprovalRequired`; `delegatePlan` calls `RequestApproval` (`orchestrator.go:402-418`); worker can also pause (`worker.go:200`). Tests: `TestJJ_ApprovalRequirement_PausesOrchestrationAndResumesOnApproval`, `TestWorkerApprovalPause`, `TestApprovalCheckpointFlow`. |
| Deliverable verification | `StandardManager.VerifyDeliverable` is only "non-empty valid JSON, status != error" (`manager.go:105-125`) and its result only **logs a warning** (`orchestrator.go:218-224`); it never fails anything. `VerificationRules` are metadata only. |

## Per-manager verdicts

Integration (Postgres) tests only exercise `research` (orchestration lifecycle) and `engineering` (approval) end to end, using `MockExecutor`/fake output; there is no test that any manager produces a real domain deliverable.

| Manager | Planner keywords (first match wins) | Plan shape | Worker keys planned (AvailableWorkers) | Approval in plan | Verdict |
|---|---|---|---|---|---|
| career | job, resume, interview, recruiter, career | bespoke 2-step (`planner.go:84-121`) | career_researcher, resume_tailorer (application_drafter never planned) | only if text has "send"/"apply"; otherwise none even though manager allows job:apply/email:send | GENERIC-VIA-AGY (bespoke decomposition, no integration test) |
| research | default fallback (anything unmatched) | bespoke 2-step (`planner.go:123-152`) | web_researcher, synthesis_writer (citation_auditor never planned) | none | REAL at orchestration level (`TestJJ_SubmitRequest_ProducesPlanAndDelegates`, `TestJJ_ReconcileOrchestration_CompletesLifecycle`, failure + restart tests); execution generic |
| engineering | code, bug, refactor, test, git, repo | bespoke 2-step (`planner.go:48-83`) | bug_diagnostician, code_reviewer (test_engineer never planned; note the write step uses `code_reviewer`) | always on write step (repo:modify, medium) | REAL at orchestration level (approval pause/resume test, `tests/orchestration_test.go:226`); execution generic |
| hackathon | hackathon, bounty, grant | single step | hackathon_scout only | none | GENERIC-VIA-AGY |
| freelance | freelance, gig, proposal, contract | single step | proposal_writer only | yes (sensitive caps) | GENERIC-VIA-AGY |
| networking | networking, crm, outreach, meeting | single step | contact_researcher only | yes | GENERIC-VIA-AGY |
| tech_intelligence | news, ai release, model, benchmark | single step | ecosystem_monitor only | none | GENERIC-VIA-AGY |
| learning | learn, study, curriculum, quiz | single step | curriculum_architect only | none | GENERIC-VIA-AGY |
| personal_ops | calendar, agenda, schedule, travel | single step | schedule_coordinator only | yes | GENERIC-VIA-AGY |
| finance | credit, aws bill, finance, subscription | single step | cloud_cost_auditor only | yes | GENERIC-VIA-AGY |
| product_ideas | prd, feature, idea, product | single step | product_architect only | none | GENERIC-VIA-AGY |

No manager is PLACEHOLDER in the sense of being unreachable: every one routes, produces a plan that passes `domain.ValidatePlan`, and delegates (`TestManagerMatrix_EveryManagerRoutesAndYieldsValidPlan`, 11 subtests). None has manager-specific execution logic; `MockExecutor` is the only non-AGY executor and is dev-only.

## Worker definitions: where they live

Declared worker keys: 25, unique across managers (`TestManagerMatrix_WorkerKeysUniqueAndUnseeded`). They exist **only as strings in Go** (`registry.go`). The `worker_definitions` table is created by `migrations/000003_orchestration_and_agent_factory.up.sql:7`, and **no migration or startup code inserts any row** (test scans all migrations). Rows are created only by `POST` to the worker-definitions API (`handlers/worker_definitions.go:119`) per user. Consequence: on a fresh account every delegation takes the plain-task fallback and no specialist system prompt is ever applied.

## Gaps found

1. **AvailableWorkers have no seeded definitions** (25 of 25). Specialist prompts, schemas and capability lists are never used unless the user hand-creates definitions; the fallback is silent (no log/metric at `orchestrator.go:371`).
2. **Dead workers**: default path always uses `AvailableWorkers[0]` (`planner.go:158-161`). Never planned: application_drafter, citation_auditor, test_engineer, pitch_drafter, gig_scout, outreach_drafter, briefing_synthesizer, quiz_master, itinerary_planner, benefit_scout, domain_modeler.
3. **Approval inconsistency in career**: manager lists `job:apply`/`email:send` as approval ops, but the plan only gates on the substring "send"/"apply" (`planner.go:85`), and `career_tailor` carries only `email:draft`.
4. **Engineering write step uses `code_reviewer`** instead of an implementing worker (`planner.go:73`).
5. **`StandardManager.Interpret` and `VerifyDeliverable` are effectively dead/advisory**: Interpret has no production caller; verification failure only logs.
6. **AIPlanner not wired in production** (`main.go:151` nil planner).
7. **Keyword router is brittle**: first-match substrings ("test" in "latest", "model" in any ML request routes to tech_intelligence; "contract" to freelance); unmatched requests silently go to research.
8. `ValidatePlan` assigns `ct.ManagerDomain = plan.ManagerID` on a loop copy (`orchestration.go:175-177`), a no-op; harmless today because planners always set it.
9. `InstantiateTask` does not copy `WorkspaceID` (`agent_factory.go:272-282`), unlike the plain-task fallback.

No production bug was changed; items above are design gaps reported for triage.

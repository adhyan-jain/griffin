# Griffin Antigravity AI Execution Provider

## 1. Executive Summary

Griffin designates Google Antigravity CLI (`agy`) as its **primary cloud AI execution provider**. Tasks assigned to orchestrator **JJ** or specialist workers are driven by headless invocations of the authenticated `agy` CLI installed on persistent compute (local development host or dedicated AWS EC2 Ubuntu instance).

This document details the architectural design, subprocess management, argument construction, structured JSON streaming/schema parsing, and process lifecycle controls implemented in [`internal/provider/antigravity/`](file:///home/adhyan/Desktop/griffin/internal/provider/antigravity/).

---

## 2. Architecture & Provider-Neutral Contract

The Antigravity integration implements Griffin's provider-neutral [`worker.TaskExecutor`](file:///home/adhyan/Desktop/griffin/internal/worker/executor.go) interface:

```go
type TaskExecutor interface {
    Execute(ctx context.Context, task *domain.Task) (*ExecutionResult, error)
    Name() string
    IsHealthy(ctx context.Context) (bool, error)
}
```

This abstraction ensures that Griffin's core task engine, PostgreSQL RLS, worker lease management, and REST APIs remain completely agnostic of underlying AI providers, allowing alternative execution providers (such as the deterministic `MockExecutor` or future Claude Cowork runners) to be plugged in without engine modifications.

```
+-----------------------------------------------------------------------+
|                         Griffin Task Engine                           |
|            (Atomic Leases, RLS Isolation, DAG Dependencies)           |
+-----------------------------------+-----------------------------------+
                                    |
                                    v
+-----------------------------------------------------------------------+
|                     Worker Execution Loop (worker.go)                 |
|             (Heartbeats, Attempt Tracking, Retries, Approvals)        |
+-----------------------------------+-----------------------------------+
                                    |
                                    v
+-----------------------------------------------------------------------+
|                    AntigravityExecutor (executor.go)                   |
|  - Bounded Semaphore (1-2 Subprocesses Max)                           |
|  - Workspace Isolation (/tmp/griffin-workspaces/task-<id>-<exec>)     |
|  - Process Group Termination (Setpgid, SIGTERM -> SIGKILL)            |
|  - Argument Construction (No shell interpolation)                     |
+------------------+---------------------------------+------------------+
                   |                                 |
                   v                                 v
+-----------------------------------+  +--------------------------------+
|     Google Antigravity CLI        |  |     Output Parser & Validator  |
|         (/usr/bin/agy)            |  |  - Auth failure detection      |
|  - Authenticated via subscription |  |  - JSON schema extraction      |
|  - Headless JSON output stream    |  |  - Token telemetry metrics     |
+-----------------------------------+  +--------------------------------+
```

---

## 3. Empirically Validated CLI Flags & Argument Construction

All command executions are launched via direct slice arguments using `os/exec.Command` with an isolated environment. **Shell interpolation (`sh -c` / `bash -c`) is strictly prohibited** to prevent argument injection vulnerabilities.

Flags supported and empirically validated against installed `agy` (v0.1.0-alpha):

| Flag | Purpose | Example Value |
| :--- | :--- | :--- |
| `-p`, `--print` | Headless non-interactive prompt execution | `"Summarize quarterly earnings report"` |
| `--output-format json` | Forces machine-readable JSON stdout output | `json` |
| `--disable-slash-commands` | Disables interactive slash commands (`/goal`, `/plan`, etc.) | *Boolean flag* |
| `--conversation <id>` | Continues an existing multi-turn conversation context | `c1f7a0de-fbb0-4d45-be02-401738739958` |
| `--model <model>` | Specifies target Gemini foundation model | `gemini-2.5-pro` |
| `--json-schema <json>` | Enforces strict schema conformance on model response | `{"type":"object","properties":{...}}` |
| `--print-timeout <dur>` | Per-turn generation timeout within the CLI | `5m0s` |

### Argument Assembly Algorithm
1. Prompt extraction: `task.InputPayload.prompt` if non-empty, otherwise falls back to `task.Goal`.
2. Multi-turn continuation: `task.InputPayload.conversation_id` is supplied to `--conversation <id>`.
3. Schema enforcement: If `task.InputPayload.json_schema` is present, it is formatted as a single JSON argument to `--json-schema`.
4. Model routing: If `task.InputPayload.model` is present, it is forwarded to `--model`; otherwise falls back to `DefaultModel`.

---

## 4. Subprocess Lifecycle & Process Group Termination

Because the Antigravity CLI can spawn subagents, sidecar processes, or tool execution children (e.g. compilers, shell scripts, Python runners), terminating only the parent process PID risks leaving orphan zombie processes consuming CPU and memory on the host.

### Process Group Isolation (`Setpgid: true`)
The executor starts the child process with a dedicated process group:
```go
cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
```

### Two-Stage Graceful Termination
When a task times out (`ExecutionTimeout`) or the execution context is cancelled by a lease expiration or user cancellation:
1. **Stage 1 (SIGTERM)**: A negative process group kill signal `syscall.Kill(-pgid, syscall.SIGTERM)` is broadcast to all processes in the process group.
2. **Stage 2 (SIGKILL Fallback)**: The executor waits up to `GracefulTermination` (default: 5 seconds). If any processes remain active, it enforces hard termination via `syscall.Kill(-pgid, syscall.SIGKILL)`.
3. **Reap**: `cmd.Wait()` is awaited to ensure process table entries are cleanly reaped and zombie processes are prevented.

---

## 5. Bounded Concurrency & Resource Safeguards

To prevent Out-Of-Memory (OOM) kills on resource-constrained compute instances (such as AWS EC2 `t4g.small` or `t4g.medium`) and avoid Antigravity rate limits:
- The executor maintains an internal bounded semaphore channel: `sem chan struct{}`.
- Concurrency is strictly bounded between **1 and 2** concurrent subprocesses (default: `1`, maximum: `2`).
- Tasks attempting to execute concurrently are queued on the channel and await an available slot.
- Worker heartbeat goroutines keep database leases active while tasks are being processed.

---

## 6. Output Parsing & Error Classification

Output produced on `stdout` and `stderr` is processed by [`internal/provider/antigravity/parser.go`](file:///home/adhyan/Desktop/griffin/internal/provider/antigravity/parser.go):

### Output Structure
Successful invocations emit a JSON document:
```json
{
  "conversation_id": "c1f7a0de-fbb0-4d45-be02-401738739958",
  "status": "SUCCESS",
  "response": "Detailed text response...",
  "duration_seconds": 4.25,
  "num_turns": 1,
  "structured_output": {
    "key": "value"
  },
  "usage": {
    "input_tokens": 120,
    "output_tokens": 45,
    "total_tokens": 165
  }
}
```

### Error Taxonomy
| Condition | Trigger | Domain Error | Retryable |
| :--- | :--- | :--- | :--- |
| **Authentication Expired** | "authentication required", "unauthenticated", "token expired" in stderr/stdout | `ErrAuthenticationRequired` | **No** (halts execution, alerts operator) |
| **Rate Limit / Quota** | "rate limit", "quota exceeded", "resource exhausted" | `ErrRateLimitExceeded` | **Yes** (exponential backoff) |
| **Execution Timeout** | Context deadline exceeded | `ErrProcessTimeout` | **Yes** |
| **Parent Cancelled** | Task cancelled by operator / lease expired | `ErrProcessCancelled` | **No** |
| **Malformed Output** | Non-JSON output emitted with exit code 0 | `ErrInvalidOutput` | **No** |
| **CLI Failure** | Non-zero exit code or `status: "ERROR"` | `ErrExecutionFailed` | **No** |

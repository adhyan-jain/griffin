# Griffin Antigravity Integration Testing Strategy

## 1. Overview

Testing AI CLI subprocess interactions requires a rigorous two-tier methodology to ensure both hermetic CI testability and production-grade fidelity against the actual binary:

1. **Controlled Subprocess Mock Tests**: Hermetic, sub-second unit and subprocess tests simulating all CLI exit codes, stdout/stderr patterns, signal traps, and concurrency limits without external network access or token consumption.
2. **Live Integration Tests**: End-to-end tests executing actual prompts against the authenticated `/usr/bin/agy` CLI, validating real-world schema enforcement, token counters, and conversation continuation.
3. **Database Audit & Worker Integration Tests**: End-to-end database tests validating PostgreSQL lease renewal, attempt recording, and audit event logging during Antigravity task runs.

---

## 2. Controlled Subprocess Mock Tests

Located in [`internal/provider/antigravity/executor_test.go`](file:///home/adhyan/Desktop/griffin/internal/provider/antigravity/executor_test.go):

| Test Case | Scenario Simulated | Verification Criterion |
| :--- | :--- | :--- |
| `TestAntigravityExecutor_Success` | Successful execution emitting structured JSON | Validates `Status: "SUCCESS"`, structured output extraction, token usage metrics, duration. |
| `TestAntigravityExecutor_ArgumentPassing` | Arbitrary prompt strings with quotes and metacharacters | Confirms pure `argv` tokenization without shell injection or corruption. |
| `TestAntigravityExecutor_AuthFailure` | Stderr containing `"error: authentication required"` | Validates `ErrAuthenticationRequired` returned, `Retryable: false`, executor marked unhealthy. |
| `TestAntigravityExecutor_ProcessTimeoutAndKill` | Subprocess trapping `SIGTERM` and sleeping | Confirms `SIGTERM` followed by `SIGKILL` after 100ms grace period; process killed and reaped. |
| `TestAntigravityExecutor_ProcessCancellation` | Context cancelled while subprocess is running | Confirms `ErrProcessCancelled`, `Status: "CANCELLED"`, clean process teardown. |
| `TestAntigravityExecutor_BoundedConcurrency` | 2 concurrent tasks on semaphore pool with limit 1 | Proves execution serialization (START 1 -> END 1 -> START 2 -> END 2) with zero overlap. |
| `TestAntigravityExecutor_BinaryNotFound` | Execution pointing to non-existent binary path | Returns `ErrBinaryNotFound` without crashing; flags health check. |

---

## 3. Live Integration Probe

Located in [`internal/provider/antigravity/executor_test.go`](file:///home/adhyan/Desktop/griffin/internal/provider/antigravity/executor_test.go):

```go
func TestAntigravityLiveIntegration(t *testing.T) {
    if os.Getenv("GRIFFIN_ENABLE_LIVE_AGY_TEST") != "true" {
        t.Skip("Skipping live Antigravity CLI integration test (set GRIFFIN_ENABLE_LIVE_AGY_TEST=true to enable)")
    }
    // Invokes /usr/bin/agy with prompt, --output-format json, and --json-schema
}
```

### Running the Live Integration Test
```bash
GRIFFIN_ENABLE_LIVE_AGY_TEST=true go test -v -run TestAntigravityLiveIntegration ./internal/provider/antigravity/...
```

### Verified Live Output Metrics
- Execution duration: ~21.8 seconds.
- Total tokens reported: 29,533 tokens.
- Strict schema extraction: JSON schema verified against returned structured output.
- Non-zero conversation ID assigned and validated.

---

## 4. Worker & Database Audit Integration Tests

Located in [`tests/worker_test.go`](file:///home/adhyan/Desktop/griffin/tests/worker_test.go):

| Test Case | System Under Test | Verification Criterion |
| :--- | :--- | :--- |
| `TestWorkerAntigravitySuccess` | `Worker` + `TaskEngine` + `AntigravityExecutor` + PostgreSQL | Verifies task transitions from `QUEUED` to `RUNNING` to `COMPLETED`; output payload persisted in PostgreSQL; attempt record populated with token counts. |
| `TestWorkerAntigravityAuthFailureEvent` | `Worker` + `TaskEngine` + `AntigravityExecutor` + PostgreSQL | Verifies auth failure transitions task directly to `FAILED` with `retry_count = 0` (no automated retries); verifies `AUTHENTICATION_REQUIRED` event recorded in `task_events` audit table. |

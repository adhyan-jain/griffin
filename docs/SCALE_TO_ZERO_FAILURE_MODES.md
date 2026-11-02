# Griffin Scale-to-Zero Failure Modes & Risk Analysis

## 1. Overview

Scale-to-zero architectures introduce unique distributed systems failure modes, race conditions, and operational risks. This document analyzes every identified failure mode, Griffin's implemented defenses, and the unresolved risks that must be tested on live AWS infrastructure before declaring production readiness.

---

## 2. Failure Modes & Implemented Mitigations

### 1. Simultaneous Task Arrival During Idle Shutdown (Shutdown Race)
- **Failure Scenario**: The idle timer expires, workers drain, and the monitor prepares to issue the shutdown command. At the exact same millisecond, a user submits a high-priority task via the serverless API. If the host powers off, the task languishes in `QUEUED` state until an external trigger occurs.
- **Griffin Defense**: [`IdleShutdownMonitor`](file:///home/adhyan/Desktop/griffin/internal/runner/shutdown.go) enforces a **two-phase pre-shutdown check**. After active executions drain to 0, an immediate atomic query against PostgreSQL (`CountRunnableTasks`) re-evaluates the queue. If `count > 0`, shutdown is immediately aborted, `WorkerPool.Resume()` is called, and the runner reverts to `ACTIVE` state. Verified in automated test `TestScaleToZero_TaskArrivalDuringDrainingAbortsShutdownWithRealDB`.

### 2. Burst Task Submissions Causing Duplicate EC2 Launch Calls
- **Failure Scenario**: A batch job inserts 50 tasks in 1 second. 50 parallel API invocations attempt to call `ec2:StartInstances`, causing AWS API throttling errors (`RequestLimitExceeded`).
- **Griffin Defense**: [`WakeupController`](file:///home/adhyan/Desktop/griffin/internal/runner/wakeup.go) protects the AWS API via a synchronized mutex and a configurable `CooldownWindow` (default: 30 seconds). Only the first call queries instance state and triggers boot; all subsequent concurrent calls within the window are safely debounced as idempotent no-ops. Verified in automated test `TestWakeupController_ConcurrentTriggers`.

### 3. Startup Call Rejected Due to In-Progress Stopping Transition
- **Failure Scenario**: The runner was idle and initiated shutdown (state: `stopping`). A user submits a task while the instance is transitioning from `stopping` to `stopped`. Calling `ec2:StartInstances` immediately results in AWS error `InvalidInstanceState`.
- **Griffin Defense**: The wake-up controller detects `RunnerStateStopping` and initiates an automated backoff retry loop (`MaxStoppingRetries: 5`, delay: 2s) awaiting the instance to reach `stopped` before issuing `StartInstances`. Verified in automated test `TestWakeupController_StoppingTransitionRetry`.

### 4. Worker Crash, OOM Kill, or Abrupt Host Poweroff Mid-Task
- **Failure Scenario**: A worker crashes mid-execution due to host poweroff or kernel panic. The task remains in `RUNNING` status with an unreleased database lease.
- **Griffin Defense**: Every task claimed by a worker has an expiring lease (`lease_expires_at`). Griffin's [`LeaseReaper`](file:///home/adhyan/Desktop/griffin/internal/worker/reaper.go) runs continuously whenever the runner is active. Upon instance boot, the reaper detects expired leases (`lease_expires_at <= NOW()`), increments `retry_count`, logs a recovery event, and transitions the task to `RETRYING` with exponential backoff.

### 5. Antigravity Keyring Lockout on Headless Reboot
- **Failure Scenario**: The Linux system restarts. If the Antigravity CLI relies on a GNOME desktop keyring daemon that requires an active graphical login session, headless invocations fail with missing credentials.
- **Griffin Defense**:
  - Credentials reside on persistent KMS-encrypted EBS storage (`/home/griffin/.gemini/`).
  - Systemd user lingering is enabled (`loginctl enable-linger griffin`), ensuring systemd user services and user DBus sessions initialize at boot without interactive login.
  - Headless keyring unlock is staged in systemd service definitions using `dbus-run-session`.
  - If authentication expires, the worker halts with `ErrAuthenticationRequired` without infinite retries or silent paid fallback.

---

## 3. Unresolved Risks & Testing Requirements

> [!CAUTION]
> **Scale-to-zero is architected, implemented in Go, and verified against simulated subprocesses and real PostgreSQL databases, but IT IS NOT YET PRODUCTION-READY for live deployment.**
> 
> Production readiness requires completing live validation on AWS EC2 once credentials and launch approvals are provided.

### Unresolved Operational Risks

| Risk Area | Description | Required Verification Test |
| :--- | :--- | :--- |
| **Boot Cold-Start Latency** | Graviton2 `t4g.medium` instance boot typically takes 30–45 seconds. User expectations must tolerate async execution. | Measure wall-clock elapsed time from `202 Accepted` to worker task claiming on live EC2. |
| **AWS API Rate Limits** | Continuous EventBridge polling could trigger AWS service quotas if misconfigured. | Verify EventBridge tick frequency (5m rate consumes <0.1% of quota). |
| **OAuth Token Refresh Longevity** | Antigravity subscription OAuth tokens expire periodically (typically 14–30 days). | Run a continuous 6-hour background canary probe to verify automatic token refreshing without human presence. |
| **Keyring Persistence Across Reboots** | Unattended DBus keyring unlocking under headless Ubuntu Linux must be validated against real `agy` auth storage. | Execute 5 consecutive `stop -> start -> agy -p "ping"` cycles on live EC2. |
| **Filesystem Mount Ordering** | Dedicated `/var/griffin/workspaces` EBS mount must be available before `griffin-worker.service` starts. | Validate `After=local-fs.target` dependencies in systemd unit. |

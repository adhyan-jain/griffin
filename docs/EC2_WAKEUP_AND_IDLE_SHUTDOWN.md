# Griffin EC2 Wake-Up & Idle Shutdown Coordination

## 1. Overview

Scale-to-zero requires precise coordination between the external wake-up controller and the internal EC2 idle shutdown monitor to guarantee:
1. **Idempotent, race-safe startup**.
2. **Zero task loss during shutdown**.
3. **Clean draining of active executions**.
4. **Immediate shutdown abortion when new tasks arrive**.

---

## 2. Wake-Up Controller Design (`internal/runner/wakeup.go`)

The [`WakeupController`](file:///home/adhyan/Desktop/griffin/internal/runner/wakeup.go) runs in the serverless API layer.

### State Evaluation Matrix
When `TriggerWakeup(ctx)` is invoked:

| Current Instance State | Controller Action | Rationale |
| :--- | :--- | :--- |
| `running` | **No-Op** (Update cooldown timestamp) | Runner is already processing tasks. |
| `pending` | **No-Op** (Update cooldown timestamp) | Runner is already booting. |
| `stopped` | Call `ec2:StartInstances` | Initiates instance boot. |
| `stopping` | **Retry with backoff** until `stopped`, then call `StartInstances` | AWS EC2 returns `InvalidInstanceState` if `StartInstances` is called while an instance is stopping. |
| `shutting-down` / `terminated` | Return error; alert operator | Fatal infrastructure anomaly. |

### Startup Debouncing & Concurrency Protection
To prevent API burst traffic (e.g. 50 tasks submitted within 2 seconds) from triggering 50 concurrent AWS EC2 API calls:
- A `sync.Mutex` serializes state checks.
- A configurable `CooldownWindow` (default: 30 seconds) suppresses redundant start calls if a start signal was dispatched recently.

### Least-Privilege IAM Policy for Serverless API
The serverless API role (`GriffinServerlessAPIRole`) requires strictly scoped permissions:
```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "AllowStartGriffinRunnerOnly",
      "Effect": "Allow",
      "Action": [
        "ec2:StartInstances",
        "ec2:DescribeInstances",
        "ec2:DescribeInstanceStatus"
      ],
      "Resource": "arn:aws:ec2:ap-south-1:ACCOUNT_ID:instance/i-GRIFFIN_RUNNER_ID"
    }
  ]
}
```

---

## 3. Idle Shutdown Monitor Design (`internal/runner/shutdown.go`)

The [`IdleShutdownMonitor`](file:///home/adhyan/Desktop/griffin/internal/runner/shutdown.go) runs on the EC2 runner alongside the Go worker pool.

### Lifecycle State Machine

```
   [ACTIVE] <--------------------+
      |                          |
      | 0 runnable tasks         | Runnable task arrives
      v                          |
   [IDLE] -----------------------+
      |                          |
      | IdleTimeout expired      |
      v                          |
  [DRAINING] --------------------+ (Pre-shutdown recheck count > 0)
      |
      | In-flight count == 0 & Recheck count == 0
      v
  [STOPPING] ---> [STOPPED]
```

### 1. Polling & Task Counting
Every `CheckInterval` (default: 5 seconds), the monitor executes [`CountRunnableTasks`](file:///home/adhyan/Desktop/griffin/internal/repository/task_repo.go):
```sql
SELECT COUNT(*)
FROM tasks t
WHERE t.status IN ('RUNNING', 'QUEUED', 'ROUTED')
   OR (t.status = 'WAITING_FOR_DEPENDENCY' AND NOT EXISTS (
       SELECT 1 FROM task_dependencies td
       JOIN tasks parent ON parent.id = td.depends_on_task_id
       WHERE td.task_id = t.id AND parent.status != 'COMPLETED'
   ))
   OR (t.status = 'RETRYING' AND (t.scheduled_for IS NULL OR t.scheduled_for <= NOW()));
```

### 2. Draining Phase
When 0 runnable tasks persist for `IdleTimeout` (default: 300 seconds / 5 minutes):
1. State transitions to `DRAINING`.
2. [`WorkerPool.Pause()`](file:///home/adhyan/Desktop/griffin/internal/worker/worker.go) is called, halting all workers from claiming new tasks.
3. The monitor tracks `WorkerPool.ActiveCount()`. If workers are executing tasks, the monitor waits up to `DrainTimeout` (default: 30 seconds).

### 3. Pre-Shutdown Atomic Verification Check (Race Neutralization)
Even if in-flight tasks have drained to 0, a user could have submitted a new task via the serverless API a millisecond prior.
- Before calling the system poweroff, the monitor runs an **atomic pre-shutdown database query**.
- **If `recheckCount > 0`**:
  - Shutdown is immediately **ABORTED**.
  - `WorkerPool.Resume()` is called.
  - State reverts to `ACTIVE`.
  - A warning is logged: `"Simultaneous task arrival detected during pre-shutdown window; aborting shutdown and resuming workers"`.
- **If `recheckCount == 0`**:
  - State transitions to `STOPPING`.
  - `Stopper.StopInstance(ctx)` executes `sudo shutdown -h now`.

---

## 4. Zero Inbound Ports & Administrative Access

- Inbound Port 22 (SSH) is **permanently closed**.
- Administration and manual maintenance occur via **AWS Systems Manager (SSM) Session Manager**, requiring zero open firewall ports and zero public bastion infrastructure.

# Griffin Scale-to-Zero Architecture

## 1. Executive Summary

Griffin transitions its deployment architecture from an always-on 24/7 AWS EC2 compute model to an **on-demand Scale-to-Zero compute model**.

In this architecture, compute costs are incurred only when runnable tasks exist. While tasks are inactive, the EC2 instance hosting the Go worker and Google Antigravity CLI remains in an AWS `stopped` state, consuming $0.00 in compute CPU and RAM charges. Persistent encrypted EBS storage ($2.40/mo) and Supabase PostgreSQL retain all state.

API ingress and scheduled triggers operate via lightweight, low-cost serverless endpoints that remain available 24/7 to accept tasks, query status, and trigger runner boot.

---

## 2. Architectural Blueprint

```
                              +-------------------------------------------+
                              |          Client / Frontend / PWA          |
                              +---------------------+---------------------+
                                                    |
                                    HTTPS REST API (JWT Authenticated)
                                                    |
                                                    v
+---------------------------------------------------------------------------------------------------+
|                        Serverless API Ingress Layer (Always Available)                            |
|  - Hosted on AWS Lambda / Vercel Edge / Cloud Run (Pay-per-request, $0 idle)                      |
|  - Accepts authenticated task submissions                                                         |
|  - Returns 202 Accepted for tasks requiring a sleeping runner                                      |
|  - WakeupController triggers EC2 startup with debouncing and stopping-state retry                 |
+---------------------+-------------------------------------------------------+---------------------+
                      |                                                       |
        Persist Tasks | (QUEUED status)                         Trigger Wakeup| ec2:StartInstances
                      v                                                       v
+------------------------------------+             +------------------------------------------------+
|     Supabase PostgreSQL 17         |             |              AWS EC2 Cloud Runner              |
|  - Durable task queue & state      |             |         (Graviton2 Ubuntu, ap-south-1)         |
|  - RLS tenant isolation            |<------------+  - Boots only when runnable tasks exist        |
|  - Atomic claims (SKIP LOCKED)     |   Claim &   |  - Runs griffin-worker & Antigravity CLI        |
|  - Event ledgers & attempt history |  Heartbeat  |  - IdleShutdownMonitor drains & stops host     |
+------------------------------------+             +------------------------------------------------+
                  ^                                                           |
                  |                                     AWS EventBridge       | Stops when idle
                  +-----------------------------------------------------------+
                               Scheduler Tick (Periodic Wakeup)
```

---

## 3. Decoupling Principles

### 1. Ingress Availability Independent of Worker State
The public HTTP REST API is never hosted exclusively on the stoppable EC2 instance. The serverless API layer (`GRIFFIN_MODE=api` or containerized serverless deployment) handles authentication, validation, and task queuing directly against Supabase PostgreSQL.

### 2. Asynchronous 202 Accepted Semantics
When a task is submitted and queued while in scale-to-zero mode, the API returns an HTTP `202 Accepted` response with the created task representation and response header:
```http
HTTP/1.1 202 Accepted
Content-Type: application/json
X-Runner-Status: WAKEUP_SIGNALED

{
  "success": true,
  "data": {
    "id": "c1f7a0de-fbb0-4d45-be02-401738739958",
    "status": "QUEUED",
    "title": "Quarterly market analysis",
    "created_at": "2026-11-02T10:20:00Z"
  }
}
```
Clients poll `GET /api/v1/tasks/{id}` or listen via Server-Sent Events (SSE) for task progression.

### 3. Durability Outside Compute RAM
No task state, queue metadata, or audit ledgers reside in volatile EC2 memory. If an EC2 instance crashes, is abruptly stopped, or undergoes spot interruption, all state is safely preserved in Supabase PostgreSQL. Griffin's lease reaper automatically recovers orphaned leases upon reboot.

---

## 4. End-to-End Scale-to-Zero Lifecycle

```
[Stopped State] ---> [Task Arrives] ---> [API Enqueues & Signals Wakeup] ---> [EC2 Boots]
         ^                                                                         |
         |                                                                         v
[EC2 Stops] <--- [Final Recheck (0 tasks)] <--- [Worker Draining] <--- [Idle Period Exceeded]
```

1. **Resting State**: The EC2 instance is in `stopped` state. Compute charge is $0.00/hour.
2. **Work Enqueued**: A user creates a task, an approval is granted, or EventBridge triggers `/api/v1/scheduler/tick`.
3. **Wakeup Signal**: `WakeupController` inspects instance state and issues `ec2:StartInstances`. Debouncing prevents duplicate API calls.
4. **Boot & Recovery**: EC2 boots (~30–45s), systemd launches `griffin-worker` and unlocks credentials on encrypted EBS.
5. **Execution**: Workers claim queued tasks via `SELECT ... FOR UPDATE SKIP LOCKED` and invoke Antigravity CLI.
6. **Idle Countdown**: When the queue empties (`CountRunnableTasks == 0`), `IdleShutdownMonitor` initiates the idle countdown (default: 5 minutes).
7. **Draining**: If no new tasks arrive during the idle window, workers enter `DRAINING` mode (claiming is paused; in-flight tasks finish).
8. **Pre-Shutdown Verification**: Once in-flight executions reach 0, an atomic database re-check executes. If a task arrived during draining, shutdown aborts and workers resume.
9. **Enforce Stop**: If 0 runnable tasks remain, the monitor calls `sudo shutdown -h now` or `ec2:StopInstances`.

---

## 5. Economic & Cost Comparison

Assuming an active workload of **2 hours per day** (60 hours/month):

| Cost Dimension | 24/7 Always-On EC2 (`t4g.medium`) | Scale-to-Zero EC2 (`t4g.medium`) | Savings |
| :--- | :--- | :--- | :--- |
| **EC2 Compute** | $16.35 / month (730 hrs) | **$1.34 / month** (60 hrs) | **91.8% reduction** |
| **EBS Storage (30 GiB gp3)** | $2.40 / month | **$2.40 / month** | Identical (persistent storage) |
| **Serverless API (Lambda)** | $0.00 (hosted on EC2) | **$0.05 / month** (pay-per-request) | Negligible |
| **EventBridge Scheduler** | N/A | **$0.04 / month** (1/min ticks) | Negligible |
| **Public IPv4 Address** | $3.65 / month (always allocated) | **$0.30 / month** (active hours only) | **91.7% reduction** |
| **Total Monthly Cost** | **$22.40 / month** | **$4.13 / month** | **81.6% net savings** |

*When covered by promotional credits, cash out-of-pocket remains $0.00 while credits stretch over 5x longer.*

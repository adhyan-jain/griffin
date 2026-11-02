# Griffin Serverless API & Scheduling Design

## 1. Executive Overview

To support EC2 scale-to-zero, Griffin decouples API availability from worker availability. The public API must remain reachable 24/7 with zero idle compute costs, accepting tasks, reporting status, handling human approval gates, and triggering runner boot on demand.

---

## 2. Serverless API Deployment Options

### Comparison Matrix

| Dimension | Option A: AWS Lambda (`provided.al2023`) (Recommended) | Option B: Cloud Run / App Runner | Option C: Vercel Edge Functions |
| :--- | :--- | :--- | :--- |
| **Idle Cost** | **$0.00 / month** | **$0.00 / month** | **$0.00 / month** |
| **Cold Start** | **~25–50 ms** (Compiled Go binary) | ~200–500 ms (Container) | ~15–30 ms (V8 isolate) |
| **AWS IAM Integration** | Native IAM execution role (`ec2:StartInstances`) | Requires AWS IAM access keys | Requires AWS IAM access keys |
| **Database Connection** | Via Supabase Connection Pooler | Via Supabase Connection Pooler | Via Supabase HTTPS REST API |
| **Network Latency** | Low (same AWS region `ap-south-1`) | Low (`asia-south1`) | Edge-dependent |

### Selected Architecture: AWS Lambda (`provided.al2023`)
- **Binary**: Griffin server compiled for Linux ARM64 with `GRIFFIN_MODE=api`.
- **Packaging**: Zip archive containing single executable binary (`bootstrap`).
- **Ingress**: Amazon API Gateway HTTP API ($1.00 per million requests) or Lambda Function URL (Free).
- **Execution Role**: Has strictly scoped `ec2:StartInstances` permissions for the Griffin EC2 instance ARN.
- **Estimated Cost**: **<$0.10 / month** for personal workload.

---

## 3. Database Connection Pooling Strategy

Serverless functions spawn and terminate dynamically, which can exhaust PostgreSQL connection limits if connected directly to port 5432.
- **Solution**: The serverless API connects to **Supabase Connection Pooler (Supavisor)** on port **6543** (Transaction Mode).
- **Connection Limits**: Maintains lightweight short-lived connections per request without exhausting PostgreSQL backend process limits.

---

## 4. API Endpoints & Scale-to-Zero Behaviors

### 1. `POST /api/v1/tasks` (Task Ingestion)
- **Status**: Returns `202 Accepted` when `SCALE_TO_ZERO_ENABLED=true` and task is placed in `QUEUED` state.
- **Header**: Includes `X-Runner-Status: WAKEUP_SIGNALED`.
- **Action**: Launches asynchronous background goroutine calling `WakeupController.TriggerWakeup(ctx)`.

### 2. `POST /api/v1/approvals/{id}/respond` (Human Approval Gate)
- **Status**: Returns `200 OK` with updated approval object.
- **Action**: If decision is `APPROVE`, task transitions to `QUEUED`. If in scale-to-zero mode, `WakeupController.TriggerWakeup(ctx)` is triggered so the approved task is executed without human operator having to manually start the runner.

### 3. `POST /api/v1/scheduler/tick` (Scheduled Execution Ingress)
- **Trigger**: Invoked periodically (e.g. every 1 to 5 minutes) by **AWS EventBridge Scheduler**.
- **Auth**: Protected by `X-Worker-API-Key` or AWS IAM Authorization.
- **Action**: Queries PostgreSQL for runnable tasks and due schedules. If runnable work exists, signals `WakeupController.TriggerWakeup(ctx)`.
- **Response**:
```json
{
  "success": true,
  "data": {
    "evaluated_at": "2026-11-02T10:25:00Z",
    "runnable_tasks": 2,
    "wakeup_triggered": true
  }
}
```

---

## 5. EventBridge Scheduler Configuration

A lightweight AWS EventBridge schedule is configured to invoke the API without running always-on compute:

```json
{
  "Name": "GriffinSchedulerTick",
  "ScheduleExpression": "rate(5 minutes)",
  "Target": {
    "Arn": "https://api.griffin.internal/api/v1/scheduler/tick",
    "HttpParameters": {
      "HeaderParameters": {
        "X-Worker-API-Key": "<SECURE_WORKER_KEY>"
      }
    },
    "RetryPolicy": {
      "MaximumRetryAttempts": 2
    }
  }
}
```

Cost for 1 invocation every 5 minutes: 8,640 requests/month = **$0.01 / month** (within AWS free tier of 14,000,000 invocations).

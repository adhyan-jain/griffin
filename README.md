# Griffin — Cloud-First Personal AI Workforce

Griffin is a cloud-first personal AI workforce managed by a top-level orchestrator named **JJ**. The backend is a modular Go monolith running a persistent Directed Acyclic Graph (DAG) task engine on PostgreSQL, featuring distributed lease management via `SELECT ... FOR UPDATE SKIP LOCKED`, Supabase Row Level Security (RLS), and zero mandatory pay-per-token API fees.

---

## 1. Prerequisites

- **Go**: 1.22+ (tested on Go 1.27)
- **PostgreSQL**: 16+ (or local Supabase Docker instance)
- **Git**

---

## 2. Quick Start & Local Development

### 1. Configure Environment
Copy the example environment configuration:
```bash
cp .env.example .env
```

### 2. Database Setup
Create the local development and test databases:
```bash
PGPASSWORD=postgres psql -h localhost -p 54322 -U postgres -d postgres -c "CREATE DATABASE griffin_dev;"
PGPASSWORD=postgres psql -h localhost -p 54322 -U postgres -d postgres -c "CREATE DATABASE griffin_test;"
```

Apply migrations:
```bash
make migrate
```
*(Note: Migrations are also automatically applied by the server on boot and by the test suite on test initialization).*

### 3. Build & Run the Backend
```bash
make run
```
The server will start on `http://localhost:8080` with background lease reapers and 2 mock worker execution goroutines.

### 4. Run Automated Tests
```bash
make test-v
```

---

## 3. Database Schema & Migration Guide

Migrations are version-controlled in [`migrations/`](file:///home/adhyan/Desktop/griffin/migrations/):

| File | Purpose |
| :--- | :--- |
| `000001_init_schema.up.sql` | DDL for profiles, workspaces, agents, tasks, task_dependencies, task_attempts, task_events, approvals, schedules, artifacts, and notifications. |
| `000001_init_schema.down.sql` | Clean rollback for all tables and custom enums. |
| `000002_rls_policies.up.sql` | Enables and enforces Row Level Security (`FORCE ROW LEVEL SECURITY`) with `auth.uid() = user_id` tenant isolation policies. |
| `000002_rls_policies.down.sql` | Disables RLS and drops all security policies. |

---

## 4. REST API Documentation & Examples

All user endpoints require a valid Supabase JWT Bearer token: `Authorization: Bearer <token>`.
All worker endpoints require `X-Worker-Key: <WORKER_API_KEY>`.

### Health & Readiness (Unauthenticated)
```bash
# Liveness
curl -s http://localhost:8080/healthz

# Readiness (pings database)
curl -s http://localhost:8080/readyz
```

### Task Management (User Auth)
```bash
# 1. Create a Task
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Authorization: Bearer $JWT" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: task-unique-12345" \
  -d '{
    "title": "Synthesize competitor analysis",
    "goal": "Audit top 3 competitors and produce markdown report",
    "manager_domain": "JJ",
    "priority": "HIGH_P1",
    "input_payload": {"market": "AI developer tools"}
  }'

# 2. Get Task by ID
curl -s -H "Authorization: Bearer $JWT" http://localhost:8080/api/v1/tasks/<task_id>

# 3. List / Filter Tasks
curl -s -H "Authorization: Bearer $JWT" "http://localhost:8080/api/v1/tasks?status=QUEUED&limit=10"

# 4. Cancel Task (Cascades to dependent children)
curl -X POST http://localhost:8080/api/v1/tasks/<task_id>/cancel \
  -H "Authorization: Bearer $JWT" \
  -H "Content-Type: application/json" \
  -d '{"reason": "Cancelled by user"}'

# 5. Get Task Event History
curl -s -H "Authorization: Bearer $JWT" http://localhost:8080/api/v1/tasks/<task_id>/events

# 6. Get Task Execution Attempts
curl -s -H "Authorization: Bearer $JWT" http://localhost:8080/api/v1/tasks/<task_id>/attempts
```

### Human Approvals (User Auth)
```bash
# List Pending Approvals
curl -s -H "Authorization: Bearer $JWT" "http://localhost:8080/api/v1/approvals?status=PENDING"

# Approve or Reject
curl -X POST http://localhost:8080/api/v1/approvals/<approval_id>/respond \
  -H "Authorization: Bearer $JWT" \
  -H "Content-Type: application/json" \
  -d '{"decision": "APPROVE", "reason": "Looks good to proceed"}'
```

### Worker Endpoints (Worker Auth)
```bash
# 1. Claim Next Available Task (SKIP LOCKED)
curl -X POST http://localhost:8080/api/v1/workers/claim \
  -H "X-Worker-Key: $WORKER_API_KEY" \
  -H "X-Worker-ID: cloud-worker-1"

# 2. Renew Lease Heartbeat
curl -X POST http://localhost:8080/api/v1/workers/<task_id>/heartbeat \
  -H "X-Worker-Key: $WORKER_API_KEY" \
  -H "X-Worker-ID: cloud-worker-1"

# 3. Complete Task
curl -X POST http://localhost:8080/api/v1/workers/<task_id>/complete \
  -H "X-Worker-Key: $WORKER_API_KEY" \
  -H "X-Worker-ID: cloud-worker-1" \
  -H "Content-Type: application/json" \
  -d '{"output": {"summary": "Analysis completed successfully"}}'

# 4. Fail Task (Triggers retry backoff or terminal failure)
curl -X POST http://localhost:8080/api/v1/workers/<task_id>/fail \
  -H "X-Worker-Key: $WORKER_API_KEY" \
  -H "X-Worker-ID: cloud-worker-1" \
  -H "Content-Type: application/json" \
  -d '{"reason": "Provider rate limited", "retryable": true}'
```

---

## 5. Architectural Invariants

- **Zero API Keys Required**: Default mock worker runs deterministically for local testing. Antigravity primary execution runner connects via worker endpoints in Phase 2.
- **Atomic Concurrency**: Tasks are claimed using PostgreSQL `SELECT ... FOR UPDATE SKIP LOCKED`. Two workers can never claim the same task simultaneously.
- **Lease Protection & Reaper**: Running tasks have time-bounded leases (default 60s) renewed by worker heartbeats. Abandoned tasks are reclaimed by the background reaper daemon.
- **Tenant Isolation**: Row Level Security (RLS) policies guarantee that no user can query or modify another user's tasks, agents, or artifacts.

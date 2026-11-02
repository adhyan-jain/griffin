# Agent Factory & Specialist Worker Definitions

## 1. Overview & Purpose

In Griffin, specialist managers delegate tactical work to focused **specialist workers**.

The **Agent Factory** (`internal/service/agent_factory.go`) provides:
1. Reusable, template-based worker definitions associated with domain managers.
2. Strict **versioned immutability**: updating a worker definition never overwrites previous records; instead, it creates a new immutable version with an incremented version number (`version: 2, 3, ...`).
3. Complete **audit logging**: every creation, update, and toggle event is recorded in `agent_factory_audits` with actor attribution, timestamp, and a JSON delta of changes.
4. **Task instantiation** (`InstantiateTask`): stamps out ready-to-execute `domain.Task` instances with injected worker prompts, schemas, tools, and capability boundaries.

---

## 2. Worker Definition Schema

Persisted in `worker_definitions` table with PostgreSQL Row Level Security:

```sql
CREATE TABLE IF NOT EXISTS worker_definitions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,
    worker_key VARCHAR(100) NOT NULL,
    manager_id VARCHAR(50) NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL,
    system_prompt TEXT NOT NULL,
    input_schema JSONB NOT NULL DEFAULT '{}'::jsonb,
    output_schema JSONB NOT NULL DEFAULT '{}'::jsonb,
    allowed_capabilities JSONB NOT NULL DEFAULT '[]'::jsonb,
    allowed_tools JSONB NOT NULL DEFAULT '[]'::jsonb,
    execution_timeout_sec INT NOT NULL DEFAULT 300,
    max_retries INT NOT NULL DEFAULT 3,
    verification_criteria JSONB NOT NULL DEFAULT '[]'::jsonb,
    version INT NOT NULL DEFAULT 1,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, worker_key, version)
);
```

---

## 3. Immutability & Audit Trail

### Versioning Model
- When a worker is created via `CreateWorkerDefinition`, its initial version is `1`.
- When updated via `UpdateWorkerDefinition`, Griffin looks up the latest active record, creates a **brand-new database row** with `version = latest.version + 1`, and copies forward unchanged configuration fields.
- Historical versions remain queryable via `ListDefinitionVersions` (`GET /api/v1/workers/definitions/{id}/versions`).
- Previous task executions retain unambiguous references to the exact worker version that executed them.

### Audit Log (`agent_factory_audits`)
All mutations record an audit row:
```sql
CREATE TABLE IF NOT EXISTS agent_factory_audits (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,
    worker_definition_id UUID REFERENCES worker_definitions(id) ON DELETE SET NULL,
    worker_key VARCHAR(100) NOT NULL,
    version INT NOT NULL,
    action VARCHAR(50) NOT NULL, -- 'CREATED', 'UPDATED', 'TOGGLED'
    actor VARCHAR(50) NOT NULL DEFAULT 'USER', -- 'USER', 'JJ', 'SYSTEM'
    changes JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

---

## 4. Task Instantiation (`InstantiateTask`)

When JJ delegates a child task referencing a worker key:
```go
task, err := factory.InstantiateTask(ctx, def, parentID, rootID, title, goal, userPayload, priority)
```
The factory:
1. Verifies that the worker is currently enabled (`is_active = true`).
2. Wraps the user payload into an execution context embedding:
   - `_worker_key`: Worker key.
   - `_worker_version`: Exact active version number.
   - `_system_prompt`: Domain-specialized system prompt.
   - `_capabilities`: Allowed capability whitelist.
   - `_tools`: Allowed tool whitelist.
   - `_output_schema`: Structured JSON output schema.
3. Configures execution timeouts and retry policies defined on the worker.
4. Returns a fully materialized `domain.Task` ready for `TaskEngine.CreateTask`.

---

## 5. REST API Endpoints

| Method | Endpoint | Description |
| :--- | :--- | :--- |
| `GET` | `/api/v1/workers/definitions` | Lists latest versions of all worker definitions (optional `?manager_id=xxx` filter). |
| `GET` | `/api/v1/workers/definitions/{id}` | Retrieves a worker definition by ID. |
| `GET` | `/api/v1/workers/definitions/{id}/versions` | Retrieves the entire immutable version history for a worker key. |
| `POST` | `/api/v1/workers/definitions` | Registers a new worker definition (initializes at `version = 1`). |
| `PUT` | `/api/v1/workers/definitions/{id}` | Updates a worker definition (creates a new row with `version + 1`). |
| `POST` | `/api/v1/workers/definitions/{id}/toggle` | Enables or disables a worker definition (`{"is_active": false}`). |

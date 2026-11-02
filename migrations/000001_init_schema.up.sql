-- 000001_init_schema.up.sql
-- Griffin Core Schema Migration

-- Extensions
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- NOTE: this migration requires the Supabase `auth` schema (auth.users, auth.uid()).
-- On plain PostgreSQL (local/CI) apply migrations/local/auth_shim.sql first via
-- `griffin-migrate -local-auth-shim`. It must never redefine auth.uid() on Supabase.

-- -----------------------------------------------------------------------------
-- ENUMS
-- -----------------------------------------------------------------------------
DO $$ BEGIN
    CREATE TYPE task_status AS ENUM (
        'CREATED',
        'ROUTED',
        'QUEUED',
        'RUNNING',
        'WAITING_FOR_DEPENDENCY',
        'VERIFYING',
        'AWAITING_APPROVAL',
        'RETRYING',
        'COMPLETED',
        'FAILED',
        'CANCELLED'
    );
EXCEPTION
    WHEN duplicate_object THEN null;
END $$;

DO $$ BEGIN
    CREATE TYPE task_priority AS ENUM (
        'CRITICAL_P0',
        'HIGH_P1',
        'NORMAL_P2',
        'BACKGROUND_P3'
    );
EXCEPTION
    WHEN duplicate_object THEN null;
END $$;

DO $$ BEGIN
    CREATE TYPE approval_status AS ENUM (
        'PENDING',
        'APPROVED',
        'REJECTED',
        'EXPIRED'
    );
EXCEPTION
    WHEN duplicate_object THEN null;
END $$;

DO $$ BEGIN
    CREATE TYPE approval_risk_level AS ENUM (
        'LOW',
        'MEDIUM',
        'HIGH',
        'CRITICAL'
    );
EXCEPTION
    WHEN duplicate_object THEN null;
END $$;

-- -----------------------------------------------------------------------------
-- 1. PROFILES & WORKSPACES
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS profiles (
    id UUID PRIMARY KEY REFERENCES auth.users(id) ON DELETE CASCADE,
    email VARCHAR(255) NOT NULL,
    full_name VARCHAR(255),
    role VARCHAR(50) NOT NULL DEFAULT 'OWNER',
    settings JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS workspaces (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,
    name VARCHAR(100) NOT NULL,
    slug VARCHAR(100) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, slug)
);

-- -----------------------------------------------------------------------------
-- 2. AGENTS & AGENT REGISTRY
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS agents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,
    name VARCHAR(100) NOT NULL,
    role_type VARCHAR(50) NOT NULL, -- 'ORCHESTRATOR', 'MANAGER', 'WORKER'
    manager_domain VARCHAR(50) NOT NULL, -- 'JJ', 'CAREER', 'RESEARCH', 'ENGINEERING', etc.
    description TEXT NOT NULL,
    system_prompt TEXT NOT NULL,
    provider_preference VARCHAR(50) NOT NULL DEFAULT 'ANTIGRAVITY_PRIMARY',
    model_preference VARCHAR(50) NOT NULL DEFAULT 'gemini-2.5-pro',
    max_execution_timeout_sec INT NOT NULL DEFAULT 300,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- -----------------------------------------------------------------------------
-- 3. TASKS
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS tasks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,
    workspace_id UUID REFERENCES workspaces(id) ON DELETE SET NULL,
    parent_id UUID REFERENCES tasks(id) ON DELETE SET NULL,
    root_id UUID REFERENCES tasks(id) ON DELETE SET NULL,
    assigned_agent_id UUID REFERENCES agents(id) ON DELETE SET NULL,
    manager_domain VARCHAR(50) NOT NULL DEFAULT 'JJ',
    title VARCHAR(255) NOT NULL,
    goal TEXT NOT NULL,
    input_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    output_payload JSONB,
    status task_status NOT NULL DEFAULT 'CREATED',
    priority task_priority NOT NULL DEFAULT 'NORMAL_P2',
    idempotency_key VARCHAR(255),
    
    -- Distributed Leases
    leased_by VARCHAR(100),
    lease_expires_at TIMESTAMPTZ,
    
    -- Execution tracking & retries
    retry_count INT NOT NULL DEFAULT 0,
    max_retries INT NOT NULL DEFAULT 3,
    failure_reason TEXT,
    scheduled_for TIMESTAMPTZ,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Targeted queue polling index (sub-millisecond claim)
CREATE INDEX IF NOT EXISTS idx_tasks_queue_poll 
ON tasks (status, scheduled_for, priority DESC, created_at ASC)
WHERE status = 'QUEUED';

-- Lease expiration & reaper index
CREATE INDEX IF NOT EXISTS idx_tasks_lease_reap 
ON tasks (status, lease_expires_at)
WHERE status = 'RUNNING';

-- Idempotency constraint per user
CREATE UNIQUE INDEX IF NOT EXISTS idx_tasks_idempotency 
ON tasks (user_id, idempotency_key)
WHERE idempotency_key IS NOT NULL AND status NOT IN ('CANCELLED', 'FAILED');

-- User & status lookup
CREATE INDEX IF NOT EXISTS idx_tasks_user_status ON tasks(user_id, status);
CREATE INDEX IF NOT EXISTS idx_tasks_parent_id ON tasks(parent_id);

-- -----------------------------------------------------------------------------
-- 4. TASK DEPENDENCIES
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS task_dependencies (
    task_id UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    depends_on_task_id UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (task_id, depends_on_task_id)
);

CREATE INDEX IF NOT EXISTS idx_task_deps_depends_on ON task_dependencies(depends_on_task_id);

-- -----------------------------------------------------------------------------
-- 5. TASK ATTEMPTS & AGENT RUNS
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS task_attempts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    agent_id UUID REFERENCES agents(id) ON DELETE SET NULL,
    worker_id VARCHAR(100) NOT NULL,
    attempt_number INT NOT NULL DEFAULT 1,
    status VARCHAR(50) NOT NULL, -- 'RUNNING', 'COMPLETED', 'FAILED', 'TIMED_OUT'
    prompt_tokens INT NOT NULL DEFAULT 0,
    completion_tokens INT NOT NULL DEFAULT 0,
    duration_ms INT NOT NULL DEFAULT 0,
    error_message TEXT,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_task_attempts_task_id ON task_attempts(task_id, attempt_number ASC);

-- -----------------------------------------------------------------------------
-- 6. TASK EVENTS (Append-only audit log)
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS task_events (
    id BIGSERIAL PRIMARY KEY,
    task_id UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    event_type VARCHAR(50) NOT NULL, -- 'STATUS_CHANGE', 'TOOL_CALLED', 'RETRY', 'HEARTBEAT', 'LOG'
    message TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_task_events_task_id ON task_events(task_id, created_at ASC);

-- -----------------------------------------------------------------------------
-- 7. APPROVALS
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS approvals (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,
    task_id UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    action_type VARCHAR(100) NOT NULL, -- 'SEND_EMAIL', 'APPLY_JOB', 'GITHUB_MERGE', 'FINANCIAL', 'EXECUTE_TOOL'
    risk_level approval_risk_level NOT NULL DEFAULT 'MEDIUM',
    summary TEXT NOT NULL,
    action_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    status approval_status NOT NULL DEFAULT 'PENDING',
    decided_by UUID REFERENCES auth.users(id),
    decision_reason TEXT,
    decided_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_approvals_user_status ON approvals(user_id, status);
CREATE INDEX IF NOT EXISTS idx_approvals_task_id ON approvals(task_id);

-- -----------------------------------------------------------------------------
-- 8. SCHEDULES
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS schedules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,
    manager_domain VARCHAR(50) NOT NULL DEFAULT 'JJ',
    name VARCHAR(100) NOT NULL,
    cron_expression VARCHAR(50) NOT NULL, -- e.g. '0 8 * * *'
    timezone VARCHAR(50) NOT NULL DEFAULT 'Asia/Kolkata',
    task_spec_template JSONB NOT NULL DEFAULT '{}'::jsonb,
    is_paused BOOLEAN NOT NULL DEFAULT FALSE,
    last_run_at TIMESTAMPTZ,
    next_run_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_schedules_next_run ON schedules(next_run_at) WHERE is_paused = FALSE;

-- -----------------------------------------------------------------------------
-- 9. ARTIFACTS
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS artifacts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,
    task_id UUID REFERENCES tasks(id) ON DELETE SET NULL,
    title VARCHAR(255) NOT NULL,
    artifact_type VARCHAR(50) NOT NULL, -- 'GOOGLE_DOC', 'PDF', 'MARKDOWN', 'CODE_DIFF', 'JSON'
    storage_uri TEXT NOT NULL,
    file_size_bytes BIGINT,
    mime_type VARCHAR(100),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_artifacts_user_id ON artifacts(user_id);
CREATE INDEX IF NOT EXISTS idx_artifacts_task_id ON artifacts(task_id);

-- -----------------------------------------------------------------------------
-- 10. NOTIFICATIONS
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS notifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,
    title VARCHAR(255) NOT NULL,
    body TEXT NOT NULL,
    channel VARCHAR(50) NOT NULL DEFAULT 'IN_APP', -- 'IN_APP', 'WEB_PUSH', 'EMAIL'
    priority VARCHAR(20) NOT NULL DEFAULT 'NORMAL',
    read_at TIMESTAMPTZ,
    action_url TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_notifications_user_read ON notifications(user_id, read_at);

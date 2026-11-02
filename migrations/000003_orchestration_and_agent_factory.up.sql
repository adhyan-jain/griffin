-- 000003_orchestration_and_agent_factory.up.sql
-- Griffin Phase 3: Orchestration, Managers & Agent Factory Schema

-- -----------------------------------------------------------------------------
-- 1. WORKER DEFINITIONS (Specialist worker specifications with versioning)
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS worker_definitions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,
    worker_key VARCHAR(100) NOT NULL,
    version INT NOT NULL DEFAULT 1,
    manager_id VARCHAR(50) NOT NULL,
    name VARCHAR(150) NOT NULL,
    description TEXT NOT NULL,
    system_prompt TEXT NOT NULL,
    domain VARCHAR(50) NOT NULL,
    input_schema JSONB NOT NULL DEFAULT '{}'::jsonb,
    output_schema JSONB NOT NULL DEFAULT '{}'::jsonb,
    allowed_capabilities TEXT[] NOT NULL DEFAULT '{}',
    allowed_tools TEXT[] NOT NULL DEFAULT '{}',
    execution_timeout_sec INT NOT NULL DEFAULT 300,
    max_retries INT NOT NULL DEFAULT 3,
    verification_criteria TEXT[] NOT NULL DEFAULT '{}',
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, worker_key, version)
);

CREATE INDEX IF NOT EXISTS idx_worker_defs_user_key ON worker_definitions(user_id, worker_key, version DESC);
CREATE INDEX IF NOT EXISTS idx_worker_defs_manager ON worker_definitions(user_id, manager_id);

-- -----------------------------------------------------------------------------
-- 2. AGENT FACTORY AUDIT LOG (Immutable audit history of worker definitions)
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS agent_factory_audits (
    id BIGSERIAL PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,
    worker_definition_id UUID REFERENCES worker_definitions(id) ON DELETE SET NULL,
    worker_key VARCHAR(100) NOT NULL,
    version INT NOT NULL,
    action VARCHAR(50) NOT NULL, -- 'CREATED', 'UPDATED', 'TOGGLED'
    actor VARCHAR(50) NOT NULL,  -- 'USER', 'JJ', 'SYSTEM'
    changes JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_agent_factory_audits_user ON agent_factory_audits(user_id, worker_key, created_at ASC);

-- -----------------------------------------------------------------------------
-- 3. ORCHESTRATIONS (Top-level JJ Orchestration lifecycle tracking)
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS orchestrations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,
    workspace_id UUID REFERENCES workspaces(id) ON DELETE SET NULL,
    root_task_id UUID REFERENCES tasks(id) ON DELETE CASCADE,
    request_text TEXT NOT NULL,
    manager_id VARCHAR(50) NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'PLANNING', -- 'PLANNING', 'DELEGATED', 'RUNNING', 'AWAITING_APPROVAL', 'VERIFYING', 'COMPLETED', 'FAILED', 'CANCELLED'
    final_response TEXT,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_orchestrations_user_status ON orchestrations(user_id, status);
CREATE INDEX IF NOT EXISTS idx_orchestrations_root_task ON orchestrations(root_task_id);

-- -----------------------------------------------------------------------------
-- 4. ORCHESTRATION PLANS (Structured plans produced by JJ / Managers)
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS orchestration_plans (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    orchestration_id UUID NOT NULL REFERENCES orchestrations(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,
    objective TEXT NOT NULL,
    interpreted_request TEXT NOT NULL,
    manager_id VARCHAR(50) NOT NULL,
    task_type VARCHAR(100) NOT NULL,
    priority VARCHAR(50) NOT NULL DEFAULT 'NORMAL_P2',
    plan_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    status VARCHAR(50) NOT NULL DEFAULT 'DRAFT', -- 'DRAFT', 'VALIDATED', 'REJECTED', 'EXECUTED'
    validation_errors JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_orchestration_plans_orchestration ON orchestration_plans(orchestration_id);

-- -----------------------------------------------------------------------------
-- 5. DELEGATION RECORDS (Tracking parent/child task delegation)
-- -----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS delegation_records (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    orchestration_id UUID NOT NULL REFERENCES orchestrations(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES auth.users(id) ON DELETE CASCADE,
    parent_task_id UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    child_task_id UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    task_key VARCHAR(100) NOT NULL,
    manager_id VARCHAR(50) NOT NULL,
    worker_key VARCHAR(100),
    status VARCHAR(50) NOT NULL DEFAULT 'QUEUED',
    result_summary TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(orchestration_id, child_task_id)
);

CREATE INDEX IF NOT EXISTS idx_delegation_records_orchestration ON delegation_records(orchestration_id);
CREATE INDEX IF NOT EXISTS idx_delegation_records_child ON delegation_records(child_task_id);

-- -----------------------------------------------------------------------------
-- 6. ROW LEVEL SECURITY (RLS) POLICIES
-- -----------------------------------------------------------------------------
ALTER TABLE worker_definitions ENABLE ROW LEVEL SECURITY;
ALTER TABLE worker_definitions FORCE ROW LEVEL SECURITY;
ALTER TABLE agent_factory_audits ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_factory_audits FORCE ROW LEVEL SECURITY;
ALTER TABLE orchestrations ENABLE ROW LEVEL SECURITY;
ALTER TABLE orchestrations FORCE ROW LEVEL SECURITY;
ALTER TABLE orchestration_plans ENABLE ROW LEVEL SECURITY;
ALTER TABLE orchestration_plans FORCE ROW LEVEL SECURITY;
ALTER TABLE delegation_records ENABLE ROW LEVEL SECURITY;
ALTER TABLE delegation_records FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS worker_definitions_user_policy ON worker_definitions;
CREATE POLICY worker_definitions_user_policy ON worker_definitions
    FOR ALL
    USING (auth.uid() = user_id)
    WITH CHECK (auth.uid() = user_id);

DROP POLICY IF EXISTS agent_factory_audits_user_policy ON agent_factory_audits;
CREATE POLICY agent_factory_audits_user_policy ON agent_factory_audits
    FOR ALL
    USING (auth.uid() = user_id)
    WITH CHECK (auth.uid() = user_id);

DROP POLICY IF EXISTS orchestrations_user_policy ON orchestrations;
CREATE POLICY orchestrations_user_policy ON orchestrations
    FOR ALL
    USING (auth.uid() = user_id)
    WITH CHECK (auth.uid() = user_id);

DROP POLICY IF EXISTS orchestration_plans_user_policy ON orchestration_plans;
CREATE POLICY orchestration_plans_user_policy ON orchestration_plans
    FOR ALL
    USING (auth.uid() = user_id)
    WITH CHECK (auth.uid() = user_id);

DROP POLICY IF EXISTS delegation_records_user_policy ON delegation_records;
CREATE POLICY delegation_records_user_policy ON delegation_records
    FOR ALL
    USING (auth.uid() = user_id)
    WITH CHECK (auth.uid() = user_id);

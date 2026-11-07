-- -----------------------------------------------------------------------------
-- 000005: Execution hosts (laptop, phone) - ADR-074 / PRD 7.15
--
-- A host is a device that connects OUT to the Host API with its own revocable token and does work.
-- The owner grants capabilities (`capabilities`); the host reports what it can currently do
-- (`advertised`); the effective set is the intersection, so a host can never escalate itself.
-- Only a SHA-256 hash of the token is stored. Hosts never receive database credentials.
-- -----------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS hosts (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    name          VARCHAR(100) NOT NULL,
    kind          VARCHAR(20)  NOT NULL CHECK (kind IN ('laptop', 'phone')),
    capabilities  TEXT[] NOT NULL DEFAULT '{}',
    advertised    TEXT[] NOT NULL DEFAULT '{}',
    version       VARCHAR(64) NOT NULL DEFAULT '',
    token_hash    CHAR(64) NOT NULL UNIQUE,
    paused        BOOLEAN NOT NULL DEFAULT FALSE,
    last_seen_at  TIMESTAMPTZ,
    revoked_at    TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_hosts_user ON hosts(user_id);
CREATE INDEX IF NOT EXISTS idx_hosts_live ON hosts(last_seen_at) WHERE revoked_at IS NULL;

-- Tenant isolation, same pattern as every other tenant table.
ALTER TABLE hosts ENABLE ROW LEVEL SECURITY;
ALTER TABLE hosts FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS hosts_user_policy ON hosts;
CREATE POLICY hosts_user_policy ON hosts
    FOR ALL
    USING (auth.uid() = user_id)
    WITH CHECK (auth.uid() = user_id);

-- Claiming for a host filters on a step's declared `requires` (input_payload->'requires').
CREATE INDEX IF NOT EXISTS idx_tasks_requires ON tasks ((jsonb_typeof(input_payload->'requires')))
    WHERE status IN ('QUEUED', 'RETRYING');

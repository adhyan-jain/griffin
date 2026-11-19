-- -----------------------------------------------------------------------------
-- 000015: Durable scheduled operations (idempotent runs, leases, cursors)
-- -----------------------------------------------------------------------------

-- One row per (user, operation, run key). The unique key is the idempotency key: a brief for a
-- given local day, a discovery sweep for a given slot, and so on. status moves
-- running -> succeeded | failed -> (retry) running | dead.
CREATE TABLE IF NOT EXISTS scheduled_operations (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    op_type          VARCHAR(64) NOT NULL,
    run_key          VARCHAR(255) NOT NULL,
    status           VARCHAR(16) NOT NULL DEFAULT 'running'
                     CHECK (status IN ('running', 'succeeded', 'failed', 'dead')),
    attempts         INT NOT NULL DEFAULT 1,
    lease_expires_at TIMESTAMPTZ,
    next_attempt_at  TIMESTAMPTZ,
    last_error       TEXT NOT NULL DEFAULT '',
    started_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at      TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, op_type, run_key)
);

CREATE INDEX IF NOT EXISTS idx_scheduled_operations_user_type ON scheduled_operations (user_id, op_type, created_at DESC);

-- Persistent per-user, per-operation state: the sync cursor (e.g. last processed id) and the
-- last success / error, so progress survives restarts and is observable.
CREATE TABLE IF NOT EXISTS operation_cursors (
    user_id         UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    op_type         VARCHAR(64) NOT NULL,
    cursor          JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_success_at TIMESTAMPTZ,
    last_error      TEXT NOT NULL DEFAULT '',
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, op_type)
);

DO $$
DECLARE t TEXT;
BEGIN
    FOREACH t IN ARRAY ARRAY['scheduled_operations', 'operation_cursors'] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', t || '_user_policy', t);
        EXECUTE format('CREATE POLICY %I ON %I FOR ALL USING (auth.uid() = user_id) WITH CHECK (auth.uid() = user_id)', t || '_user_policy', t);
    END LOOP;
END $$;

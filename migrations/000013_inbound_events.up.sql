-- -----------------------------------------------------------------------------
-- 000013: Inbound events & notification triage ledger
-- -----------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS inbound_events (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    source        VARCHAR(64) NOT NULL,
    package_name  VARCHAR(255) NOT NULL DEFAULT '',
    category      VARCHAR(64) NOT NULL DEFAULT 'other',
    title         TEXT NOT NULL DEFAULT '',
    text          TEXT NOT NULL DEFAULT '',
    actionable    BOOLEAN NOT NULL DEFAULT false,
    triage_action VARCHAR(64) NOT NULL DEFAULT 'none',
    payload       JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_inbound_events_user_created ON inbound_events (user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_inbound_events_actionable ON inbound_events (user_id, actionable) WHERE actionable = true;

ALTER TABLE inbound_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE inbound_events FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS inbound_events_user_policy ON inbound_events;
CREATE POLICY inbound_events_user_policy ON inbound_events FOR ALL USING (auth.uid() = user_id) WITH CHECK (auth.uid() = user_id);

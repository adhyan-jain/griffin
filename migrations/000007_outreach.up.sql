-- -----------------------------------------------------------------------------
-- 000007: Outreach campaigns (cold mail, applications, messages) and their people
--
-- A campaign is one job. A target is one person in it, with a status that only moves along the transitions
-- in domain/outreach.go. Every update touches a single row, so a researching agent and a sending agent can
-- work on one campaign at once. Adds tables only: the previous binaries keep working until deployed.
-- -----------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS outreach_campaigns (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    name       TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    goal       TEXT NOT NULL DEFAULT '',
    channel    TEXT NOT NULL DEFAULT 'email' CHECK (channel IN ('email','linkedin','whatsapp','instagram','other')),
    status     TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','paused','done')),
    limits     JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS outreach_targets (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    campaign_id      UUID NOT NULL REFERENCES outreach_campaigns(id) ON DELETE CASCADE,
    user_id          UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    name             TEXT NOT NULL DEFAULT '',
    handle           TEXT NOT NULL CHECK (length(handle) BETWEEN 1 AND 500),
    info             TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'found'
                     CHECK (status IN ('found','drafted','approved','sent','replied','followup_due','closed')),
    draft            TEXT NOT NULL DEFAULT '',
    sent_at          TIMESTAMPTZ,
    replied_at       TIMESTAMPTZ,
    reply_text       TEXT NOT NULL DEFAULT '',
    next_followup_at TIMESTAMPTZ,
    followups_sent   INTEGER NOT NULL DEFAULT 0 CHECK (followups_sent >= 0),
    -- a sender holds a lease while it works on this person, so two senders never message them twice
    claimed_until    TIMESTAMPTZ,
    notes            TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (campaign_id, handle)
);

CREATE INDEX IF NOT EXISTS outreach_targets_campaign_status ON outreach_targets (campaign_id, status);
CREATE INDEX IF NOT EXISTS outreach_targets_followups ON outreach_targets (next_followup_at) WHERE status = 'sent';

-- An audit trail: who/what changed a target and when (for "why was this sent?").
CREATE TABLE IF NOT EXISTS outreach_events (
    id         BIGSERIAL PRIMARY KEY,
    target_id  UUID NOT NULL REFERENCES outreach_targets(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL,
    detail     TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

DO $$ DECLARE t text; BEGIN
    FOREACH t IN ARRAY ARRAY['outreach_campaigns','outreach_targets','outreach_events'] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', t || '_user_policy', t);
        EXECUTE format('CREATE POLICY %I ON %I FOR ALL USING (auth.uid() = user_id) WITH CHECK (auth.uid() = user_id)', t || '_user_policy', t);
    END LOOP;
END $$;

-- -----------------------------------------------------------------------------
-- 000014: Inbox intelligence, job discovery, assessments & application tracking
-- -----------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS oauth_credentials (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    provider      VARCHAR(64) NOT NULL,
    email         VARCHAR(255) NOT NULL DEFAULT '',
    access_token  TEXT NOT NULL DEFAULT '',
    refresh_token TEXT NOT NULL DEFAULT '',
    token_type    VARCHAR(32) NOT NULL DEFAULT 'Bearer',
    expires_at    TIMESTAMPTZ,
    scope         TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, provider, email)
);

CREATE TABLE IF NOT EXISTS email_threads (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    thread_id       VARCHAR(128) NOT NULL,
    message_id      VARCHAR(128) NOT NULL,
    sender          TEXT NOT NULL DEFAULT '',
    recipient       TEXT NOT NULL DEFAULT '',
    subject         TEXT NOT NULL DEFAULT '',
    snippet         TEXT NOT NULL DEFAULT '',
    body_text       TEXT NOT NULL DEFAULT '',
    has_attachments BOOLEAN NOT NULL DEFAULT false,
    received_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, message_id)
);

CREATE TABLE IF NOT EXISTS assessments (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    company          VARCHAR(255) NOT NULL,
    platform         VARCHAR(64) NOT NULL,
    role             VARCHAR(255) NOT NULL DEFAULT '',
    test_url         TEXT NOT NULL DEFAULT '',
    deadline         TIMESTAMPTZ,
    status           VARCHAR(32) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'completed', 'expired')),
    inbound_event_id UUID REFERENCES inbound_events(id) ON DELETE SET NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS job_discoveries (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    source      VARCHAR(64) NOT NULL,
    external_id VARCHAR(255) NOT NULL DEFAULT '',
    company     VARCHAR(255) NOT NULL,
    role        VARCHAR(255) NOT NULL,
    url         TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    tier        VARCHAR(32) NOT NULL DEFAULT 'Standard',
    status      VARCHAR(32) NOT NULL DEFAULT 'discovered' CHECK (status IN ('discovered', 'staged', 'applied', 'rejected')),
    task_id     UUID REFERENCES tasks(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, company, role)
);

CREATE TABLE IF NOT EXISTS application_submissions (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    company          VARCHAR(255) NOT NULL,
    role             VARCHAR(255) NOT NULL,
    url              TEXT NOT NULL,
    policy_decision  VARCHAR(32) NOT NULL DEFAULT 'escalate' CHECK (policy_decision IN ('auto_staged', 'auto_submitted', 'escalate')),
    form_payload     JSONB NOT NULL DEFAULT '{}'::jsonb,
    tailored_bullets TEXT NOT NULL DEFAULT '',
    receipt_path     TEXT NOT NULL DEFAULT '',
    receipt_hash     VARCHAR(128) NOT NULL DEFAULT '',
    submitted_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_email_threads_user_received ON email_threads (user_id, received_at DESC);
CREATE INDEX IF NOT EXISTS idx_email_threads_user_thread ON email_threads (user_id, thread_id);
CREATE INDEX IF NOT EXISTS idx_assessments_user_deadline ON assessments (user_id, deadline ASC) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_job_discoveries_user_status ON job_discoveries (user_id, status);
CREATE INDEX IF NOT EXISTS idx_application_submissions_user ON application_submissions (user_id, submitted_at DESC);

DO $$ DECLARE tbl text; BEGIN
    FOREACH tbl IN ARRAY ARRAY['oauth_credentials', 'email_threads', 'assessments', 'job_discoveries', 'application_submissions'] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', tbl);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', tbl);
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', tbl || '_user_policy', tbl);
        EXECUTE format('CREATE POLICY %I ON %I FOR ALL USING (auth.uid() = user_id) WITH CHECK (auth.uid() = user_id)', tbl || '_user_policy', tbl);
    END LOOP;
END $$;

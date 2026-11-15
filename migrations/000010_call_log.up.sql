-- -----------------------------------------------------------------------------
-- 000010: Inbound call log
--
-- One row per inbound call the phone provider hands to Griffin: who called, what they said they wanted (the
-- provider's speech-to-text of their answer) and what Griffin did with the call. Adds a table only.
-- -----------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS call_log (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    call_sid    TEXT NOT NULL,
    from_number TEXT NOT NULL DEFAULT '',
    outcome     TEXT NOT NULL CHECK (outcome IN ('forwarded', 'screening', 'message', 'no_answer')),
    message     TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, call_sid)
);

CREATE INDEX IF NOT EXISTS idx_call_log_user_created ON call_log (user_id, created_at DESC);

ALTER TABLE call_log ENABLE ROW LEVEL SECURITY;
ALTER TABLE call_log FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS call_log_user_policy ON call_log;
CREATE POLICY call_log_user_policy ON call_log FOR ALL
    USING (auth.uid() = user_id) WITH CHECK (auth.uid() = user_id);

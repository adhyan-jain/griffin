-- -----------------------------------------------------------------------------
-- 000009: DSA coach
--
-- The coach hands out a few practice problems a day from a fixed list, checks that they were solved, and brings
-- a problem back a week after it was missed. dsa_items holds one row per problem per owner; dsa_settings holds
-- the owner's pace, time zone and LeetCode user name (used only to read public accepted submissions).
-- Adds tables only, so the previous binaries keep working until the new ones are deployed.
-- -----------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS dsa_settings (
    user_id           UUID PRIMARY KEY REFERENCES profiles(id) ON DELETE CASCADE,
    daily_count       INT  NOT NULL DEFAULT 2 CHECK (daily_count BETWEEN 1 AND 10),
    timezone          TEXT NOT NULL DEFAULT 'Asia/Kolkata',
    leetcode_username TEXT NOT NULL DEFAULT '',
    nag_from_hour     INT  NOT NULL DEFAULT 18 CHECK (nag_from_hour BETWEEN 0 AND 23),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS dsa_items (
    user_id     UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    slug        TEXT NOT NULL,
    title       TEXT NOT NULL,
    topic       TEXT NOT NULL DEFAULT '',
    difficulty  TEXT NOT NULL DEFAULT '',
    ord         INT  NOT NULL,
    state       TEXT NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'assigned', 'solved', 'failed')),
    assigned_on DATE,
    due_on      DATE,           -- for a failed item: the first day it may be handed out again
    attempts    INT  NOT NULL DEFAULT 0,
    solved_at   TIMESTAMPTZ,
    PRIMARY KEY (user_id, slug)
);

CREATE INDEX IF NOT EXISTS idx_dsa_items_user_state ON dsa_items (user_id, state);

ALTER TABLE dsa_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE dsa_settings FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS dsa_settings_user_policy ON dsa_settings;
CREATE POLICY dsa_settings_user_policy ON dsa_settings FOR ALL
    USING (auth.uid() = user_id) WITH CHECK (auth.uid() = user_id);

ALTER TABLE dsa_items ENABLE ROW LEVEL SECURITY;
ALTER TABLE dsa_items FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS dsa_items_user_policy ON dsa_items;
CREATE POLICY dsa_items_user_policy ON dsa_items FOR ALL
    USING (auth.uid() = user_id) WITH CHECK (auth.uid() = user_id);

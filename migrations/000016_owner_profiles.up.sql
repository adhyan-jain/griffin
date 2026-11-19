-- -----------------------------------------------------------------------------
-- 000016: Versioned canonical owner profile + verified facts
-- -----------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS owner_profiles (
    user_id    UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    version    INT NOT NULL,
    data       JSONB NOT NULL,
    source     TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, version)
);

ALTER TABLE owner_profiles ENABLE ROW LEVEL SECURITY;
ALTER TABLE owner_profiles FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS owner_profiles_user_policy ON owner_profiles;
CREATE POLICY owner_profiles_user_policy ON owner_profiles FOR ALL USING (auth.uid() = user_id) WITH CHECK (auth.uid() = user_id);

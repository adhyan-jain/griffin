-- -----------------------------------------------------------------------------
-- 000008: Owner aliases ("mummy" means "mom")
--
-- One row per alias per owner. The alias is stored lower-cased and the server rewrites it to the target in what
-- the owner says or types, so every client (phone, web, laptop) understands the same names.
-- Adds a table only, so the previous binaries keep working until the new ones are deployed.
-- -----------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS contact_aliases (
    user_id    UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    alias      TEXT NOT NULL CHECK (char_length(alias) BETWEEN 1 AND 40),
    target     TEXT NOT NULL CHECK (char_length(target) BETWEEN 1 AND 80),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, alias)
);

ALTER TABLE contact_aliases ENABLE ROW LEVEL SECURITY;
ALTER TABLE contact_aliases FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS contact_aliases_user_policy ON contact_aliases;
CREATE POLICY contact_aliases_user_policy ON contact_aliases
    FOR ALL
    USING (auth.uid() = user_id)
    WITH CHECK (auth.uid() = user_id);

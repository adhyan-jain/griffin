-- -----------------------------------------------------------------------------
-- 000006: The owner's permission policy (PRD 7.16, ADR-079)
--
-- One row per owner. `policy` is validated by internal/policy before it is stored; `version` lets
-- two devices edit without silently overwriting each other (an update must name the version it read).
-- Adds a table only, so the previous binaries keep working until the new ones are deployed.
-- -----------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS permission_policies (
    user_id    UUID PRIMARY KEY REFERENCES profiles(id) ON DELETE CASCADE,
    policy     JSONB NOT NULL,
    version    INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE permission_policies ENABLE ROW LEVEL SECURITY;
ALTER TABLE permission_policies FORCE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS permission_policies_user_policy ON permission_policies;
CREATE POLICY permission_policies_user_policy ON permission_policies
    FOR ALL
    USING (auth.uid() = user_id)
    WITH CHECK (auth.uid() = user_id);

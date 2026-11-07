-- -----------------------------------------------------------------------------
-- 000005: Revert execution hosts
-- -----------------------------------------------------------------------------

DROP INDEX IF EXISTS idx_tasks_requires;
DROP POLICY IF EXISTS hosts_user_policy ON hosts;
DROP TABLE IF EXISTS hosts;

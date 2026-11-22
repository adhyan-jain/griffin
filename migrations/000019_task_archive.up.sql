-- -----------------------------------------------------------------------------
-- 000019: Archive minor tasks.
-- An archived task is hidden from the normal task list but kept (and restorable) until the owner deletes it.
-- The rule that archives old, finished, minor tasks lives in code (domain.AutoArchiveIDs).
-- -----------------------------------------------------------------------------

ALTER TABLE tasks ADD COLUMN IF NOT EXISTS archived_at TIMESTAMPTZ NULL;
CREATE INDEX IF NOT EXISTS idx_tasks_user_archived ON tasks (user_id, archived_at);

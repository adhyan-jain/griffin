DROP INDEX IF EXISTS idx_tasks_user_archived;
ALTER TABLE tasks DROP COLUMN IF EXISTS archived_at;

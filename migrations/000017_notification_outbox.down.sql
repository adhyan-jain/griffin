DROP INDEX IF EXISTS idx_notifications_user_seq;
DROP INDEX IF EXISTS idx_notifications_user_dedupe;
ALTER TABLE notifications DROP COLUMN IF EXISTS acked_at;
ALTER TABLE notifications DROP COLUMN IF EXISTS dedupe_key;
ALTER TABLE notifications DROP COLUMN IF EXISTS entity_id;
ALTER TABLE notifications DROP COLUMN IF EXISTS kind;
ALTER TABLE notifications DROP COLUMN IF EXISTS seq;

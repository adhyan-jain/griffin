-- -----------------------------------------------------------------------------
-- 000017: The notifications table becomes an outbox. Each event the owner should hear about is written once
-- (dedupe_key is unique per owner); the phone reads rows after a cursor (seq) and acknowledges them, so the
-- same event can never be pushed twice.
-- -----------------------------------------------------------------------------

ALTER TABLE notifications ADD COLUMN IF NOT EXISTS seq BIGSERIAL;
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS kind VARCHAR(32) NOT NULL DEFAULT 'task';
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS entity_id UUID;
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS dedupe_key TEXT;
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS acked_at TIMESTAMPTZ;

CREATE UNIQUE INDEX IF NOT EXISTS idx_notifications_user_dedupe ON notifications (user_id, dedupe_key) WHERE dedupe_key IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_notifications_user_seq ON notifications (user_id, seq);

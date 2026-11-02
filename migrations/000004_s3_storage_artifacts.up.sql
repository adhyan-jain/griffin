-- -----------------------------------------------------------------------------
-- 000004: S3 Object Storage Integration & Artifact Metadata Extension
-- -----------------------------------------------------------------------------

ALTER TABLE artifacts
    ADD COLUMN IF NOT EXISTS storage_provider VARCHAR(50) NOT NULL DEFAULT 'S3',
    ADD COLUMN IF NOT EXISTS bucket VARCHAR(255),
    ADD COLUMN IF NOT EXISTS object_key TEXT,
    ADD COLUMN IF NOT EXISTS filename VARCHAR(255),
    ADD COLUMN IF NOT EXISTS checksum VARCHAR(128);

-- Index for object key lookups and deduplication within a bucket
CREATE INDEX IF NOT EXISTS idx_artifacts_object_key ON artifacts(bucket, object_key)
WHERE object_key IS NOT NULL;

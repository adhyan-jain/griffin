-- -----------------------------------------------------------------------------
-- 000004: Revert S3 Object Storage Integration & Artifact Metadata Extension
-- -----------------------------------------------------------------------------

DROP INDEX IF EXISTS idx_artifacts_object_key;

ALTER TABLE artifacts
    DROP COLUMN IF EXISTS checksum,
    DROP COLUMN IF EXISTS filename,
    DROP COLUMN IF EXISTS object_key,
    DROP COLUMN IF EXISTS bucket,
    DROP COLUMN IF EXISTS storage_provider;

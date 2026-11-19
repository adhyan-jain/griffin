-- 000012: Add rich attempt telemetry fields to task_attempts: planned vs actual provider,
-- exact model ID, exit code, termination reason, fallback reason, and usage JSONB.

ALTER TABLE task_attempts
    ADD COLUMN IF NOT EXISTS planned_provider VARCHAR(50),
    ADD COLUMN IF NOT EXISTS actual_provider VARCHAR(50),
    ADD COLUMN IF NOT EXISTS model VARCHAR(100),
    ADD COLUMN IF NOT EXISTS exit_code INT,
    ADD COLUMN IF NOT EXISTS termination_reason VARCHAR(100),
    ADD COLUMN IF NOT EXISTS fallback_reason TEXT,
    ADD COLUMN IF NOT EXISTS fallback_stage VARCHAR(50),
    ADD COLUMN IF NOT EXISTS usage JSONB;

UPDATE task_attempts
SET actual_provider = 'unknown',
    planned_provider = 'unknown',
    model = 'unknown'
WHERE actual_provider IS NULL;

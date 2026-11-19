ALTER TABLE task_attempts
    DROP COLUMN IF EXISTS planned_provider,
    DROP COLUMN IF EXISTS actual_provider,
    DROP COLUMN IF EXISTS model,
    DROP COLUMN IF EXISTS exit_code,
    DROP COLUMN IF EXISTS termination_reason,
    DROP COLUMN IF EXISTS fallback_reason,
    DROP COLUMN IF EXISTS fallback_stage,
    DROP COLUMN IF EXISTS usage;

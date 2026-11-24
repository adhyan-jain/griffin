ALTER TABLE evidence_items DROP CONSTRAINT IF EXISTS evidence_items_run_dedupe_uniq;
ALTER TABLE evidence_items DROP COLUMN IF EXISTS dedupe_key;

ALTER TABLE research_sources DROP CONSTRAINT IF EXISTS research_sources_run_canonical_uniq;

UPDATE research_steps SET status = 'completed' WHERE status = 'done';
ALTER TABLE research_steps DROP CONSTRAINT IF EXISTS research_steps_status_check;
ALTER TABLE research_steps ADD CONSTRAINT research_steps_status_check
    CHECK (status IN ('pending', 'running', 'completed', 'failed', 'skipped'));
ALTER TABLE research_steps DROP CONSTRAINT IF EXISTS research_steps_run_step_uniq;

ALTER TABLE research_runs DROP CONSTRAINT IF EXISTS research_runs_stop_reason_check;
ALTER TABLE research_runs
    DROP COLUMN IF EXISTS revision,
    DROP COLUMN IF EXISTS next_step_index,
    DROP COLUMN IF EXISTS stop_reason,
    DROP COLUMN IF EXISTS seen_urls,
    DROP COLUMN IF EXISTS pending_urls,
    DROP COLUMN IF EXISTS executed_queries,
    DROP COLUMN IF EXISTS pending_queries,
    DROP COLUMN IF EXISTS round;

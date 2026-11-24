-- -----------------------------------------------------------------------------
-- 000024: Durable, resumable research runs.
-- A run is a state machine any worker can advance one step at a time: its phase (status), round, query and URL
-- queues, executed-query set and stop reason are stored on the run. Steps, sources and evidence get uniqueness
-- keys so a duplicate claim or a crash mid-step cannot create duplicates.
-- -----------------------------------------------------------------------------

ALTER TABLE research_runs
    ADD COLUMN IF NOT EXISTS round            INT         NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS pending_queries  JSONB       NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS executed_queries JSONB       NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS pending_urls     JSONB       NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS seen_urls        JSONB       NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS stop_reason      TEXT        NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS next_step_index  INT         NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS revision         BIGINT      NOT NULL DEFAULT 0;

ALTER TABLE research_runs DROP CONSTRAINT IF EXISTS research_runs_stop_reason_check;
ALTER TABLE research_runs ADD CONSTRAINT research_runs_stop_reason_check CHECK (stop_reason IN (
    '', 'answered', 'exhausted', 'budget_searches', 'budget_pages', 'budget_model_calls',
    'budget_time', 'cancelled', 'failed'
));

-- Steps: one row per (run, step_index); 'done' joins the allowed statuses ('completed' stays for older rows).
DELETE FROM research_steps s USING research_steps d
 WHERE s.run_id = d.run_id AND s.step_index = d.step_index
   AND (s.created_at, s.id) > (d.created_at, d.id);
ALTER TABLE research_steps DROP CONSTRAINT IF EXISTS research_steps_run_step_uniq;
ALTER TABLE research_steps ADD CONSTRAINT research_steps_run_step_uniq UNIQUE (run_id, step_index);
ALTER TABLE research_steps DROP CONSTRAINT IF EXISTS research_steps_status_check;
ALTER TABLE research_steps ADD CONSTRAINT research_steps_status_check
    CHECK (status IN ('pending', 'running', 'completed', 'done', 'failed', 'skipped'));

-- Sources: a source row exists only for a fetched page, once per (run, canonical URL).
UPDATE research_sources SET canonical_url = url WHERE canonical_url = '';

WITH ranked AS (
    SELECT id, first_value(id) OVER (PARTITION BY run_id, canonical_url ORDER BY retrieved_at, id) AS keep
      FROM research_sources WHERE run_id IS NOT NULL
)
UPDATE evidence_items e SET source_id = r.keep FROM ranked r WHERE e.source_id = r.id AND r.id <> r.keep;

WITH ranked AS (
    SELECT id, first_value(id) OVER (PARTITION BY run_id, canonical_url ORDER BY retrieved_at, id) AS keep
      FROM research_sources WHERE run_id IS NOT NULL
)
UPDATE entity_observations o SET source_id = r.keep FROM ranked r WHERE o.source_id = r.id AND r.id <> r.keep;

WITH ranked AS (
    SELECT id, first_value(id) OVER (PARTITION BY run_id, canonical_url ORDER BY retrieved_at, id) AS keep
      FROM research_sources WHERE run_id IS NOT NULL
)
DELETE FROM research_sources s USING ranked r WHERE s.id = r.id AND r.id <> r.keep;

ALTER TABLE research_sources DROP CONSTRAINT IF EXISTS research_sources_run_canonical_uniq;
ALTER TABLE research_sources ADD CONSTRAINT research_sources_run_canonical_uniq UNIQUE (run_id, canonical_url);

-- Evidence: the same claim and quote from the same page is stored once per run.
ALTER TABLE evidence_items ADD COLUMN IF NOT EXISTS dedupe_key TEXT NULL;
ALTER TABLE evidence_items DROP CONSTRAINT IF EXISTS evidence_items_run_dedupe_uniq;
ALTER TABLE evidence_items ADD CONSTRAINT evidence_items_run_dedupe_uniq UNIQUE (run_id, dedupe_key);

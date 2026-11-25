-- -----------------------------------------------------------------------------
-- 000023: General-purpose Deep Research Engine and Knowledge Graph Persistence.
-- Supports durable multi-phase research runs, structured evidence items with
-- provenance and confidence, canonical entities, aliases, graph relationships,
-- and temporal observations.
-- -----------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS research_runs (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id               UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    playbook              TEXT NOT NULL DEFAULT 'generic',
    objective             TEXT NOT NULL,
    depth                 TEXT NOT NULL DEFAULT 'standard' CHECK (depth IN ('quick', 'standard', 'deep', 'comprehensive')),
    status                TEXT NOT NULL DEFAULT 'REQUEST_RECEIVED' CHECK (status IN (
                            'REQUEST_RECEIVED', 'CONTEXT_LOADING', 'OBJECTIVE_ANALYSIS',
                            'RESEARCH_PLANNING', 'SOURCE_DISCOVERY', 'INFORMATION_RETRIEVAL',
                            'CONTENT_INSPECTION', 'EXTRACTION', 'EVIDENCE_VALIDATION',
                            'GAP_ANALYSIS', 'FOLLOW_UP_RESEARCH', 'SYNTHESIS',
                            'PERSISTENCE', 'COMPLETED', 'PAUSED', 'CANCELLED', 'FAILED'
                          )),
    plan                  JSONB NOT NULL DEFAULT '{}'::jsonb,
    context_data          JSONB NOT NULL DEFAULT '{}'::jsonb,
    constraints_list      JSONB NOT NULL DEFAULT '[]'::jsonb,
    budget_config         JSONB NOT NULL DEFAULT '{}'::jsonb,
    progress_metrics      JSONB NOT NULL DEFAULT '{}'::jsonb,
    findings              JSONB NOT NULL DEFAULT '[]'::jsonb,
    unresolved_questions  JSONB NOT NULL DEFAULT '[]'::jsonb,
    report_markdown       TEXT NOT NULL DEFAULT '',
    report_structured     JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_message         TEXT NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at          TIMESTAMPTZ NULL
);
CREATE INDEX IF NOT EXISTS idx_research_runs_user_status ON research_runs (user_id, status);
CREATE INDEX IF NOT EXISTS idx_research_runs_created ON research_runs (user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS research_steps (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id        UUID NOT NULL REFERENCES research_runs(id) ON DELETE CASCADE,
    user_id       UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    step_index    INT NOT NULL,
    phase         TEXT NOT NULL,
    action        TEXT NOT NULL,
    input_data    JSONB NOT NULL DEFAULT '{}'::jsonb,
    output_data   JSONB NOT NULL DEFAULT '{}'::jsonb,
    duration_ms   INT NOT NULL DEFAULT 0,
    status        TEXT NOT NULL CHECK (status IN ('pending', 'running', 'completed', 'failed', 'skipped')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_research_steps_run ON research_steps (run_id, step_index);

CREATE TABLE IF NOT EXISTS research_sources (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    run_id           UUID NULL REFERENCES research_runs(id) ON DELETE SET NULL,
    url              TEXT NOT NULL,
    canonical_url    TEXT NOT NULL DEFAULT '',
    title            TEXT NOT NULL DEFAULT '',
    domain           TEXT NOT NULL DEFAULT '',
    source_type      TEXT NOT NULL DEFAULT 'unverified_web' CHECK (source_type IN (
                       'official_ats', 'official_career_page', 'company_announcement',
                       'government_regulatory', 'accelerator_directory', 'credible_publication',
                       'public_profile', 'community_discussion', 'unverified_web', 'internal_db'
                     )),
    reliability_tier TEXT NOT NULL DEFAULT 'unverified' CHECK (reliability_tier IN (
                       'primary_official', 'high_authority', 'secondary_credible', 'community_user', 'unverified'
                     )),
    content_hash     TEXT NULL,
    raw_text         TEXT NULL,
    retrieved_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_research_sources_user_url ON research_sources (user_id, url);
CREATE INDEX IF NOT EXISTS idx_research_sources_domain ON research_sources (user_id, domain);

CREATE TABLE IF NOT EXISTS canonical_entities (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    kind          TEXT NOT NULL CHECK (kind IN ('company', 'person', 'job', 'institution', 'technology', 'topic')),
    canonical_key TEXT NOT NULL,
    name          TEXT NOT NULL,
    summary       TEXT NOT NULL DEFAULT '',
    metadata      JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, kind, canonical_key)
);
CREATE INDEX IF NOT EXISTS idx_canonical_entities_user_kind ON canonical_entities (user_id, kind);

CREATE TABLE IF NOT EXISTS entity_aliases (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_id  UUID NOT NULL REFERENCES canonical_entities(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    alias      TEXT NOT NULL,
    kind       TEXT NOT NULL DEFAULT 'name' CHECK (kind IN ('name', 'domain', 'handle', 'ticker', 'legal_name')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (entity_id, alias)
);
CREATE INDEX IF NOT EXISTS idx_entity_aliases_lookup ON entity_aliases (user_id, alias);

CREATE TABLE IF NOT EXISTS evidence_items (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id           UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    run_id            UUID NULL REFERENCES research_runs(id) ON DELETE SET NULL,
    source_id         UUID NULL REFERENCES research_sources(id) ON DELETE SET NULL,
    entity_id         UUID NULL REFERENCES canonical_entities(id) ON DELETE SET NULL,
    claim             TEXT NOT NULL,
    extracted_value   TEXT NOT NULL DEFAULT '',
    excerpt_quote     TEXT NOT NULL DEFAULT '',
    source_url        TEXT NOT NULL DEFAULT '',
    source_domain     TEXT NOT NULL DEFAULT '',
    source_type       TEXT NOT NULL DEFAULT 'unverified_web',
    publication_date  TIMESTAMPTZ NULL,
    retrieved_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    verified_at       TIMESTAMPTZ NULL,
    reliability       TEXT NOT NULL DEFAULT 'secondary_credible',
    confidence        INT NOT NULL DEFAULT 50 CHECK (confidence BETWEEN 0 AND 100),
    status            TEXT NOT NULL DEFAULT 'discovered' CHECK (status IN (
                        'discovered', 'inspected', 'supported', 'cross_verified',
                        'conflicting', 'outdated', 'unverified', 'rejected'
                      )),
    extraction_method TEXT NOT NULL DEFAULT 'model',
    model_used        TEXT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_evidence_user_claim ON evidence_items (user_id, claim);
CREATE INDEX IF NOT EXISTS idx_evidence_entity ON evidence_items (user_id, entity_id);
CREATE INDEX IF NOT EXISTS idx_evidence_status ON evidence_items (user_id, status);

CREATE TABLE IF NOT EXISTS entity_relationships (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    subject_id  UUID NOT NULL REFERENCES canonical_entities(id) ON DELETE CASCADE,
    predicate   TEXT NOT NULL CHECK (predicate IN (
                  'works_at', 'studied_at', 'operates_in', 'has_job',
                  'competes_with', 'discovered_by', 'uses_technology',
                  'funded_by', 'founded_by'
                )),
    object_id   UUID NOT NULL REFERENCES canonical_entities(id) ON DELETE CASCADE,
    evidence_id UUID NULL REFERENCES evidence_items(id) ON DELETE SET NULL,
    confidence  INT NOT NULL DEFAULT 80 CHECK (confidence BETWEEN 0 AND 100),
    valid_from  TIMESTAMPTZ NULL,
    valid_to    TIMESTAMPTZ NULL,
    metadata    JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, subject_id, predicate, object_id)
);
CREATE INDEX IF NOT EXISTS idx_entity_relationships_subj ON entity_relationships (user_id, subject_id, predicate);
CREATE INDEX IF NOT EXISTS idx_entity_relationships_obj ON entity_relationships (user_id, object_id, predicate);

CREATE TABLE IF NOT EXISTS entity_observations (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    entity_id   UUID NOT NULL REFERENCES canonical_entities(id) ON DELETE CASCADE,
    property    TEXT NOT NULL,
    value       TEXT NOT NULL,
    source_id   UUID NULL REFERENCES research_sources(id) ON DELETE SET NULL,
    evidence_id UUID NULL REFERENCES evidence_items(id) ON DELETE SET NULL,
    observed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_entity_observations_entity ON entity_observations (entity_id, property, observed_at DESC);

-- Enable Row Level Security on all research and knowledge tables
DO $$ DECLARE tbl text; BEGIN
    FOREACH tbl IN ARRAY ARRAY[
        'research_runs', 'research_steps', 'research_sources',
        'canonical_entities', 'entity_aliases', 'evidence_items',
        'entity_relationships', 'entity_observations'
    ] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', tbl);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', tbl);
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', tbl || '_user_policy', tbl);
        EXECUTE format('CREATE POLICY %I ON %I FOR ALL USING (auth.uid() = user_id) WITH CHECK (auth.uid() = user_id)', tbl || '_user_policy', tbl);
    END LOOP;
END $$;

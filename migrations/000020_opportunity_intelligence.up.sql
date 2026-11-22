-- -----------------------------------------------------------------------------
-- 000020: Opportunity intelligence, stage 1: canonical employers, ATS boards, scan history, listing identity,
-- change snapshots and evidence.
-- job_discoveries is extended, not replaced: every existing row stays, as an aggregator lead (it was read from
-- Internshala, Unstop or InternDock, never from the employer), with its pipeline stage untouched.
-- -----------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS companies (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    normalized_name TEXT NOT NULL,
    domain          TEXT NULL,
    kind            TEXT NOT NULL DEFAULT 'unknown' CHECK (kind IN ('startup', 'enterprise', 'lab', 'foundation', 'unknown')),
    discovered_via  TEXT NOT NULL DEFAULT '',
    evidence_url    TEXT NULL,
    status          TEXT NOT NULL DEFAULT 'candidate' CHECK (status IN ('candidate', 'active', 'ignored')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, normalized_name)
);

CREATE TABLE IF NOT EXISTS ats_boards (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id              UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    company_id           UUID NULL REFERENCES companies(id) ON DELETE SET NULL,
    provider             TEXT NOT NULL CHECK (provider IN ('greenhouse', 'lever', 'lever_eu', 'ashby', 'smartrecruiters', 'workday', 'recruitee', 'workable', 'careers_jsonld')),
    token_or_url         TEXT NOT NULL,
    status               TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'broken', 'retired')),
    last_scan_at         TIMESTAMPTZ NULL,
    last_ok_at           TIMESTAMPTZ NULL,
    consecutive_failures INT NOT NULL DEFAULT 0,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, provider, token_or_url)
);

-- One row per scan of one source target: the scan history and the source health. A failed or partial scan is
-- recorded as such and is never read as "no openings".
CREATE TABLE IF NOT EXISTS source_scans (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    source      TEXT NOT NULL,
    target      TEXT NOT NULL,
    status      TEXT NOT NULL CHECK (status IN ('ok', 'partial', 'failed')),
    jobs_seen   INT NOT NULL DEFAULT 0,
    pages       INT NOT NULL DEFAULT 0,
    error       TEXT NULL,
    started_at  TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_source_scans_target ON source_scans (user_id, source, target, finished_at DESC);

ALTER TABLE job_discoveries ADD COLUMN IF NOT EXISTS company_id UUID NULL REFERENCES companies(id) ON DELETE SET NULL;
ALTER TABLE job_discoveries ADD COLUMN IF NOT EXISTS ats_board_id UUID NULL REFERENCES ats_boards(id) ON DELETE SET NULL;
ALTER TABLE job_discoveries ADD COLUMN IF NOT EXISTS source_kind TEXT NOT NULL DEFAULT 'aggregator';
ALTER TABLE job_discoveries ADD COLUMN IF NOT EXISTS verification TEXT NOT NULL DEFAULT 'lead';
ALTER TABLE job_discoveries ADD COLUMN IF NOT EXISTS canonical_url TEXT NULL;
ALTER TABLE job_discoveries ADD COLUMN IF NOT EXISTS employer_url TEXT NULL;
-- employer_key / role_key / source_target: the identity keys (domain.NormalizeEmployer, domain.NormalizeRole) and
-- the scan target the listing was last read from. '' means not computed yet; ingest fills them in.
ALTER TABLE job_discoveries ADD COLUMN IF NOT EXISTS employer_key TEXT NOT NULL DEFAULT '';
ALTER TABLE job_discoveries ADD COLUMN IF NOT EXISTS role_key TEXT NOT NULL DEFAULT '';
ALTER TABLE job_discoveries ADD COLUMN IF NOT EXISTS source_target TEXT NOT NULL DEFAULT '';
ALTER TABLE job_discoveries ADD COLUMN IF NOT EXISTS description_hash TEXT NULL;
ALTER TABLE job_discoveries ADD COLUMN IF NOT EXISTS compensation_text TEXT NULL;
ALTER TABLE job_discoveries ADD COLUMN IF NOT EXISTS deadline TIMESTAMPTZ NULL;
ALTER TABLE job_discoveries ADD COLUMN IF NOT EXISTS first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
ALTER TABLE job_discoveries ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMPTZ NULL;
ALTER TABLE job_discoveries ADD COLUMN IF NOT EXISTS closed_at TIMESTAMPTZ NULL;
ALTER TABLE job_discoveries ADD COLUMN IF NOT EXISTS reopened_count INT NOT NULL DEFAULT 0;

-- Existing rows keep what is truthfully known: first seen when created, last seen when last checked (NULL when
-- never), closed when their stage last changed.
UPDATE job_discoveries SET first_seen_at = created_at;
UPDATE job_discoveries SET last_seen_at = last_checked_at WHERE last_checked_at IS NOT NULL;
UPDATE job_discoveries SET closed_at = updated_at WHERE stage = 'closed' AND closed_at IS NULL;

DO $$ BEGIN
    ALTER TABLE job_discoveries ADD CONSTRAINT job_discoveries_source_kind_check
        CHECK (source_kind IN ('employer_ats', 'employer_site', 'aggregator'));
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
DO $$ BEGIN
    ALTER TABLE job_discoveries ADD CONSTRAINT job_discoveries_verification_check
        CHECK (verification IN ('lead', 'employer_verified'));
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
-- An aggregator record can never be employer-verified: only the employer's own posting can be.
DO $$ BEGIN
    ALTER TABLE job_discoveries ADD CONSTRAINT job_discoveries_verified_needs_employer_check
        CHECK (verification = 'lead' OR source_kind IN ('employer_ats', 'employer_site'));
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

CREATE INDEX IF NOT EXISTS idx_job_discoveries_identity ON job_discoveries (user_id, employer_key, role_key);
CREATE INDEX IF NOT EXISTS idx_job_discoveries_canonical_url ON job_discoveries (user_id, canonical_url) WHERE canonical_url IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_job_discoveries_external ON job_discoveries (user_id, source, external_id) WHERE external_id <> '';
CREATE INDEX IF NOT EXISTS idx_job_discoveries_target ON job_discoveries (user_id, source, source_target) WHERE closed_at IS NULL;

-- One row each time something about a listing changed (and one when it was first seen), so its history survives.
CREATE TABLE IF NOT EXISTS job_snapshots (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id           UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    listing_id        UUID NOT NULL REFERENCES job_discoveries(id) ON DELETE CASCADE,
    captured_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    description_hash  TEXT NULL,
    compensation_text TEXT NULL,
    deadline          TIMESTAMPTZ NULL,
    title             TEXT NOT NULL DEFAULT '',
    location          TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_job_snapshots_listing ON job_snapshots (listing_id, captured_at DESC);

-- A claim about a listing (stipend, eligibility, deadline) with the exact words and page that support it.
CREATE TABLE IF NOT EXISTS listing_evidence (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    listing_id  UUID NOT NULL REFERENCES job_discoveries(id) ON DELETE CASCADE,
    claim       TEXT NOT NULL,
    value       TEXT NOT NULL,
    quote       TEXT NOT NULL,
    source_url  TEXT NOT NULL,
    captured_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_listing_evidence_listing ON listing_evidence (listing_id, captured_at DESC);

DO $$ DECLARE tbl text; BEGIN
    FOREACH tbl IN ARRAY ARRAY['companies', 'ats_boards', 'source_scans', 'job_snapshots', 'listing_evidence'] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', tbl);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', tbl);
        EXECUTE format('DROP POLICY IF EXISTS %I ON %I', tbl || '_user_policy', tbl);
        EXECUTE format('CREATE POLICY %I ON %I FOR ALL USING (auth.uid() = user_id) WITH CHECK (auth.uid() = user_id)', tbl || '_user_policy', tbl);
    END LOOP;
END $$;

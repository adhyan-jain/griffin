-- -----------------------------------------------------------------------------
-- 000022: Application preparation. One package per listing: the resume, the owner's projects that match the posting
-- (each with the posting words that matched), a fact-checked cover letter, answers to the questions the posting asks,
-- the documents it requires, its deadline and what only the owner can supply. A package is only a draft for the owner
-- to approve through the approvals table (action type 'application_package'); nothing in it is ever sent from here.
-- Every claim in cover_letter and answers was checked against the owner's profile and the posting before it was
-- stored; a draft that failed the check is not stored and its gap is listed in missing_information.
-- Self-contained: depends only on profiles, job_discoveries and tasks, which exist since the first migrations.
-- -----------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS application_packages (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    listing_id          UUID NOT NULL REFERENCES job_discoveries(id) ON DELETE CASCADE,
    resume_file         TEXT NOT NULL DEFAULT '',
    role_class          TEXT NOT NULL DEFAULT 'unknown',
    selected_projects   JSONB NOT NULL DEFAULT '[]'::jsonb,
    cover_letter        TEXT NOT NULL DEFAULT '',
    answers             JSONB NOT NULL DEFAULT '[]'::jsonb,
    required_documents  JSONB NOT NULL DEFAULT '[]'::jsonb,
    deadline            TIMESTAMPTZ NULL,
    missing_information JSONB NOT NULL DEFAULT '[]'::jsonb,
    evidence            JSONB NOT NULL DEFAULT '[]'::jsonb,
    status              TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'needs_owner', 'ready_for_approval', 'approved', 'rejected')),
    approval_id         UUID NULL,
    task_id             UUID NULL,
    description_hash    TEXT NULL,
    profile_hash        TEXT NOT NULL DEFAULT '',
    model_used          TEXT NULL,
    model_calls         INT NOT NULL DEFAULT 0,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (listing_id)
);
CREATE INDEX IF NOT EXISTS idx_application_packages_user_status ON application_packages (user_id, status);

ALTER TABLE application_packages ENABLE ROW LEVEL SECURITY;
ALTER TABLE application_packages FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS application_packages_user_policy ON application_packages;
CREATE POLICY application_packages_user_policy ON application_packages FOR ALL USING (auth.uid() = user_id) WITH CHECK (auth.uid() = user_id);

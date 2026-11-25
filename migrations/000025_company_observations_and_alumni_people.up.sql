-- -----------------------------------------------------------------------------
-- 000025: Research attached to the existing Companies and Alumni data.
-- Companies: typed, dated observations keyed to a row of the existing companies table (history is kept: a new
-- value adds a row, nothing is overwritten). Alumni: person fields on canonical_entities (kind = 'person'): the
-- do-not-contact flag, the contact state the owner moves by hand, graduation year with its evidence, and the
-- profile URL that identifies a person (two people with one name are two people).
-- -----------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS company_observations (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    company_id   UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    property     TEXT NOT NULL CHECK (property IN (
                   'funding', 'revenue', 'profitability', 'advertised_pay', 'employee_reported_pay',
                   'internship_stipend', 'hiring_signal', 'product', 'market')),
    value        TEXT NOT NULL,
    evidence_id  UUID NULL REFERENCES evidence_items(id) ON DELETE SET NULL,
    source_url   TEXT NOT NULL DEFAULT '',
    claim_origin TEXT NOT NULL DEFAULT 'unknown' CHECK (claim_origin IN ('company_reported', 'independent', 'unknown')),
    observed_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (company_id, property, value, source_url)
);
CREATE INDEX IF NOT EXISTS idx_company_observations_company ON company_observations (company_id, property, observed_at DESC);

ALTER TABLE company_observations ENABLE ROW LEVEL SECURITY;
ALTER TABLE company_observations FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS company_observations_user_policy ON company_observations;
CREATE POLICY company_observations_user_policy ON company_observations FOR ALL
    USING (auth.uid() = user_id) WITH CHECK (auth.uid() = user_id);

-- Person fields. They sit on canonical_entities (RLS already on it) and mean something only for kind = 'person'.
ALTER TABLE canonical_entities
    ADD COLUMN IF NOT EXISTS do_not_contact BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS contact_state TEXT NOT NULL DEFAULT 'not_contacted',
    ADD COLUMN IF NOT EXISTS graduation_year INT NULL,
    ADD COLUMN IF NOT EXISTS graduation_year_evidence_id UUID NULL REFERENCES evidence_items(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS last_verified_at TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS profile_url TEXT NULL;

ALTER TABLE canonical_entities DROP CONSTRAINT IF EXISTS canonical_entities_contact_state_check;
ALTER TABLE canonical_entities ADD CONSTRAINT canonical_entities_contact_state_check CHECK (contact_state IN (
    'not_contacted', 'drafted', 'approved', 'sent_manually', 'replied', 'opted_out'));
ALTER TABLE canonical_entities DROP CONSTRAINT IF EXISTS canonical_entities_graduation_year_check;
ALTER TABLE canonical_entities ADD CONSTRAINT canonical_entities_graduation_year_check CHECK (graduation_year IS NULL OR graduation_year BETWEEN 1950 AND 2100);

CREATE UNIQUE INDEX IF NOT EXISTS uq_canonical_entities_profile_url
    ON canonical_entities (user_id, profile_url) WHERE profile_url IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_canonical_entities_person_filters
    ON canonical_entities (user_id, graduation_year) WHERE kind = 'person';

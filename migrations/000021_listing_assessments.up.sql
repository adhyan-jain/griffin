-- -----------------------------------------------------------------------------
-- 000021: Opportunity intelligence, stage 2: one assessment per listing.
-- The assessment is what Griffin concluded about a listing from its text: what kind of job it is, whether the owner
-- is eligible, what it pays, what skills match and a career-value score with every component explained. A value
-- the text does not give stays NULL / 'unknown'; nothing is guessed. Every model-derived conclusion carries a quote
-- that code verified against the stored description before it was written.
--
-- One row per listing (the current assessment). assessment_key is the description_hash the assessment was made from
-- (or, for an aggregator lead with no description, a hash of its title/company/location/pay), so a model is asked at
-- most once per listing and key: model_key records the key a model call was attempted at, even when its answer was
-- unusable, and model_output keeps the verified conclusions so a rule change can re-score without another call.
-- -----------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS listing_assessments (
    listing_id               UUID PRIMARY KEY REFERENCES job_discoveries(id) ON DELETE CASCADE,
    user_id                  UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    description_hash         TEXT NULL,
    assessment_key           TEXT NOT NULL,
    input_hash               TEXT NOT NULL,
    assessor_version         INT NOT NULL,
    employment_class         TEXT NOT NULL DEFAULT 'unknown' CHECK (employment_class IN ('internship', 'full_time', 'unknown')),
    role_class               TEXT NOT NULL DEFAULT 'unknown' CHECK (role_class IN ('backend', 'frontend', 'fullstack', 'ai_ml', 'research', 'infra', 'devtools', 'data', 'mobile', 'security', 'other_tech', 'non_tech', 'unknown')),
    eligibility              TEXT NOT NULL DEFAULT 'unknown' CHECK (eligibility IN ('confirmed', 'likely', 'unknown', 'ineligible')),
    eligibility_reasons      JSONB NOT NULL DEFAULT '[]'::jsonb,
    location_fit             TEXT NOT NULL DEFAULT 'unknown' CHECK (location_fit IN ('india', 'remote_ok', 'relocation', 'outside', 'unknown')),
    grad_year_fit            TEXT NOT NULL DEFAULT 'unknown' CHECK (grad_year_fit IN ('fit', 'mismatch', 'unknown')),
    compensation_monthly_inr NUMERIC(12, 2) NULL,
    duration                 TEXT NULL,
    skills_matched           JSONB NOT NULL DEFAULT '[]'::jsonb,
    value_score              INT NOT NULL CHECK (value_score BETWEEN 0 AND 100),
    value_breakdown          JSONB NOT NULL DEFAULT '[]'::jsonb,
    value_class              TEXT NOT NULL CHECK (value_class IN ('exceptional', 'strong', 'good', 'exploratory', 'low')),
    assessed_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    model_used               TEXT NULL,
    model_key                TEXT NULL,
    model_output             JSONB NULL
);
CREATE INDEX IF NOT EXISTS idx_listing_assessments_user_score ON listing_assessments (user_id, value_score DESC);
CREATE INDEX IF NOT EXISTS idx_listing_assessments_user_class ON listing_assessments (user_id, value_class);
CREATE INDEX IF NOT EXISTS idx_listing_assessments_user_eligibility ON listing_assessments (user_id, eligibility);

ALTER TABLE listing_assessments ENABLE ROW LEVEL SECURITY;
ALTER TABLE listing_assessments FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS listing_assessments_user_policy ON listing_assessments;
CREATE POLICY listing_assessments_user_policy ON listing_assessments FOR ALL USING (auth.uid() = user_id) WITH CHECK (auth.uid() = user_id);

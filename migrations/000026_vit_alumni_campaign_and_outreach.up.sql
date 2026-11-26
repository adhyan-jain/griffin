-- -----------------------------------------------------------------------------
-- 000026: VIT Alumni Prospecting, ACM Prioritisation, and Startup Intelligence
-- -----------------------------------------------------------------------------

-- 1. Alumni qualification fields on canonical_entities (kind = 'person')
ALTER TABLE canonical_entities
    ADD COLUMN IF NOT EXISTS campus TEXT NOT NULL DEFAULT 'unknown',
    ADD COLUMN IF NOT EXISTS vit_affiliation TEXT NOT NULL DEFAULT 'unconfirmed',
    ADD COLUMN IF NOT EXISTS branch TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS degree TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS graduation_date DATE NULL,
    ADD COLUMN IF NOT EXISTS graduation_eligibility TEXT NOT NULL DEFAULT 'unknown',
    ADD COLUMN IF NOT EXISTS acm_vit_affiliation TEXT NOT NULL DEFAULT 'unknown',
    ADD COLUMN IF NOT EXISTS acm_role TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS acm_tenure TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS acm_source TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS acm_evidence_excerpt TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS normalized_title TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS professional_function TEXT NOT NULL DEFAULT 'unknown',
    ADD COLUMN IF NOT EXISTS seniority TEXT NOT NULL DEFAULT 'unknown',
    ADD COLUMN IF NOT EXISTS classification_confidence INT NOT NULL DEFAULT 50,
    ADD COLUMN IF NOT EXISTS current_company_id UUID NULL REFERENCES companies(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS contactability_status TEXT NOT NULL DEFAULT 'unknown',
    ADD COLUMN IF NOT EXISTS preferred_channel TEXT NOT NULL DEFAULT 'linkedin',
    ADD COLUMN IF NOT EXISTS public_contact_route TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS public_business_email TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS priority_tier TEXT NOT NULL DEFAULT 'REVIEW',
    ADD COLUMN IF NOT EXISTS priority_score INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS priority_rationale TEXT NOT NULL DEFAULT '';

DO $$ BEGIN
    ALTER TABLE canonical_entities ADD CONSTRAINT canonical_entities_campus_check
        CHECK (campus IN ('vellore', 'chennai', 'ap', 'bhopal', 'other', 'unknown'));
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
    ALTER TABLE canonical_entities ADD CONSTRAINT canonical_entities_vit_affiliation_check
        CHECK (vit_affiliation IN ('confirmed_vellore', 'unconfirmed', 'other_campus', 'none'));
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
    ALTER TABLE canonical_entities ADD CONSTRAINT canonical_entities_graduation_eligibility_check
        CHECK (graduation_eligibility IN ('eligible', 'borderline_2022', 'ineligible', 'unknown'));
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
    ALTER TABLE canonical_entities ADD CONSTRAINT canonical_entities_acm_affiliation_check
        CHECK (acm_vit_affiliation IN ('confirmed', 'likely', 'unknown', 'not_confirmed'));
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
    ALTER TABLE canonical_entities ADD CONSTRAINT canonical_entities_professional_function_check
        CHECK (professional_function IN ('technology', 'operations', 'other', 'unknown'));
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
    ALTER TABLE canonical_entities ADD CONSTRAINT canonical_entities_seniority_check
        CHECK (seniority IN ('founder', 'executive', 'lead', 'senior', 'mid', 'entry', 'unknown'));
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
    ALTER TABLE canonical_entities ADD CONSTRAINT canonical_entities_priority_tier_check
        CHECK (priority_tier IN ('P1', 'P2', 'P3', 'P4', 'REVIEW'));
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

CREATE INDEX IF NOT EXISTS idx_canonical_entities_acm_priority
    ON canonical_entities (user_id, priority_tier, acm_vit_affiliation) WHERE kind = 'person';
CREATE INDEX IF NOT EXISTS idx_canonical_entities_campus_grad
    ON canonical_entities (user_id, campus, graduation_year) WHERE kind = 'person';

-- 2. Multi-position employment history
CREATE TABLE IF NOT EXISTS person_employments (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
    person_id     UUID NOT NULL REFERENCES canonical_entities(id) ON DELETE CASCADE,
    company_id    UUID NULL REFERENCES companies(id) ON DELETE SET NULL,
    employer_name TEXT NOT NULL,
    title         TEXT NOT NULL,
    start_date    DATE NULL,
    end_date      DATE NULL,
    is_current    BOOLEAN NOT NULL DEFAULT false,
    location      TEXT NOT NULL DEFAULT '',
    function      TEXT NOT NULL DEFAULT '',
    evidence_id   UUID NULL REFERENCES evidence_items(id) ON DELETE SET NULL,
    confidence    INT NOT NULL DEFAULT 80 CHECK (confidence BETWEEN 0 AND 100),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (person_id, employer_name, title, is_current)
);
CREATE INDEX IF NOT EXISTS idx_person_employments_person ON person_employments (person_id, is_current);

ALTER TABLE person_employments ENABLE ROW LEVEL SECURITY;
ALTER TABLE person_employments FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS person_employments_user_policy ON person_employments;
CREATE POLICY person_employments_user_policy ON person_employments FOR ALL
    USING (auth.uid() = user_id) WITH CHECK (auth.uid() = user_id);

-- Company intelligence is not stored as columns on companies: pay, funding and hiring are dated, sourced rows
-- in company_observations (migration 000025), so no rating can be set by hand.

-- 3. Outreach Campaigns & Target Lifecycle Extension
ALTER TABLE outreach_campaigns
    ADD COLUMN IF NOT EXISTS campaign_type TEXT NOT NULL DEFAULT 'general',
    ADD COLUMN IF NOT EXISTS cohort_cutoff_date DATE NOT NULL DEFAULT '2022-11-26',
    ADD COLUMN IF NOT EXISTS priority_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS included_sources JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS last_research_at TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS metrics JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE outreach_targets
    ADD COLUMN IF NOT EXISTS person_id UUID NULL REFERENCES canonical_entities(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS company_id UUID NULL REFERENCES companies(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS priority_tier TEXT NOT NULL DEFAULT 'REVIEW',
    ADD COLUMN IF NOT EXISTS message_type TEXT NOT NULL DEFAULT 'initial_connection',
    ADD COLUMN IF NOT EXISTS alternative_draft TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS selection_reason TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS personalisation_evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS extended_status TEXT NOT NULL DEFAULT 'DISCOVERED';

DO $$ BEGIN
    ALTER TABLE outreach_targets ADD CONSTRAINT outreach_targets_extended_status_check
        CHECK (extended_status IN (
            'DISCOVERED', 'VERIFIED', 'QUALIFIED', 'PRIORITISED',
            'DRAFTED', 'NEEDS_USER_REVIEW', 'APPROVED', 'SENT_BY_USER',
            'REPLIED', 'FOLLOW_UP', 'CLOSED',
            'DO_NOT_CONTACT', 'OPTED_OUT', 'INVALID_CONTACT',
            'DUPLICATE', 'STALE_INFORMATION', 'NEEDS_RESEARCH'
        ));
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

CREATE INDEX IF NOT EXISTS idx_outreach_targets_person ON outreach_targets (person_id);
CREATE INDEX IF NOT EXISTS idx_outreach_targets_tier ON outreach_targets (campaign_id, priority_tier);

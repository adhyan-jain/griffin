DROP INDEX IF EXISTS idx_canonical_entities_person_filters;
DROP INDEX IF EXISTS uq_canonical_entities_profile_url;
ALTER TABLE canonical_entities DROP CONSTRAINT IF EXISTS canonical_entities_graduation_year_check;
ALTER TABLE canonical_entities DROP CONSTRAINT IF EXISTS canonical_entities_contact_state_check;
ALTER TABLE canonical_entities
    DROP COLUMN IF EXISTS profile_url,
    DROP COLUMN IF EXISTS last_verified_at,
    DROP COLUMN IF EXISTS graduation_year_evidence_id,
    DROP COLUMN IF EXISTS graduation_year,
    DROP COLUMN IF EXISTS contact_state,
    DROP COLUMN IF EXISTS do_not_contact;
DROP TABLE IF EXISTS company_observations;

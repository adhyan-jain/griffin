-- -----------------------------------------------------------------------------
-- 000026: Down migration for VIT Alumni Campaign schema
-- -----------------------------------------------------------------------------

DROP TABLE IF EXISTS person_employments;

ALTER TABLE outreach_targets
    DROP CONSTRAINT IF EXISTS outreach_targets_extended_status_check,
    DROP COLUMN IF EXISTS person_id,
    DROP COLUMN IF EXISTS company_id,
    DROP COLUMN IF EXISTS priority_tier,
    DROP COLUMN IF EXISTS message_type,
    DROP COLUMN IF EXISTS alternative_draft,
    DROP COLUMN IF EXISTS selection_reason,
    DROP COLUMN IF EXISTS personalisation_evidence,
    DROP COLUMN IF EXISTS extended_status;

ALTER TABLE outreach_campaigns
    DROP COLUMN IF EXISTS campaign_type,
    DROP COLUMN IF EXISTS cohort_cutoff_date,
    DROP COLUMN IF EXISTS priority_config,
    DROP COLUMN IF EXISTS included_sources,
    DROP COLUMN IF EXISTS last_research_at,
    DROP COLUMN IF EXISTS metrics;

ALTER TABLE canonical_entities
    DROP CONSTRAINT IF EXISTS canonical_entities_priority_tier_check,
    DROP CONSTRAINT IF EXISTS canonical_entities_seniority_check,
    DROP CONSTRAINT IF EXISTS canonical_entities_professional_function_check,
    DROP CONSTRAINT IF EXISTS canonical_entities_acm_affiliation_check,
    DROP CONSTRAINT IF EXISTS canonical_entities_graduation_eligibility_check,
    DROP CONSTRAINT IF EXISTS canonical_entities_vit_affiliation_check,
    DROP CONSTRAINT IF EXISTS canonical_entities_campus_check,
    DROP COLUMN IF EXISTS campus,
    DROP COLUMN IF EXISTS vit_affiliation,
    DROP COLUMN IF EXISTS branch,
    DROP COLUMN IF EXISTS degree,
    DROP COLUMN IF EXISTS graduation_date,
    DROP COLUMN IF EXISTS graduation_eligibility,
    DROP COLUMN IF EXISTS acm_vit_affiliation,
    DROP COLUMN IF EXISTS acm_role,
    DROP COLUMN IF EXISTS acm_tenure,
    DROP COLUMN IF EXISTS acm_source,
    DROP COLUMN IF EXISTS acm_evidence_excerpt,
    DROP COLUMN IF EXISTS normalized_title,
    DROP COLUMN IF EXISTS professional_function,
    DROP COLUMN IF EXISTS seniority,
    DROP COLUMN IF EXISTS classification_confidence,
    DROP COLUMN IF EXISTS current_company_id,
    DROP COLUMN IF EXISTS contactability_status,
    DROP COLUMN IF EXISTS preferred_channel,
    DROP COLUMN IF EXISTS public_contact_route,
    DROP COLUMN IF EXISTS public_business_email,
    DROP COLUMN IF EXISTS priority_tier,
    DROP COLUMN IF EXISTS priority_score,
    DROP COLUMN IF EXISTS priority_rationale;

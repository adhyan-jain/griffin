-- -----------------------------------------------------------------------------
-- 000018: Verified internship listings.
-- A listing exists only if code read it from an internship site (Internshala, Unstop, InternDock) with its own
-- link. Its stage moves through the application pipeline; it never comes from a model.
-- -----------------------------------------------------------------------------

ALTER TABLE job_discoveries ADD COLUMN IF NOT EXISTS stage TEXT NOT NULL DEFAULT 'verified';
ALTER TABLE job_discoveries ADD COLUMN IF NOT EXISTS location TEXT NOT NULL DEFAULT '';
ALTER TABLE job_discoveries ADD COLUMN IF NOT EXISTS pay TEXT NOT NULL DEFAULT '';
ALTER TABLE job_discoveries ADD COLUMN IF NOT EXISTS posted_at TIMESTAMPTZ;
ALTER TABLE job_discoveries ADD COLUMN IF NOT EXISTS verified_at TIMESTAMPTZ;
ALTER TABLE job_discoveries ADD COLUMN IF NOT EXISTS last_checked_at TIMESTAMPTZ;
ALTER TABLE job_discoveries ADD COLUMN IF NOT EXISTS stage_note TEXT NOT NULL DEFAULT '';
ALTER TABLE job_discoveries ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
DO $$ BEGIN
    ALTER TABLE job_discoveries ADD CONSTRAINT job_discoveries_stage_check
        CHECK (stage IN ('verified', 'drafted', 'approved', 'submitting', 'confirmed', 'needs_you', 'closed'));
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
CREATE INDEX IF NOT EXISTS idx_job_discoveries_user_stage ON job_discoveries (user_id, stage);

-- -----------------------------------------------------------------------------
-- 000014: Drop inbox intelligence, job discovery, assessments & application tracking
-- -----------------------------------------------------------------------------

DROP TABLE IF EXISTS application_submissions CASCADE;
DROP TABLE IF EXISTS job_discoveries CASCADE;
DROP TABLE IF EXISTS assessments CASCADE;
DROP TABLE IF EXISTS email_threads CASCADE;
DROP TABLE IF EXISTS oauth_credentials CASCADE;

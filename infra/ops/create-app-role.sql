-- Least-privilege runtime role for the Lambda API and EC2 worker (instead of the `postgres` admin role).
-- Run ONCE, after `griffin-migrate up`, as `postgres` over the SESSION pooler (needs approval):
--   psql "$MIGRATION_DATABASE_URL" -v app_pw="$GRIFFIN_APP_PW" -f infra/ops/create-app-role.sql
-- BYPASSRLS is required because the backend enforces tenancy in code and sets no per-request
-- `request.jwt.claims`; without it every RLS policy (auth.uid()) would deny the app. The role has
-- DML on `public` only: no DDL, no `auth` schema, no ownership. Migrations keep using `postgres`.
-- Status: IMPLEMENTED_NOT_LIVE_VERIFIED (not yet run against the hosted project).
CREATE ROLE griffin_app LOGIN PASSWORD :'app_pw' BYPASSRLS NOCREATEDB NOCREATEROLE NOINHERIT;
GRANT USAGE ON SCHEMA public TO griffin_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO griffin_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO griffin_app;
ALTER DEFAULT PRIVILEGES FOR ROLE postgres IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO griffin_app;
ALTER DEFAULT PRIVILEGES FOR ROLE postgres IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO griffin_app;
-- Rotation later: ALTER ROLE griffin_app PASSWORD '...';  (see docs/SUPABASE_KEY_ROTATION.md)

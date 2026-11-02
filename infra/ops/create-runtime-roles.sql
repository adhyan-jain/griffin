-- Least-privilege runtime roles. Run ONCE after `griffin-migrate up`, as `postgres`, over the SESSION pooler
-- (requires explicit approval):
--   psql "$MIGRATION_DATABASE_URL" -v app_pw="$GRIFFIN_APP_PW" -v system_pw="$GRIFFIN_SYSTEM_PW" \
--        -f infra/ops/create-runtime-roles.sql
--
-- griffin_app    : user requests. NOSUPERUSER, NOBYPASSRLS => row-level security is ENFORCED. Each request runs in a
--                  transaction that sets auth.uid() with set_config(..., is_local => true) (see internal/database/scope.go).
-- griffin_system : worker, lease reaper, scheduler tick, worker-key routes (cross-tenant by nature). BYPASSRLS, but
--                  NOSUPERUSER, DML only, and reachable only from code paths explicitly marked database.WithSystem(ctx).
-- Neither role can run DDL, owns objects, or can read `auth` tables. Migrations keep using `postgres`.
CREATE ROLE griffin_app    LOGIN PASSWORD :'app_pw'    NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION NOINHERIT;
CREATE ROLE griffin_system LOGIN PASSWORD :'system_pw' NOSUPERUSER BYPASSRLS   NOCREATEDB NOCREATEROLE NOREPLICATION NOINHERIT;

-- Needed so RLS policies can call auth.uid() as the querying role (schema name resolution only; no table grants).
-- On plain Postgres (tests) this grant works. On Supabase the `auth` schema is owned by supabase_admin, so `postgres`
-- cannot grant USAGE on it (the GRANT only warns). There, membership in the platform role `authenticated` (which
-- `postgres` holds WITH ADMIN OPTION and which already has `auth` access) provides exactly that access, and nothing
-- on public tables (those grants were revoked by harden-public-schema.sql).
GRANT USAGE ON SCHEMA auth   TO griffin_app, griffin_system;
DO $$ BEGIN
    IF NOT has_schema_privilege('griffin_app', 'auth', 'USAGE')
       AND EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'authenticated') THEN
        -- PG16+: the grant carries its own INHERIT option (not the member's rolinherit at a later time).
        GRANT authenticated TO griffin_app WITH INHERIT TRUE;
    END IF;
END $$;
GRANT USAGE ON SCHEMA public TO griffin_app, griffin_system;

GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES    IN SCHEMA public TO griffin_app, griffin_system;
GRANT USAGE, SELECT                  ON ALL SEQUENCES IN SCHEMA public TO griffin_app, griffin_system;
-- The migration ledger is read-only for runtime roles (the server only runs a read-only schema check).
REVOKE INSERT, UPDATE, DELETE ON public.schema_migrations FROM griffin_app, griffin_system;

ALTER DEFAULT PRIVILEGES FOR ROLE postgres IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO griffin_app, griffin_system;
ALTER DEFAULT PRIVILEGES FOR ROLE postgres IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO griffin_app, griffin_system;

-- Fixed search_path: no object in another schema can shadow public tables for these roles.
ALTER ROLE griffin_app    SET search_path = public;
ALTER ROLE griffin_system SET search_path = public;
-- Rotation later: ALTER ROLE griffin_app PASSWORD '...';  (docs/SUPABASE_KEY_ROTATION.md)

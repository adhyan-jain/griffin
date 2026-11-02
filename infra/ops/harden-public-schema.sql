-- Run ONCE after `griffin-migrate up`, as `postgres` over the session pooler.
-- Supabase grants ALL on every new public table/sequence/function to `anon` and `authenticated` (the roles behind the
-- publishable key / PostgREST). Griffin's PWA only uses Supabase *Auth*; all data goes through the Go API. So:
--   1. remove those grants (the public REST/GraphQL API can no longer touch Griffin tables at all; RLS stays as 2nd layer)
--   2. stop future tables from re-acquiring them
--   3. put the migration ledger behind RLS with no policy (it was the only table without RLS)
-- Reversible with GRANT. `service_role` is left alone (its key is never distributed).
REVOKE ALL ON ALL TABLES    IN SCHEMA public FROM anon, authenticated;
REVOKE ALL ON ALL SEQUENCES IN SCHEMA public FROM anon, authenticated;
REVOKE ALL ON ALL FUNCTIONS IN SCHEMA public FROM anon, authenticated;
ALTER DEFAULT PRIVILEGES FOR ROLE postgres IN SCHEMA public REVOKE ALL ON TABLES    FROM anon, authenticated;
ALTER DEFAULT PRIVILEGES FOR ROLE postgres IN SCHEMA public REVOKE ALL ON SEQUENCES FROM anon, authenticated;
ALTER DEFAULT PRIVILEGES FOR ROLE postgres IN SCHEMA public REVOKE ALL ON FUNCTIONS FROM anon, authenticated;
ALTER TABLE public.schema_migrations ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.schema_migrations FORCE ROW LEVEL SECURITY;

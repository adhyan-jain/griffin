-- LOCAL / TEST ONLY. Emulates the minimal Supabase `auth` schema on plain PostgreSQL.
-- Applied only by `griffin-migrate -local-auth-shim` (and the Go test helpers).
-- NEVER apply to Supabase: it already owns auth.users and auth.uid().
CREATE SCHEMA IF NOT EXISTS auth;

CREATE TABLE IF NOT EXISTS auth.users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email VARCHAR(255) UNIQUE,
    raw_user_meta_data JSONB DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Only create auth.uid() if it does not already exist (never replace a platform function).
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
        WHERE n.nspname = 'auth' AND p.proname = 'uid'
    ) THEN
        CREATE FUNCTION auth.uid() RETURNS UUID AS $f$
            SELECT NULLIF(current_setting('request.jwt.claim.sub', true), '')::UUID;
        $f$ LANGUAGE SQL STABLE;
    END IF;
END $$;

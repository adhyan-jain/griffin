# Supabase migration plan — AWAITING APPROVAL (nothing applied)

Target: project `<project-ref>` (ap-south-1, Postgres 17.11, `max_connections` 60), via the **session pooler :5432** (`MIGRATION_DATABASE_URL`).

## Verified before this plan (live, read-only)
- Connectivity: session (5432) and transaction (6543) poolers both authenticate; password correct.
- `public` schema empty; `schema_migrations` absent; `griffin-migrate status` → applied: none, pending: 000001–000004.
- `auth.users`/`auth.uid()` present (owner `supabase_auth_admin`); `auth.users` has 0 rows (no operator yet).
- JWT: asymmetric — JWKS publishes one ES256/P-256 key → backend uses `SUPABASE_JWKS_URL` + issuer; no HS256 secret needed.
- Publishable key accepted by Auth; supabase-js ^2.118 supports it; the backend never uses it.
- Backup procedure tested: `pg_dump -Fc -n public` → `~/griffin-backups/` (mode 700; empty schema today).

## What `griffin-migrate up` will do (one transaction per file, advisory-locked, SHA-256 recorded)
| # | File | Effect |
|---|---|---|
| 1 | `000001_init_schema` | `uuid-ossp`, `pgcrypto`; 11 tables (users-owned task/approval/agent/artifact data) |
| 2 | `000002_rls_policies` | `ENABLE`+`FORCE ROW LEVEL SECURITY` and one `auth.uid()` policy on each of the 11 tables (only `DROP POLICY IF EXISTS` precede them) |
| 3 | `000003_orchestration_and_agent_factory` | 5 tables + 5 forced-RLS policies |
| 4 | `000004_s3_storage_artifacts` | artifact storage columns |
It does **not** touch `auth.*`; no `-local-auth-shim`. Duration: seconds.

## Verification afterwards
`griffin-migrate status` (pending empty) · `SELECT relname,relrowsecurity,relforcerowsecurity FROM pg_class WHERE relnamespace='public'::regnamespace AND relkind='r'` all `t/t` (16 tables) · `auth.uid()` owner/definition unchanged · re-run `up` ⇒ "already up to date".

## Finding you must know: RLS does not protect the backend path
The role the app would use (`postgres`) has `BYPASSRLS=true` and the code sets no per-request user context. On this database RLS protects **direct PostgREST access with the publishable key**; the backend's tenant isolation is the application-level `user_id` checks (covered by `TestArtifactTenantIsolation`, task/approval tests). Recommended hardening, included here: run the app as a dedicated `griffin_app` role (`infra/ops/create-app-role.sql`: DML on `public` only, no DDL, no `auth` access) so the Lambda/worker never hold the admin role. Step 5 below.

## Steps requiring your approval
1. `pg_dump` again immediately before (cheap).  2. `go run ./cmd/griffin-migrate up`.  3. Run the verification queries.
4. (You, dashboard) disable sign-ups, create operator user.  5. Create `griffin_app` (generate its password locally, never printed) and point `DATABASE_URL_SESSION/TXN` at it for runtime.

## Rollback
Database is empty today, so: restore = reset `public` to empty (`*.down.sql` in reverse order are safe **only while no user data exists**). After real data exists: restore the `pg_dump -Fc` into a fresh project/branch; never run down files on production data. Cost: $0 (existing project).

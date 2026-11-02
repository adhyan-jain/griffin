# Supabase production checklist

Nothing here has been run against a hosted project. **Status: REQUIRES_USER_APPROVAL** (checkpoint 2) and **BLOCKED** on
project credentials the repository does not have. Never paste connection strings or keys into chat, docs or commits.

| # | Item | Status | How / evidence |
|---|---|---|---|
| 1 | Hosted project in `ap-south-1` | BLOCKED (user action) | Create in dashboard; record project ref. Needed from user: confirm project exists or authorise creation, and the **plan** (backups differ by plan) |
| 2 | Auth config | REQUIRES_USER_APPROVAL | Disable public sign-ups; create operator user; Site URL = Vercel URL. Code requires `aud=authenticated`, `role=authenticated`, `exp` |
| 3 | JWT verification mode | IMPLEMENTED_NOT_LIVE_VERIFIED | New projects use asymmetric keys → set `SUPABASE_JWKS_URL=https://<ref>.supabase.co/auth/v1/.well-known/jwks.json` and `SUPABASE_JWT_ISSUER=https://<ref>.supabase.co/auth/v1`; legacy projects → `SUPABASE_JWT_SECRET` (≥32 chars). ES256 via JWKS and HS256 rejection when JWKS-only are covered by local tests (`middleware_test.go`); never exercised against the live project |
| 4 | Poolers | IMPLEMENTED_NOT_LIVE_VERIFIED | Migrations + worker: session pooler `:5432`. Lambda: transaction pooler `:6543` — server switches pgx to simple protocol automatically; whole `./tests` suite passed under `default_query_exec_mode=simple_protocol&statement_cache_capacity=0` against local Postgres, **not** against Supavisor |
| 5 | Pool sizing | IMPLEMENTED_NOT_LIVE_VERIFIED | Lambda `DB_MAX_CONNS=2 DB_MIN_CONNS=0` + reserved concurrency 5 ⇒ ≤10 txn-pooler clients. Worker `DB_MAX_CONNS=5 DB_MIN_CONNS=0` (now set by `griffin-fetch-env.sh`; previously defaulted to 25) ⇒ ≤5 session-pooler connections. Compare with the plan's pooler client limit |
| 6 | Explicit migrations | VERIFIED (local stock Postgres + `supabase/postgres` image) | `griffin-migrate up` (refuses :6543; no `-local-auth-shim` in production). Server never migrates; it only runs a read-only `CheckSchema`. `auth.uid()` byte-identical before/after on the Supabase image |
| 7 | RLS + tenant isolation | VERIFIED locally; hosted check REQUIRES_USER_APPROVAL | After migrating run `SELECT relname, relrowsecurity, relforcerowsecurity FROM pg_class WHERE relnamespace='public'::regnamespace AND relkind='r';` — every tenant table must be `t/t`. Local tests: RLS, artifact tenant isolation (`TestArtifactTenantIsolation`) |
| 8 | Env vars | documented | Lambda: `ENVIRONMENT=production GRIFFIN_MODE=api DATABASE_URL(txn) DB_MAX_CONNS=2 DB_MIN_CONNS=0 SUPABASE_JWKS_URL|SUPABASE_JWT_SECRET SUPABASE_JWT_ISSUER WORKER_API_KEY STORAGE_PROVIDER=s3 S3_BUCKET S3_REGION SCALE_TO_ZERO_ENABLED=true EC2_INSTANCE_ID CORS_ALLOWED_ORIGINS PORT=8080 AWS_LWA_PORT=8080 AWS_LAMBDA_EXEC_WRAPPER=/opt/bootstrap`. Worker (SSM `/griffin/prod/worker/`): `DATABASE_URL(session) SUPABASE_JWT_SECRET WORKER_API_KEY S3_BUCKET`. Web: `NEXT_PUBLIC_API_URL NEXT_PUBLIC_SUPABASE_URL NEXT_PUBLIC_SUPABASE_ANON_KEY` (anon only; never `service_role`; never set `NEXT_PUBLIC_ENABLE_DEV_LOGIN`) |
| 9 | Backup & recovery | BLOCKED (plan unknown) | Free plan has no downloadable/PITR backups; Pro has daily backups, PITR is an add-on. Regardless: take `pg_dump -Fc "$MIGRATION_DATABASE_URL" > pre-migration.dump` (session pooler) before each `up`. Restore test: restore the dump into a throwaway local Postgres and run `griffin-migrate status`. Recovery = restore backup; never run `*.down.sql` on production |
| 10 | Secret handling | IMPLEMENTED | Values only via `~/.griffin-prod.env` (600) → SSM SecureString (worker) / `jq`-built Lambda env. Lambda env is encrypted at rest but readable by anyone with `lambda:GetFunctionConfiguration`; acceptable for single-operator MVP, revisit before multi-user |

## Required user actions
1. Create/confirm the Supabase project (ap-south-1) and tell me the **plan tier** and whether it uses asymmetric JWT keys (do not send keys).
2. Put the three connection strings and the anon key in `~/.griffin-prod.env` yourself (`chmod 600`).
3. Approve checkpoint 2 before any migration is run.

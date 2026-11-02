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

## RLS enforcement (RESOLVED in code, local commit `0f55698`; not yet applied to the hosted DB)
Problem found: the `postgres` role has `BYPASSRLS` and the code set no per-request user context, so RLS did not protect the backend.
Fix: every user request runs in ONE transaction on role `griffin_app` (NOSUPERUSER, **NOBYPASSRLS**) with `auth.uid()` set via
`set_config(..., is_local => true)`; worker/scheduler/reaper use the separate `griffin_system` role (BYPASSRLS, DML only) and only
from code explicitly marked `database.WithSystem(ctx)`; any unmarked context is denied. The API refuses to start if its user pool
role is superuser/BYPASSRLS. Production API mode requires `USER_DATABASE_URL`.

Proof (real Postgres, real role SQL; 15 new tests, all pass; mutation-checked): role attributes and denied DDL/auth/ledger writes;
user A cannot read/update/delete/insert-as/re-home user B rows; every public table is ENABLE+FORCE RLS and empty for an unrelated
user; no claims => zero rows; identity never leaks across pooled connections (1 connection, small pool, concurrent, simple
protocol); fail-closed scope; HTTP end to end (4xx rolls back, commit before response); approvals (nested savepoint); JJ submit.
Sabotage checks: session-level claims, BYPASSRLS app role, and a missing `auth` schema grant each make tests fail.

Runtime roles: `infra/ops/create-runtime-roles.sql` (griffin_app + griffin_system; USAGE on `auth`/`public`; DML on public tables;
no DDL, no `auth` table access, `schema_migrations` read-only, fixed `search_path`). Env: Lambda `USER_DATABASE_URL`=griffin_app
(txn pooler 6543), `DATABASE_URL`=griffin_system (txn 6543); worker `DATABASE_URL`=griffin_system (session 5432).

## Steps requiring your approval
1. `pg_dump` again immediately before (cheap).  2. `go run ./cmd/griffin-migrate up`.  3. Run the verification queries.
4. (You, dashboard) disable sign-ups, create operator user.  5. Run `create-runtime-roles.sql` (passwords already generated locally in `~/.griffin-prod.env`) and use the `LAMBDA_*`/`WORKER_*` URLs for runtime.

## Rollback
Database is empty today, so: restore = reset `public` to empty (`*.down.sql` in reverse order are safe **only while no user data exists**). After real data exists: restore the `pg_dump -Fc` into a fresh project/branch; never run down files on production data. Cost: $0 (existing project).

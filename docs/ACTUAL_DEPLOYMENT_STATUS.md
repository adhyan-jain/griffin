# Actual deployment status

Last verified: 2026-11-02 (live read-only checks; nothing created). **Griffin is NOT deployed — no live URL exists.**

| Item | Status | Evidence |
|---|---|---|
| Code on `origin/master` | VERIFIED | `83a0783` present, CI green (Test & Build, Web, Code Quality) |
| `~/.griffin-prod.env` | VERIFIED (mode 600, outside repo; 10 variables; repo grep shows no credential in Git) | values never printed |
| Supabase project / region | VERIFIED | `aws-0-ap-south-1` pooler answers; Postgres 17.11 |
| Supabase poolers | VERIFIED | session :5432 and transaction :6543 both authenticate. Direct host is IPv6-only (unusable from this machine/AWS default VPC) |
| JWT signing | VERIFIED asymmetric | JWKS has one ES256/P-256 key → `SUPABASE_JWKS_URL` + issuer; no legacy secret |
| Publishable key | VERIFIED accepted by Auth; supabase-js ^2.118 compatible | backend never uses it |
| **Public sign-ups** | **NOT DISABLED** (`disable_signup:false`) | dashboard action required **before any public deployment** |
| Operator user | MISSING | `auth.users` has 0 rows |
| Database schema | EMPTY — 4 migrations pending | see `docs/SUPABASE_MIGRATION_PLAN.md` (**awaiting approval**) |
| RLS on backend path | FINDING | role `postgres` has BYPASSRLS; isolation is app-level; plan adds least-privilege `griffin_app` |
| IAM Policy A (deployer EC2/SSM) | **MISSING** | `ec2 run-instances --dry-run` → `UnauthorizedOperation` |
| IAM Policy C (worker runtime) | **MISSING** | worker role still has only `GriffinArtifactStorageAccess` |
| Lambda / API Gateway / EventBridge / SSM access | **BLOCKED** | `AccessDenied` for `griffin-bootstrap` (needs Policy D) |
| Existing bucket / worker role / instance profile | VERIFIED | present, unchanged |
| EC2 instances | VERIFIED none | `describe-instances` empty |
| Vercel CLI | VERIFIED logged in as `adhyan-jain`; Griffin project **not linked** (other projects in the account are unrelated and untouched) |
| PWA installability | IMPLEMENTED_NOT_LIVE_VERIFIED | 192/512/maskable PNG icons, complete manifest, iOS touch icon + web-app meta added; production build passes; not yet tested on a device |
| Antigravity unattended auth | NOT TESTED | needs the instance |
| Budgets / alarms / S3 lifecycle | NOT APPLIED | scripts ready in `infra/ops/` (need Policy D + approval) |

## Actions only you can do (shortest path)

### 1. Supabase credentials → `~/.griffin-prod.env` (create it yourself, `chmod 600`, never paste in chat)
In the dashboard, open **Connect**:
- **Framework** tab → Project URL and anon key:
  `NEXT_PUBLIC_SUPABASE_URL=…` `NEXT_PUBLIC_SUPABASE_ANON_KEY=…`
- **Direct** tab → *Session pooler* (port 5432) string for `MIGRATION_DATABASE_URL=…` and `DATABASE_URL_SESSION=…` (same value);
  *Transaction pooler* (port 6543) string for `DATABASE_URL_TXN=…`. Append `?sslmode=require` if missing.
- **Project Settings → JWT Keys**: if an asymmetric (ECC/RSA) signing key is current, set
  `SUPABASE_JWKS_URL=https://<ref>.supabase.co/auth/v1/.well-known/jwks.json` and `SUPABASE_JWT_ISSUER=https://<ref>.supabase.co/auth/v1`.
  If only the legacy secret exists, set `SUPABASE_JWT_SECRET=…` (≥ 32 chars). Tell me which (not the value).
- Also set `WORKER_API_KEY=$(openssl rand -base64 36)` and `ALERT_EMAIL=you@…`.
- **Authentication → Sign In / Providers**: disable "Allow new users to sign up"; **Users → Add user** for your operator account.

### 2. AWS admin: validate, then attach (admin profile; I never receive admin credentials)
```bash
cd docs/FINAL_IAM_POLICIES
for f in A-deployer-ec2-ssm C-worker-role-runtime; do
  aws accessanalyzer validate-policy --policy-type IDENTITY_POLICY --policy-document file://$f.json --profile ADMIN   # fix ERROR findings first
done
aws iam put-user-policy --user-name griffin-bootstrap --policy-name GriffinDeployerEc2Ssm   --policy-document file://A-deployer-ec2-ssm.json   --profile ADMIN
aws iam put-role-policy --role-name GriffinEC2WorkerRole --policy-name GriffinWorkerRuntime --policy-document file://C-worker-role-runtime.json --profile ADMIN
```
Policy D (Lambda/API/EventBridge/SSM-put/budgets/alarms) is attached later, with the same pattern, once the EC2 step is done — or now if you want me to continue without pausing (it grants only the `griffin-*` resources listed in `docs/FINAL_IAM_POLICIES/README.md`). Policies B and E are roles I create after D exists.

## What happens the moment those two are done (I proceed in this order, asking at each gate)
migration plan → **your approval** → `pg_dump` + `griffin-migrate up` + RLS/auth checks → `GRIFFIN_DRY_RUN=1` EC2 → **your approval** + cost → launch → Antigravity login (you complete the URL/code step on the instance) and reboot / stop-start tests → Lambda + HTTP API + EventBridge (disabled) → Vercel deploy → acceptance tests.

## Sign-ups
Open sign-ups are an accepted risk for now. The API enforces `ALLOWED_USER_EMAILS` (required in production API mode): a stranger can register but every API call returns 403. Prefer also disabling sign-ups in the dashboard.

## Deployed 2026-11-02 (verified live)
| Component | Status | Evidence |
|---|---|---|
| API (Lambda `griffin-api` + HTTP API `fq322jvjt4`) | **LIVE** `https://fq322jvjt4.execute-api.ap-south-1.amazonaws.com` | `/healthz` 200, `/readyz` DB connected, unauth 401, dev-login 404, evil-origin CORS 403 |
| PWA (Vercel project `griffin-pwa`) | **LIVE** `https://griffin-pwa.vercel.app` | login page 200, manifest + icons 200, 0 dev-login strings in bundle |
| Runner EC2 `i-070222a98aa0cadce` | running (t4g.medium, 30 GiB encrypted, IMDSv2, 0 inbound) | worker NOT installed/enabled yet |
| Antigravity unattended auth | NOT TESTED | needs worker install + interactive login |
| Scheduler tick (EventBridge) | NOT CREATED | needs admin: `aws iam create-service-linked-role --aws-service-name apidestinations.events.amazonaws.com` |
| Budgets $10/$20, S3 lifecycle | applied | verified by name |
| Login / tasks | NOT VERIFIED | operator user must exist in Supabase |

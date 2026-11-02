# Actual deployment status

Last verified: 2026-11-02 (live read-only checks; nothing created). **Griffin is NOT deployed — no live URL exists.**

| Item | Status | Evidence |
|---|---|---|
| Code on `origin/master` | VERIFIED | `83a0783` present, CI green (Test & Build, Web, Code Quality) |
| `~/.griffin-prod.env` | **MISSING** | file not found → no Supabase credentials available |
| Supabase project / JWT type / poolers | **BLOCKED** | cannot be checked without the env file |
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

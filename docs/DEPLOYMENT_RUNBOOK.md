# Griffin Cloud Deployment & Operations Runbook

Status: gates G1–G8 are closed in code (see `docs/GATE_CLOSURE_REPORT.md`). **Nothing in this
runbook has been executed against AWS or Supabase.** Run it only after explicit approval. AWS
account `220115213279`, region `ap-south-1`, profile `griffin-bootstrap`. Final IAM policies are in `docs/FINAL_IAM_POLICIES/` (they supersede the drafts in `DEPLOYMENT_PREFLIGHT_REPORT.md` §5); see `docs/CONTROLLED_DEPLOYMENT_REPORT.md` for current status; this file holds
the commands that changed after hardening.

Conventions: `P="--profile griffin-bootstrap --region ap-south-1"`. Secrets live in
`~/.griffin-prod.env` (`chmod 600`, outside the repo); never echo them.

## 1. Topology

- **Supabase**: Postgres + Auth (source of truth, RLS on every tenant table).
- **API**: the same Go binary with `GRIFFIN_MODE=api` on Lambda (arm64, `provided.al2023`, Lambda
  Web Adapter) behind API Gateway HTTP API. Reachable while EC2 is stopped.
- **Runner**: EC2 `t4g.medium` running `GRIFFIN_MODE=worker` + `agy`, started by the API
  (`ec2:StartInstances`) and powering itself off after 300 s idle.
- **Scheduler**: EventBridge rule `rate(1 minute)` → API destination → `POST /api/v1/scheduler/tick`.
- **PWA**: Vercel. **Artifacts**: S3 `griffin-artifacts-220115213279-ap-south-1`
  (objects under `users/<user_id>/…`).

## 2. Security model (enforced in code)

| Control | Behavior |
|---|---|
| Environment | `ENVIRONMENT` unset ⇒ production. Production refuses dev secrets, `ENABLE_DEV_LOGIN`, localhost/non-TLS DB, `mock` storage/AI, default bucket, `S3_ENDPOINT`/`AWS_PROFILE`, missing CORS origins, missing `EC2_INSTANCE_ID`. |
| Dev login | `POST /api/v1/auth/login` is only registered when `ENABLE_DEV_LOGIN=true` **and** not production. The PWA strips the dev-login code from production bundles. |
| JWT | HS256 (`SUPABASE_JWT_SECRET`) and/or ES256/RS256 via `SUPABASE_JWKS_URL`; requires `exp`, `aud=authenticated`, `role=authenticated`, optional `SUPABASE_JWT_ISSUER`; `none`/other algorithms rejected. |
| CORS | Exact allowlist (`CORS_ALLOWED_ORIGINS`), no wildcard, never credentials. |
| Artifacts | Keys are `users/<user_id>/…`; register/download/presign/delete re-verify bucket + prefix; `task_id` ownership verified; uploads capped (`MAX_UPLOAD_BYTES`). |
| Migrations | Explicit `griffin-migrate`; server only runs a read-only schema check. |

## 3. Pre-deploy gate (local, offline)

```bash
gofmt -l cmd internal tests migrations          # must print nothing
go vet ./...
GRIFFIN_TEST_DB_URL=<throwaway local postgres> go test -count=1 ./...
(cd web && npm ci && npm run lint && npx tsc --noEmit && npm run build)
make package                                    # build/griffin-api.zip + build/griffin-worker.tar.gz (arm64)
gh run list --limit 1                           # CI green for this SHA
```

## 4. Database (Supabase)

Use the **session pooler (5432)** or direct connection for migrations; the tool refuses port 6543.
```bash
set -a; . ~/.griffin-prod.env; set +a            # MIGRATION_DATABASE_URL = session pooler URL
pg_dump --schema-only "$MIGRATION_DATABASE_URL" > pre-migration-schema.sql
go run ./cmd/griffin-migrate status
go run ./cmd/griffin-migrate up                  # NO -local-auth-shim: Supabase owns auth.users / auth.uid()
go run ./cmd/griffin-migrate status              # pending must be empty
```
`up` is idempotent, serialized with an advisory lock, records checksums in `schema_migrations`, and
fails if an applied file changes or if `auth.users`/`auth.uid()` are missing. Rollback = restore
from backup (down files are manual and destructive).
Runtime URLs: worker → session pooler (5432); Lambda → transaction pooler (6543); the server
detects port 6543 and switches pgx to the simple protocol automatically (use `DB_MAX_CONNS=2`,
`DB_MIN_CONNS=0` on Lambda). In Supabase: disable public sign-ups, create the operator user, and
record whether the project uses the legacy HS256 secret or asymmetric keys (then set
`SUPABASE_JWKS_URL`).

## 5. Secrets (SSM Parameter Store, SecureString)

```bash
put() { aws ssm put-parameter $P --type SecureString --overwrite --name "$1" --value "$2" >/dev/null && echo "stored $1"; }
put /griffin/prod/worker/DATABASE_URL        "$DATABASE_URL_SESSION"
put /griffin/prod/worker/SUPABASE_JWT_SECRET "$SUPABASE_JWT_SECRET"
put /griffin/prod/worker/WORKER_API_KEY      "$WORKER_API_KEY"      # openssl rand -base64 36
put /griffin/prod/worker/S3_BUCKET           griffin-artifacts-220115213279-ap-south-1
```

## 6. EC2 runner

1. Pre-check there is no existing runner (the script now refuses to launch a second one).
2. `AWS_PROFILE=griffin-bootstrap ./infra/provision-ec2.sh` — resolves the AMI from the public
   Canonical SSM parameter (no hard-coded fallback), requires IMDSv2, `stop` on OS shutdown, an
   explicit security group (zero inbound), encrypted 30 GiB gp3. Tighten the role policy to the
   bucket ARN (script now does) and add `ssm:GetParameter(s)ByPath` on `/griffin/prod/worker*`.
3. Ship and install the worker bundle (arm64), via SSM:
   ```bash
   bash scripts/package-worker.sh                                   # build/griffin-worker.tar.gz
   aws s3 cp build/griffin-worker.tar.gz s3://<bucket>/releases/<sha>/ $P
   # on the instance (aws ssm start-session): download, untar, then
   sudo bash install-worker.sh      # installs unit, sudoers (validated by visudo), env loader, AWS CLI
   sudo /usr/local/bin/griffin-fetch-env.sh   # fails closed if a required parameter is missing
   ```
4. Do **not** enable the service until Antigravity auth is verified (next section).

## 7. API (Lambda) — build offline, deploy after approval

```bash
bash scripts/package-lambda.sh        # build/griffin-api.zip containing bootstrap (arm64); migrations not shipped
```
Lambda env: `ENVIRONMENT=production GRIFFIN_MODE=api DATABASE_URL=<transaction pooler URL> DB_MAX_CONNS=2
DB_MIN_CONNS=0 SUPABASE_JWT_SECRET|SUPABASE_JWKS_URL WORKER_API_KEY STORAGE_PROVIDER=s3 S3_BUCKET S3_REGION
SCALE_TO_ZERO_ENABLED=true EC2_INSTANCE_ID CORS_ALLOWED_ORIGINS=https://<web-domain> PORT=8080
AWS_LWA_PORT=8080 AWS_LAMBDA_EXEC_WRAPPER=/opt/bootstrap` (build the JSON with `jq` from the 600 env
file). Create function / HTTP API / ACM / custom domain / EventBridge API destination with the
commands in `DEPLOYMENT_PREFLIGHT_REPORT.md` §15 after verifying the Lambda Web Adapter layer ARN.
Task creation and approvals now call `TriggerWakeup` synchronously (≤ 8 s) so the wake completes
before Lambda freezes; the 1-minute tick is the backstop.

## 8. Antigravity authentication (one time, interactive)

```bash
aws ssm start-session --target $INSTANCE_ID $P ; sudo -i -u griffin
export XDG_RUNTIME_DIR=/run/user/$(id -u) DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/$(id -u)/bus
agy                                   # URL → sign in elsewhere → paste code
agy -p 'Reply with {"status":"ready"}' --output-format json
```
Then: (1) run the same command with no session attached; (2) `sudo reboot` and repeat via
`aws ssm send-command`; (3) stop/start the instance and repeat. Only if all pass:
`sudo systemctl enable --now griffin-worker && journalctl -u griffin-worker -n 50`. If not,
STOP: do not enable the worker and do not switch to `GEMINI_API_KEY`/ADC (per-usage billing). Report the legitimate alternatives to the user (see `docs/CLOUD_ACCEPTANCE_TESTS.md`, "If it fails").
Status: persistence across reboot is **NOT VERIFIED** until this is done on the instance.

## 9. PWA (Vercel)

```bash
cd web && vercel link
vercel env add NEXT_PUBLIC_API_URL production
vercel env add NEXT_PUBLIC_SUPABASE_URL production
vercel env add NEXT_PUBLIC_SUPABASE_ANON_KEY production     # anon key only, never service_role
vercel deploy --prod                                         # do NOT set NEXT_PUBLIC_ENABLE_DEV_LOGIN
```

## 10. Rollback

PWA: `vercel rollback`. API: previous Lambda version/alias; emergency stop
`aws events disable-rule --name griffin-tick` and reserved concurrency 0. Worker:
`systemctl disable --now griffin-worker`, `aws ec2 stop-instances`. DB: restore backup. Rotate
`WORKER_API_KEY`/`SUPABASE_JWT_SECRET` in SSM + Lambda + EventBridge connection together.

## 11. Verification checklist

See `DEPLOYMENT_PREFLIGHT_REPORT.md` §20 (20 live checks). Highest value after deploy: dev login
route returns 404; evil `Origin` gets no CORS headers; user B gets 404/403 on user A's artifact;
a stopped instance starts within ~60 s of a new task; instance returns to `stopped` ~5 min after the
queue empties.

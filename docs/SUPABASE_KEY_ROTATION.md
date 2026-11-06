# Supabase credential rotation checklist (nothing rotated automatically)

The current publishable key and DB password were pasted into a chat transcript: **treat them as exposed; rotate after first setup works.**
Never rotate two things at once; verify login + one task between steps.

| Secret | Where it lives | Rotation order (no lock-out) |
|---|---|---|
| **DB password (`postgres`)** | `~/.griffin-prod.env`, only used for migrations/admin | 1) Dashboard → Database → Settings → Reset password (pooler caches creds a few minutes). 2) Update `~/.griffin-prod.env` (`DB_PASSWORD_ENC` is URL-encoded: encode `@ : / ? # %`). 3) `griffin-migrate status` to confirm. Runtime is unaffected **if the app runs as `griffin_app`**. |
| **`griffin_app` password** (runtime) | SSM `/griffin/prod/worker/DATABASE_URL`, Lambda env `DATABASE_URL` (txn pooler) | `ALTER ROLE griffin_app PASSWORD '<new>'` → update SSM + `aws lambda update-function-configuration` immediately → `systemctl restart griffin-worker` (re-fetches SSM). Expect seconds of failed requests; schedule when idle. Zero-downtime variant: create `griffin_app2`, switch both, drop the old one. |
| **Publishable/anon key** | Vercel env `NEXT_PUBLIC_SUPABASE_ANON_KEY` (public by design) | Create the new key first (Dashboard → API Keys; confirm whether your plan allows two live keys), set it in Vercel, redeploy, verify login, then revoke the old. Existing sessions keep working (they use JWTs, not the key). |
| **JWT signing key** | JWKS URL (no secret in Griffin) | Dashboard → JWT Keys: add/rotate standby key; JWKS publishes both, backend caches JWKS and verifies by `kid`, so rotation is seamless. Don't revoke the old key until old access tokens expire (≤ 1 h). Users re-login only if you revoke early. |
| **`WORKER_API_KEY`** | `~/.griffin-prod.env`, SSM `/griffin/prod/worker/WORKER_API_KEY`, Lambda env, EventBridge connection | Rotate all four together: SSM put → Lambda config → `aws events update-connection` → restart worker. Between steps the tick/worker calls get 401 for seconds. |
| **Service-role / secret keys** | Not used anywhere in Griffin | Do not create one for this app. |

Lock-out protection: keep the old value until the new one is verified in every consumer; keep `~/griffin-backups/` dump and the dashboard login (not dependent on these secrets).

---

## Incident 2026-11-06: Lambda environment printed into a chat transcript

`infra/ops/deploy-backend.sh lambda --apply` printed the whole `update-function-code` response, which includes the function's environment. The script is fixed (it now prints only version, sha, time and state), but the values were shown in a conversation and written to a local log (deleted). **Treat these as exposed:**

| Secret | Lambda env var | Other consumers to update in the same step |
|---|---|---|
| `griffin_app` database password | `USER_DATABASE_URL` | SSM `/griffin/prod/worker/DATABASE_URL` if the worker connects as this role |
| `griffin_system` database password | `DATABASE_URL` | Check whether the worker's SSM `DATABASE_URL` uses this role; find every consumer before changing it |
| `WORKER_API_KEY` | `WORKER_API_KEY` | SSM `/griffin/prod/worker/WORKER_API_KEY`, the EventBridge connection if one exists, `~/.griffin-prod.env` |

Also exposed earlier and covered above: the `postgres` admin password and the publishable key.

### Order (one secret at a time; verify sign-in and one task between steps)
1. **List consumers first, names only** (never values):
   `aws lambda get-function-configuration --function-name griffin-api --query 'Environment.Variables | keys(@)' --output json`
   `aws ssm describe-parameters --parameter-filters Key=Name,Option=BeginsWith,Values=/griffin/prod/ --query 'Parameters[].Name'`
2. **Pick the new value locally** (a password manager or `openssl rand -base64 36`); never paste it into a chat or a command that is logged.
3. **Change it at the source** (the database role, or generate the new `WORKER_API_KEY`), keeping the old value valid until step 5.
4. **Update every consumer.** For the Lambda environment, `update-function-configuration --environment` *replaces the whole map*, so merge, do not overwrite, and never let it echo the result:
   - read the current map into a file readable only by you (`umask 077`), change one key with `jq`, apply with `--environment file://...` **and `--query LastModified --output text`**, then delete the file;
   - for SSM, `aws ssm put-parameter --overwrite --type SecureString --query Version --output text`, then restart the worker so it re-reads it.
5. **Verify**: `GET /healthz`, a signed-in task list, and one real task end to end (worker claims it and finishes). Only then retire the old value (`ALTER ROLE ... PASSWORD`, or delete the old key).
6. **Record** the date in `decisions.md`. Rotate nothing while a task is mid-flight; the worker restart interrupts it.

### Rules that prevent a repeat
- Any command that returns a function or parameter configuration must use `--query` to name the fields it prints.
- `ship.sh` and `deploy-backend.sh` never print an environment; keep it that way (a regression test is worth adding if either script grows new `aws lambda` calls).

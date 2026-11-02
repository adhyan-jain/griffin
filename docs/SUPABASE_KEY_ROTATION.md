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

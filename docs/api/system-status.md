# GET /api/v1/system/status

User-authenticated (`Authorization: Bearer <Supabase JWT>`, same as other `/api/v1` routes). Returns evidence-derived health for seven components. Always HTTP 200 when authenticated; a failing probe affects only its own component. 401 without a valid token.

Fixture: `web-contract-fixtures/system_status.json`.

```json
{ "success": true,
  "data": {
    "generated_at": "RFC3339",
    "components": { "api": C, "database": C, "worker": C, "provider": C, "scheduler": C, "storage": C, "runner": C }
  } }
```

`C = { "state": "ok" | "degraded" | "down" | "unknown", "evidence": string, "observed_at": RFC3339 | null }`

Rule: `unknown` means no backend evidence. The UI must render it as unknown (neutral), never as ok. `observed_at` is null when nothing was ever observed. Evidence strings are short, human-readable and secret-free; display, do not parse.

| Component | Evidence source | ok | degraded | down | unknown |
|---|---|---|---|---|---|
| api | the request itself | always (process answered) | | | |
| database | pool ping latency (2s timeout) | <= 500ms | > 500ms | ping error/timeout | not configured / probe crashed |
| worker | caller's tasks: active leases, heartbeat (`tasks.updated_at` of leased RUNNING), attempt activity (24h), queue age, runner state | lease/heartbeat/activity within 2 min | RUNNING tasks with expired lease; or queued work > 5 min old while runner stopped/stopping/pending | queued work > 5 min old, runner running, no activity in 2 min | no work and no recent activity (idle), or runner state unknown |
| provider | outcomes of caller's last 10 finished attempts | latest attempt COMPLETED | latest timed out, or >= 3 consecutive failures | latest failed with auth error | no attempts, or single failure not attributable to provider |
| scheduler | none yet (no persisted tick) | | | | always; evidence "no tick record (needs system_heartbeats migration, not applied)". See `docs/proposed/000005_system_heartbeats.sql` |
| storage | HeadObject-style probe of a sentinel key (2s timeout); not-found counts as reachable | probe answered | access denied | bucket missing / probe failed | not configured |
| runner | instance manager state | running; or stopped with scale-to-zero enabled | pending (starting) | stopped with scale-to-zero disabled | state lookup failed / no manager |

Storage evidence is prefixed `mock (...)` when the in-memory provider is active; an `ok` there means only that the mock answered.

Tenancy: worker and provider read only the authenticated user's tasks/attempts (RLS transaction) and return aggregates with no ids. database/storage/runner are global and carry no tenant data.

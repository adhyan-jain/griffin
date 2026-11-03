-- PROPOSED, NOT APPLIED. Deliberately outside migrations/ (the embedded migrator would apply it).
-- To adopt: review, move to migrations/000005_system_heartbeats.up.sql, add a matching .down.sql,
-- and apply through the normal migration process.
--
-- Purpose: persist the last time each backend component (scheduler tick, reaper, worker loop...)
-- was observed, so GET /api/v1/system/status can report real evidence instead of "unknown".
--
-- RLS-safe design:
--   * Global operational data, not tenant data: one row per component, no user_id, and `detail`
--     must never contain user content, task ids or secrets (counters / durations only).
--   * RLS is enabled and FORCED with NO policy for the user role => end-user (authenticated) sessions
--     can neither read nor write it. Only the system path (service-role / table owner connection,
--     i.e. database.WithSystem scope) touches it. The status handler would read it through a system
--     scoped query and expose only component, last_seen_at and a derived state.

CREATE TABLE IF NOT EXISTS system_heartbeats (
    component    TEXT PRIMARY KEY CHECK (component ~ '^[a-z_]{1,40}$'),
    last_seen_at TIMESTAMPTZ NOT NULL,
    detail       JSONB
);

ALTER TABLE system_heartbeats ENABLE ROW LEVEL SECURITY;
ALTER TABLE system_heartbeats FORCE ROW LEVEL SECURITY;
-- Intentionally no CREATE POLICY: default deny for non-bypass roles.

REVOKE ALL ON system_heartbeats FROM PUBLIC;

-- Writer usage (scheduler tick handler, system scope only):
--   INSERT INTO system_heartbeats (component, last_seen_at, detail)
--   VALUES ('scheduler', now(), '{"created": 0}')
--   ON CONFLICT (component) DO UPDATE
--   SET last_seen_at = EXCLUDED.last_seen_at, detail = EXCLUDED.detail;

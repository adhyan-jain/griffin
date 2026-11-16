-- scripts/db-cleanup.sql
-- Griffin Database Free Tier Maintenance & Payload Pruning Script
-- Run this on your Neon / Supabase PostgreSQL database to reclaim 80-90% of storage.

BEGIN;

-- 1. Truncate heavy output payloads from older completed/failed tasks (keeps metadata and status intact)
UPDATE tasks
SET output_payload = '{"trimmed": true, "note": "Archived to free up database storage"}'::jsonb
WHERE status IN ('COMPLETED', 'FAILED', 'CANCELLED')
  AND created_at < NOW() - INTERVAL '7 days'
  AND length(output_payload::text) > 2048;

-- 2. Clear old audit logs older than 14 days
DELETE FROM audit_logs
WHERE timestamp < NOW() - INTERVAL '14 days';

-- 3. Clear old call logs older than 14 days
DELETE FROM call_logs
WHERE created_at < NOW() - INTERVAL '14 days';

COMMIT;

-- Reclaim freed disk space back to the database provider
VACUUM FULL tasks;
VACUUM FULL audit_logs;
VACUUM FULL call_logs;
ANALYZE;

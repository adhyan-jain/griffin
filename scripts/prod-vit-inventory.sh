#!/usr/bin/env bash
# Read-only inventory of production data for the VIT audit. Never prints the DB URL. Skips tables that don't exist.
set -euo pipefail
DBURL=$(AWS_PROFILE=griffin-bootstrap AWS_REGION=ap-south-1 aws ssm get-parameter --name /griffin/prod/worker/DATABASE_URL --with-decryption --query Parameter.Value --output text)
PGOPTIONS='-c default_transaction_read_only=on' psql "$DBURL" -X -A -t -F ' | ' -v ON_ERROR_STOP=1 <<'SQL'
SELECT 'migration', max(version)::text FROM schema_migrations;
SELECT 'tables_present', string_agg(t, ',') FROM unnest(ARRAY['canonical_entities','person_employments','company_observations','outreach_campaigns','outreach_targets','owner_profiles']) t WHERE to_regclass('public.'||t) IS NOT NULL;
SELECT 'owner_profiles_rows', count(*)::text FROM owner_profiles;
SELECT 'library_profile_docs', count(*)::text FROM artifacts WHERE metadata->>'kind' = 'owner_profile';
SELECT 'alumni_list_docs', count(*)::text FROM artifacts WHERE metadata->>'kind' = 'list' AND metadata->>'tracker' = 'alumni';
SELECT 'outreach_campaigns', count(*)::text FROM outreach_campaigns;
SELECT 'vit_campaigns', count(*)::text FROM outreach_campaigns WHERE name ILIKE '%vit%';
SELECT 'alumni_dm_tasks_by_status', coalesce(string_agg(status || '=' || n, ', '), 'none') FROM (SELECT status::text, count(*) n FROM tasks WHERE title LIKE 'ALUMNI DM - %' GROUP BY 1) s;
SQL

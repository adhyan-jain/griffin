-- 000003_orchestration_and_agent_factory.down.sql
-- Griffin Phase 3 Rollback

DROP TABLE IF EXISTS delegation_records CASCADE;
DROP TABLE IF EXISTS orchestration_plans CASCADE;
DROP TABLE IF EXISTS orchestrations CASCADE;
DROP TABLE IF EXISTS agent_factory_audits CASCADE;
DROP TABLE IF EXISTS worker_definitions CASCADE;

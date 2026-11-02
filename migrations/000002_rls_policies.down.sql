-- 000002_rls_policies.down.sql
-- Griffin Row Level Security (RLS) Rollback

DROP POLICY IF EXISTS notifications_user_policy ON notifications;
DROP POLICY IF EXISTS artifacts_user_policy ON artifacts;
DROP POLICY IF EXISTS schedules_user_policy ON schedules;
DROP POLICY IF EXISTS approvals_user_policy ON approvals;
DROP POLICY IF EXISTS task_events_user_policy ON task_events;
DROP POLICY IF EXISTS task_attempts_user_policy ON task_attempts;
DROP POLICY IF EXISTS task_dependencies_user_policy ON task_dependencies;
DROP POLICY IF EXISTS tasks_user_policy ON tasks;
DROP POLICY IF EXISTS agents_user_policy ON agents;
DROP POLICY IF EXISTS workspaces_user_policy ON workspaces;
DROP POLICY IF EXISTS profiles_user_policy ON profiles;

ALTER TABLE notifications DISABLE ROW LEVEL SECURITY;
ALTER TABLE artifacts DISABLE ROW LEVEL SECURITY;
ALTER TABLE schedules DISABLE ROW LEVEL SECURITY;
ALTER TABLE approvals DISABLE ROW LEVEL SECURITY;
ALTER TABLE task_events DISABLE ROW LEVEL SECURITY;
ALTER TABLE task_attempts DISABLE ROW LEVEL SECURITY;
ALTER TABLE task_dependencies DISABLE ROW LEVEL SECURITY;
ALTER TABLE tasks DISABLE ROW LEVEL SECURITY;
ALTER TABLE agents DISABLE ROW LEVEL SECURITY;
ALTER TABLE workspaces DISABLE ROW LEVEL SECURITY;
ALTER TABLE profiles DISABLE ROW LEVEL SECURITY;

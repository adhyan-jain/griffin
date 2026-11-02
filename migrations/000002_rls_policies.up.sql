-- 000002_rls_policies.up.sql
-- Griffin Row Level Security (RLS) Policies (Idempotent)

-- Enable RLS and FORCE RLS on all tenant-owned tables
ALTER TABLE profiles ENABLE ROW LEVEL SECURITY;
ALTER TABLE profiles FORCE ROW LEVEL SECURITY;
ALTER TABLE workspaces ENABLE ROW LEVEL SECURITY;
ALTER TABLE workspaces FORCE ROW LEVEL SECURITY;
ALTER TABLE agents ENABLE ROW LEVEL SECURITY;
ALTER TABLE agents FORCE ROW LEVEL SECURITY;
ALTER TABLE tasks ENABLE ROW LEVEL SECURITY;
ALTER TABLE tasks FORCE ROW LEVEL SECURITY;
ALTER TABLE task_dependencies ENABLE ROW LEVEL SECURITY;
ALTER TABLE task_dependencies FORCE ROW LEVEL SECURITY;
ALTER TABLE task_attempts ENABLE ROW LEVEL SECURITY;
ALTER TABLE task_attempts FORCE ROW LEVEL SECURITY;
ALTER TABLE task_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE task_events FORCE ROW LEVEL SECURITY;
ALTER TABLE approvals ENABLE ROW LEVEL SECURITY;
ALTER TABLE approvals FORCE ROW LEVEL SECURITY;
ALTER TABLE schedules ENABLE ROW LEVEL SECURITY;
ALTER TABLE schedules FORCE ROW LEVEL SECURITY;
ALTER TABLE artifacts ENABLE ROW LEVEL SECURITY;
ALTER TABLE artifacts FORCE ROW LEVEL SECURITY;
ALTER TABLE notifications ENABLE ROW LEVEL SECURITY;
ALTER TABLE notifications FORCE ROW LEVEL SECURITY;

-- 1. Profiles: Users can view and update their own profile
DROP POLICY IF EXISTS profiles_user_policy ON profiles;
CREATE POLICY profiles_user_policy ON profiles
    FOR ALL
    USING (auth.uid() = id)
    WITH CHECK (auth.uid() = id);

-- 2. Workspaces
DROP POLICY IF EXISTS workspaces_user_policy ON workspaces;
CREATE POLICY workspaces_user_policy ON workspaces
    FOR ALL
    USING (auth.uid() = user_id)
    WITH CHECK (auth.uid() = user_id);

-- 3. Agents
DROP POLICY IF EXISTS agents_user_policy ON agents;
CREATE POLICY agents_user_policy ON agents
    FOR ALL
    USING (auth.uid() = user_id)
    WITH CHECK (auth.uid() = user_id);

-- 4. Tasks
DROP POLICY IF EXISTS tasks_user_policy ON tasks;
CREATE POLICY tasks_user_policy ON tasks
    FOR ALL
    USING (auth.uid() = user_id)
    WITH CHECK (auth.uid() = user_id);

-- 5. Task Dependencies: Accessible if task belongs to user
DROP POLICY IF EXISTS task_dependencies_user_policy ON task_dependencies;
CREATE POLICY task_dependencies_user_policy ON task_dependencies
    FOR ALL
    USING (
        EXISTS (
            SELECT 1 FROM tasks 
            WHERE tasks.id = task_dependencies.task_id 
              AND tasks.user_id = auth.uid()
        )
    );

-- 6. Task Attempts: Accessible if task belongs to user
DROP POLICY IF EXISTS task_attempts_user_policy ON task_attempts;
CREATE POLICY task_attempts_user_policy ON task_attempts
    FOR ALL
    USING (
        EXISTS (
            SELECT 1 FROM tasks 
            WHERE tasks.id = task_attempts.task_id 
              AND tasks.user_id = auth.uid()
        )
    );

-- 7. Task Events: Accessible if task belongs to user
DROP POLICY IF EXISTS task_events_user_policy ON task_events;
CREATE POLICY task_events_user_policy ON task_events
    FOR ALL
    USING (
        EXISTS (
            SELECT 1 FROM tasks 
            WHERE tasks.id = task_events.task_id 
              AND tasks.user_id = auth.uid()
        )
    );

-- 8. Approvals
DROP POLICY IF EXISTS approvals_user_policy ON approvals;
CREATE POLICY approvals_user_policy ON approvals
    FOR ALL
    USING (auth.uid() = user_id)
    WITH CHECK (auth.uid() = user_id);

-- 9. Schedules
DROP POLICY IF EXISTS schedules_user_policy ON schedules;
CREATE POLICY schedules_user_policy ON schedules
    FOR ALL
    USING (auth.uid() = user_id)
    WITH CHECK (auth.uid() = user_id);

-- 10. Artifacts
DROP POLICY IF EXISTS artifacts_user_policy ON artifacts;
CREATE POLICY artifacts_user_policy ON artifacts
    FOR ALL
    USING (auth.uid() = user_id)
    WITH CHECK (auth.uid() = user_id);

-- 11. Notifications
DROP POLICY IF EXISTS notifications_user_policy ON notifications;
CREATE POLICY notifications_user_policy ON notifications
    FOR ALL
    USING (auth.uid() = user_id)
    WITH CHECK (auth.uid() = user_id);

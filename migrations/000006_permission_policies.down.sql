-- 000006: Revert the permission policy table
DROP POLICY IF EXISTS permission_policies_user_policy ON permission_policies;
DROP TABLE IF EXISTS permission_policies;

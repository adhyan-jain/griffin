-- 000011: The code has long had a BLOCKED_AWAITING_PROVIDER task status (a task waiting for the AI provider to be
-- signed in again) that the database enum never had, so moving a task into it failed with
-- "invalid input value for enum task_status". Add it. Adding an enum value changes nothing for existing rows.
ALTER TYPE task_status ADD VALUE IF NOT EXISTS 'BLOCKED_AWAITING_PROVIDER';

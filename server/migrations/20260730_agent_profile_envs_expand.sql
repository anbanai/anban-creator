-- Additive phase. Keep the legacy columns until the Go backfill verifies every row.
ALTER TABLE `task_executions`
  ADD COLUMN `profile_envs` json NULL;

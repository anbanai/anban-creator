-- One-time MySQL migration for schema changes introduced on 2026-07-15.
-- Run this before deploying a server build that no longer performs historical
-- schema migrations during startup.

ALTER TABLE `task_files`
  DROP CHECK `chk_task_file_state`,
  ADD CONSTRAINT `chk_task_file_state` CHECK (`state` IN ('pending', 'published', 'collected', 'superseded'));

ALTER TABLE `task_executions`
  DROP CHECK `chk_task_execution_manifest_status`,
  ADD CONSTRAINT `chk_task_execution_manifest_status` CHECK (`manifest_status` IN ('', 'pending', 'published', 'collected', 'discarded', 'rejected'));

-- Keep the newest feedback row for each task/agent business key. ID provides a
-- deterministic tie-break when two rows have the same creation timestamp.
DELETE older
FROM `agent_feedbacks` AS older
INNER JOIN `agent_feedbacks` AS newer
  ON older.`task_id` = newer.`task_id`
  AND older.`agent_name` = newer.`agent_name`
  AND (
    older.`created_at` < newer.`created_at`
    OR (older.`created_at` = newer.`created_at` AND older.`id` < newer.`id`)
  );

CREATE UNIQUE INDEX `idx_agent_feedback_task_agent` ON `agent_feedbacks` (`task_id`, `agent_name`);

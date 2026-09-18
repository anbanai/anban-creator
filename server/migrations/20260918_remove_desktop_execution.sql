-- Desktop execution was removed. Stop application traffic before applying this
-- migration, then restart the server so the startup migration can finish any
-- execution finalization that was interrupted during the cutover.
UPDATE `task_executions`
SET `status` = 'failed',
    `terminal_reason` = 'infrastructure_cancelled',
    `result` = JSON_OBJECT(
      'success', FALSE,
      'error', 'Desktop execution was removed',
      'terminal_reason', 'infrastructure_cancelled',
      'remote_artifacts', TRUE,
      'cost_status', 'unreconciled'
    ),
    `completed_at` = COALESCE(`completed_at`, UTC_TIMESTAMP()),
    `finalization_status` = '',
    `cleanup_status` = 'done'
WHERE `target` = 'local_claimed'
  AND `status` NOT IN ('succeeded', 'failed', 'cancelled', 'timed_out');

UPDATE `tasks`
SET `execution_target` = '', `local_claim_deadline` = NULL, `executor_info` = NULL
WHERE `execution_target` = 'local';

ALTER TABLE tasks
  DROP COLUMN execution_target,
  DROP COLUMN local_claim_deadline,
  DROP COLUMN executor_info;

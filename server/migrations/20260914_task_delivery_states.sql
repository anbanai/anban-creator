-- One-way cutover from ambiguous publication terminology to artifact delivery.
-- Stop application traffic before applying this migration.

ALTER TABLE `task_files` DROP CHECK `chk_task_file_state`;
UPDATE `task_files` SET `state` = 'delivered' WHERE `state` = 'published';
UPDATE `task_files` SET `state` = 'retained' WHERE `state` = 'collected';
ALTER TABLE `task_files`
  MODIFY COLUMN `state` varchar(20) NOT NULL DEFAULT 'delivered',
  ADD CONSTRAINT `chk_task_file_state` CHECK (`state` IN ('pending', 'delivered', 'retained', 'superseded'));

ALTER TABLE `task_executions` DROP CHECK `chk_task_execution_manifest_status`;
UPDATE `task_executions` SET `manifest_status` = 'delivered' WHERE `manifest_status` = 'published';
UPDATE `task_executions` SET `manifest_status` = 'retained' WHERE `manifest_status` = 'collected';
ALTER TABLE `task_executions`
  ADD CONSTRAINT `chk_task_execution_manifest_status` CHECK (`manifest_status` IN ('', 'pending', 'delivered', 'retained', 'discarded', 'rejected'));

-- This failed execution predates durable finalization. Retain every surviving
-- artifact in place without rerunning the Agent or creating billing events.
UPDATE `task_files` AS `file`
JOIN `task_executions` AS `execution` ON `execution`.`id` = `file`.`execution_id`
JOIN `tasks` AS `task` ON `task`.`id` = `execution`.`task_id`
SET `file`.`state` = 'retained'
WHERE `file`.`execution_id` = '6686adfb-1b1d-4042-b0a2-b89a0d4545b3'
  AND `execution`.`status` = 'failed'
  AND `task`.`status` = 'failed'
  AND `file`.`state` IN ('pending', 'delivered', 'retained');
UPDATE `task_executions` AS `execution`
JOIN `tasks` AS `task` ON `task`.`id` = `execution`.`task_id`
SET `execution`.`manifest_status` = 'retained'
WHERE `execution`.`id` = '6686adfb-1b1d-4042-b0a2-b89a0d4545b3'
  AND `execution`.`status` = 'failed'
  AND `task`.`status` = 'failed'
  AND `execution`.`manifest_status` IN ('', 'pending', 'delivered', 'retained');

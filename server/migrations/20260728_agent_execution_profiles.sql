-- Forward-only Agent execution profile cutover. Stop old application versions
-- before running: all new task and plan creation requires execution_profile.

ALTER TABLE `tasks`
  ADD COLUMN `execution_profile` varchar(40) NULL,
  ADD COLUMN `agent_profile_snapshot` json NULL;

UPDATE `tasks`
SET `execution_profile` = 'balanced',
    `agent_profile_snapshot` = JSON_OBJECT(
      'profile_id', 'balanced',
      'provider', 'volcengine_ark',
      'model_id', 'doubao-seed-evolving',
      'protocol', 'anthropic',
      'context_window', 0,
      'reasoning_effort', '',
      'thinking_required', false,
      'display_name', '平衡型'
    );

ALTER TABLE `tasks`
  MODIFY COLUMN `execution_profile` varchar(40) NOT NULL,
  MODIFY COLUMN `agent_profile_snapshot` json NOT NULL,
  ADD INDEX `idx_tasks_execution_profile` (`execution_profile`);

ALTER TABLE `plans`
  ADD COLUMN `execution_profile` varchar(40) NULL;

UPDATE `plans` SET `execution_profile` = 'balanced';

ALTER TABLE `plans`
  MODIFY COLUMN `execution_profile` varchar(40) NOT NULL,
  ADD INDEX `idx_plans_execution_profile` (`execution_profile`);

ALTER TABLE `task_executions`
  ADD COLUMN `provider` varchar(80) NULL,
  ADD COLUMN `model_id` varchar(128) NULL,
  ADD COLUMN `protocol` varchar(32) NULL,
  ADD COLUMN `reasoning_effort` varchar(20) NULL,
  ADD COLUMN `context_window` int NULL;

UPDATE `task_executions` AS `execution`
INNER JOIN `tasks` AS `task` ON `task`.`id` = `execution`.`task_id`
SET `execution`.`provider` = JSON_UNQUOTE(JSON_EXTRACT(`task`.`agent_profile_snapshot`, '$.provider')),
    `execution`.`model_id` = JSON_UNQUOTE(JSON_EXTRACT(`task`.`agent_profile_snapshot`, '$.model_id')),
    `execution`.`protocol` = JSON_UNQUOTE(JSON_EXTRACT(`task`.`agent_profile_snapshot`, '$.protocol')),
    `execution`.`reasoning_effort` = COALESCE(JSON_UNQUOTE(JSON_EXTRACT(`task`.`agent_profile_snapshot`, '$.reasoning_effort')), ''),
    `execution`.`context_window` = COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(`task`.`agent_profile_snapshot`, '$.context_window')) AS UNSIGNED), 0);

ALTER TABLE `task_executions`
  MODIFY COLUMN `provider` varchar(80) NOT NULL,
  MODIFY COLUMN `model_id` varchar(128) NOT NULL,
  MODIFY COLUMN `protocol` varchar(32) NOT NULL,
  MODIFY COLUMN `reasoning_effort` varchar(20) NOT NULL,
  MODIFY COLUMN `context_window` int NOT NULL,
  ADD INDEX `idx_task_executions_provider_model` (`provider`, `model_id`);

ALTER TABLE `billing_skus`
  ADD COLUMN `execution_profile` varchar(40) NOT NULL DEFAULT '',
  ADD INDEX `idx_billing_skus_execution_profile` (`execution_profile`),
  ADD INDEX `idx_billing_skus_catalog_operation_profile` (`catalog_id`, `operation`, `execution_profile`);

ALTER TABLE `billing_quotes`
  ADD COLUMN `agent_profile_snapshot` json NULL;

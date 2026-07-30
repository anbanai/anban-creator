-- Forward-only Agent execution profile cutover. Stop old application versions
-- before running: all new task and plan creation requires execution_profile.

ALTER TABLE `tasks`
  ADD COLUMN `execution_profile` varchar(40) NULL,
  ADD COLUMN `agent_profile_snapshot` json NULL,
  ADD COLUMN `agent_profile_fingerprint` char(64) NULL;

UPDATE `tasks`
SET `execution_profile` = 'balanced',
    `agent_profile_snapshot` = JSON_OBJECT(
      'schema_version', 2,
      'profile_id', 'balanced',
      'display_name', '平衡型',
      'provider', 'volcengine_ark',
      'protocol', 'anthropic',
      'models', JSON_OBJECT(
        'default', 'doubao-seed-evolving',
        'opus', 'doubao-seed-evolving',
        'fable', 'doubao-seed-evolving',
        'sonnet', 'doubao-seed-evolving',
        'haiku', 'doubao-seed-evolving'
      ),
      'claude', JSON_OBJECT(),
      'model_usage_aliases', JSON_OBJECT(
        'doubao-seed-evolving', 'doubao-seed-evolving'
      )
    ),
    `agent_profile_fingerprint` = 'c7f2d8997f92b789fe732f3398f16183cdb085eb09e7a1eb8b3480fefcc8fa9d';

ALTER TABLE `tasks`
  MODIFY COLUMN `execution_profile` varchar(40) NOT NULL,
  MODIFY COLUMN `agent_profile_snapshot` json NOT NULL,
  MODIFY COLUMN `agent_profile_fingerprint` char(64) NOT NULL,
  ADD INDEX `idx_tasks_execution_profile` (`execution_profile`),
  ADD INDEX `idx_tasks_agent_profile_fingerprint` (`agent_profile_fingerprint`);

ALTER TABLE `plans`
  ADD COLUMN `execution_profile` varchar(40) NULL;

UPDATE `plans` SET `execution_profile` = 'balanced';

ALTER TABLE `plans`
  MODIFY COLUMN `execution_profile` varchar(40) NOT NULL,
  ADD INDEX `idx_plans_execution_profile` (`execution_profile`);

ALTER TABLE `task_executions`
  ADD COLUMN `execution_profile` varchar(40) NULL,
  ADD COLUMN `provider` varchar(80) NULL,
  ADD COLUMN `model_matrix` json NULL,
  ADD COLUMN `claude_controls` json NULL,
  ADD COLUMN `profile_fingerprint` char(64) NULL;

UPDATE `task_executions` AS `execution`
INNER JOIN `tasks` AS `task` ON `task`.`id` = `execution`.`task_id`
SET `execution`.`execution_profile` = `task`.`execution_profile`,
    `execution`.`provider` = JSON_UNQUOTE(JSON_EXTRACT(`task`.`agent_profile_snapshot`, '$.provider')),
    `execution`.`model_matrix` = JSON_EXTRACT(`task`.`agent_profile_snapshot`, '$.models'),
    `execution`.`claude_controls` = JSON_EXTRACT(`task`.`agent_profile_snapshot`, '$.claude'),
    `execution`.`profile_fingerprint` = `task`.`agent_profile_fingerprint`;

ALTER TABLE `task_executions`
  MODIFY COLUMN `execution_profile` varchar(40) NOT NULL,
  MODIFY COLUMN `provider` varchar(80) NOT NULL,
  MODIFY COLUMN `model_matrix` json NOT NULL,
  MODIFY COLUMN `claude_controls` json NOT NULL,
  MODIFY COLUMN `profile_fingerprint` char(64) NOT NULL,
  ADD INDEX `idx_task_executions_execution_profile` (`execution_profile`),
  ADD INDEX `idx_task_executions_provider` (`provider`),
  ADD INDEX `idx_task_executions_profile_fingerprint` (`profile_fingerprint`),
  DROP COLUMN `model_id`,
  DROP COLUMN `protocol`,
  DROP COLUMN `reasoning_effort`,
  DROP COLUMN `context_window`;

ALTER TABLE `billing_skus`
  ADD COLUMN `execution_profile` varchar(40) NOT NULL DEFAULT '',
  ADD INDEX `idx_billing_skus_execution_profile` (`execution_profile`),
  ADD INDEX `idx_billing_skus_catalog_operation_profile` (`catalog_id`, `operation`, `execution_profile`);

ALTER TABLE `billing_quotes`
  ADD COLUMN `agent_profile_snapshot` json NULL;

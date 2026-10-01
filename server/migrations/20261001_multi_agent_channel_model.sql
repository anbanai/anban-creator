-- One-time identity backfill for the multi-Agent, single-channel contract.
-- Run after AutoMigrate has added the new columns/tables. The statements are
-- deliberately idempotent so a failed deployment can resume safely.

ALTER TABLE `tasks` ADD COLUMN IF NOT EXISTS `agent_id` varchar(80) NULL;
ALTER TABLE `tasks` ADD COLUMN IF NOT EXISTS `channel` varchar(40) NULL;
ALTER TABLE `tasks` ADD COLUMN IF NOT EXISTS `task_kind` varchar(80) NULL;
ALTER TABLE `task_executions` ADD COLUMN IF NOT EXISTS `agent_id` varchar(80) NULL;
ALTER TABLE `task_executions` ADD COLUMN IF NOT EXISTS `channel` varchar(40) NULL;
ALTER TABLE `task_executions` ADD COLUMN IF NOT EXISTS `task_kind` varchar(80) NULL;
ALTER TABLE `analytics_contents` ADD COLUMN IF NOT EXISTS `channel` varchar(40) NULL;

UPDATE `tasks` SET `agent_id` = 'wechat-article', `channel` = 'wechat-article', `task_kind` = 'content_generation'
  WHERE (`agent_id` IS NULL OR `agent_id` = '') AND `type` IN ('article', 'wechat', 'wechat-article');
UPDATE `tasks` SET `agent_id` = 'seednote', `channel` = 'seednote', `task_kind` = CASE
    WHEN `type` IN ('viral_analysis', 'viral-analysis') THEN 'viral_analysis'
    ELSE 'content_generation' END
  WHERE (`agent_id` IS NULL OR `agent_id` = '') AND `type` = 'seednote';
UPDATE `tasks` SET `agent_id` = 'wechat-picture', `channel` = 'wechat-picture', `task_kind` = 'content_generation'
  WHERE (`agent_id` IS NULL OR `agent_id` = '') AND `type` = 'wechat-picture';
-- Plugin-only historical workflows are intentionally not coerced into a
-- content Agent. The migration runner must archive or explicitly map these
-- rows before enforcing the new identity constraints.

UPDATE `task_executions` `e` JOIN `tasks` `t` ON `t`.`id` = `e`.`task_id`
  SET `e`.`agent_id` = `t`.`agent_id`, `e`.`channel` = `t`.`channel`, `e`.`task_kind` = `t`.`task_kind`
  WHERE (`e`.`agent_id` IS NULL OR `e`.`agent_id` = '');
UPDATE `analytics_contents` SET `channel` = CASE
  WHEN `platform` IN ('wechat', 'article') THEN 'wechat-article'
  WHEN `platform` = 'seednote' THEN 'seednote'
  ELSE `platform` END
  WHERE (`channel` IS NULL OR `channel` = '');

-- Article credentials are copied into project_channel_configs by the server
-- migration runner, which can redact and validate JSON without exposing them in
-- this SQL artifact.

-- Public terminal outcome. Raw execution evidence remains internal.
ALTER TABLE `tasks` ADD COLUMN `outcome` json NULL;

-- Draft lifecycle evidence is owned by the execution that created it. Existing
-- rows remain unbound and therefore cannot be inherited by a resumed execution.
ALTER TABLE `wechat_publications`
  ADD COLUMN `execution_id` char(36) NOT NULL DEFAULT '',
  ADD INDEX `idx_wechat_publications_execution_id` (`execution_id`);

-- One-way rename of the Feedback Agent Pack identity from `feedback` to
-- `feedback-analysis`. The Pack id, channel, Agent name, and runtime profile
-- share one kebab-case value, so historical rows must carry the same spelling
-- as newly written rows. The managed task kind, billing operation, and
-- artifact paths are unchanged.
--
-- Stop application traffic before applying this migration. Every statement is
-- idempotent because re-running it matches no rows.

UPDATE `tasks` SET `agent_id` = 'feedback-analysis' WHERE `agent_id` = 'feedback';
UPDATE `tasks` SET `channel` = 'feedback-analysis' WHERE `channel` = 'feedback';
UPDATE `task_executions` SET `agent_id` = 'feedback-analysis' WHERE `agent_id` = 'feedback';
UPDATE `task_executions` SET `channel` = 'feedback-analysis' WHERE `channel` = 'feedback';

-- The legacy `type` column keeps its historical spelling for legacy readers
-- only. It is already normalized into `task_kind` by the identity migration,
-- so it stays untouched here.
-- Server-owned WeChat article publication and recovery provenance.
-- Stop application traffic before applying this forward-only migration.

ALTER TABLE `task_executions`
  ADD COLUMN `purpose` varchar(32) NOT NULL DEFAULT 'primary',
  ADD INDEX `idx_task_executions_purpose` (`purpose`);

-- Keep the first draft/add timestamp immutable. The counter bounds safe
-- retries, while retry authorization is written only after a definitive
-- provider rejection.
ALTER TABLE `wechat_publications`
  ADD COLUMN `draft_add_attempts` int NOT NULL DEFAULT 0,
  ADD COLUMN `draft_retry_authorized_at` datetime(3) NULL;

UPDATE `wechat_publications`
SET `draft_add_attempts` = 1
WHERE `draft_add_attempts` = 0
  AND (
    `draft_add_attempted_at` IS NOT NULL
    OR `draft_media_id` <> ''
    OR `publish_id` <> ''
    OR `msg_data_id` <> ''
    OR `msg_id` <> ''
    OR `status` IN ('drafted', 'publish_submitting', 'publishing', 'published', 'needs_selection')
  );

-- The earlier execution-binding migration deliberately left legacy lifecycle
-- rows unbound. Bind only the current execution when durable provider evidence
-- exists; runtime fingerprint validation still prevents changed content from
-- inheriting the lifecycle.
UPDATE `wechat_publications` AS `publication`
JOIN `tasks` AS `task`
  ON `task`.`id` = `publication`.`task_id`
JOIN `task_executions` AS `execution`
  ON `execution`.`id` = `task`.`current_execution_id`
 AND `execution`.`task_id` = `task`.`id`
SET `publication`.`execution_id` = `execution`.`id`
WHERE `publication`.`execution_id` = ''
  AND `execution`.`draft_delivery_status` IN ('ambiguous', 'succeeded')
  AND (
    `publication`.`draft_add_attempted_at` IS NOT NULL
    OR `publication`.`draft_media_id` <> ''
    OR `publication`.`publish_id` <> ''
    OR `publication`.`msg_data_id` <> ''
    OR `publication`.`msg_id` <> ''
    OR `publication`.`status` IN ('drafted', 'publish_submitting', 'publishing', 'published', 'needs_selection')
  );

-- An ambiguous result is valid only after draft/add may have reached WeChat.
-- Keep every row backed by durable attempt evidence unchanged.
UPDATE `task_executions` AS `execution`
LEFT JOIN `wechat_publications` AS `publication`
  ON `publication`.`task_id` = `execution`.`task_id`
 AND `publication`.`execution_id` = `execution`.`id`
 AND (
   `publication`.`draft_add_attempted_at` IS NOT NULL
   OR `publication`.`draft_media_id` <> ''
   OR `publication`.`publish_id` <> ''
   OR `publication`.`msg_data_id` <> ''
   OR `publication`.`msg_id` <> ''
   OR `publication`.`status` IN ('drafted', 'publish_submitting', 'publishing', 'published', 'needs_selection')
 )
SET `execution`.`draft_delivery_status` = 'blocked',
    `execution`.`draft_delivery_result` = JSON_OBJECT(
      'source', 'publication_history_repair',
      'status', 'blocked',
      'code', 'publication_not_attempted',
      'attempted', FALSE,
      'action', 'retry_draft',
      'occurred_at', UTC_TIMESTAMP()
    )
WHERE `execution`.`draft_delivery_status` = 'ambiguous'
  AND `publication`.`id` IS NULL;

UPDATE `tasks` AS `task`
JOIN `task_executions` AS `execution`
  ON `execution`.`id` = `task`.`current_execution_id`
SET `task`.`outcome` = JSON_SET(
  COALESCE(`task`.`outcome`, JSON_OBJECT()),
  '$.publication', JSON_OBJECT(
    'status', 'blocked',
    'code', 'publication_not_attempted',
    'message', '微信尚未收到草稿请求。',
    'attempted', FALSE,
    'action', IF(
      JSON_UNQUOTE(JSON_EXTRACT(`task`.`outcome`, '$.visual.status')) = 'partial',
      'retry_visuals',
      'retry_draft'
    )
  )
)
WHERE `execution`.`draft_delivery_status` = 'blocked'
  AND JSON_UNQUOTE(JSON_EXTRACT(`execution`.`draft_delivery_result`, '$.code')) = 'publication_not_attempted'
  AND (
    JSON_UNQUOTE(JSON_EXTRACT(`task`.`outcome`, '$.publication.status')) = 'ambiguous'
    OR `task`.`id` = '2e8596be-378c-4671-8701-7d379323f957'
  );

-- Remove the legacy duplicate publication warning while retaining visual and
-- review warnings. Publication state now lives only at outcome.publication.
UPDATE `tasks` AS `task`
SET `task`.`outcome` = JSON_REMOVE(
  `task`.`outcome`,
  REPLACE(
    JSON_UNQUOTE(JSON_SEARCH(`task`.`outcome`, 'one', 'publication_ambiguous', NULL, '$.warnings[*].code')),
    '.code',
    ''
  )
)
WHERE JSON_SEARCH(`task`.`outcome`, 'one', 'publication_ambiguous', NULL, '$.warnings[*].code') IS NOT NULL;

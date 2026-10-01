-- Queryable lifecycle state and immutable revision history for the
-- database-backed six-dimensional profile. AutoMigrate creates these tables
-- for fresh installs; this migration backfills existing projects before the
-- Server begins relying on the read models.

CREATE TABLE IF NOT EXISTS `project_profile_revisions` (
  `id` char(36) NOT NULL,
  `project_id` char(36) NOT NULL,
  `revision` bigint NOT NULL,
  `six_dimensions` json NOT NULL,
  `source_task_id` char(36) NULL,
  `created_at` datetime(3) NOT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_project_profile_revisions_project_revision` (`project_id`, `revision`),
  KEY `idx_project_profile_revisions_source_task_id` (`source_task_id`)
);

INSERT INTO `project_profile_revisions` (`id`, `project_id`, `revision`, `six_dimensions`, `source_task_id`, `created_at`)
SELECT UUID(), `id`, CAST(JSON_UNQUOTE(JSON_EXTRACT(`profile`, '$.version')) AS SIGNED),
       JSON_EXTRACT(`profile`, '$.dimensions'),
       NULLIF(JSON_UNQUOTE(JSON_EXTRACT(`profile`, '$.analysis_task_id')), ''),
       CURRENT_TIMESTAMP(3)
FROM `projects`
WHERE COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(`profile`, '$.version')) AS SIGNED), 0) > 0
  AND JSON_EXTRACT(`profile`, '$.dimensions') IS NOT NULL
  AND NOT EXISTS (
    SELECT 1 FROM `project_profile_revisions` `r`
    WHERE `r`.`project_id` = `projects`.`id`
      AND `r`.`revision` = CAST(JSON_UNQUOTE(JSON_EXTRACT(`projects`.`profile`, '$.version')) AS SIGNED)
  );

CREATE TABLE IF NOT EXISTS `project_profile_states` (
  `project_id` char(36) NOT NULL,
  `status` varchar(20) NOT NULL DEFAULT 'not_started',
  `revision` bigint NOT NULL DEFAULT 0,
  `active_task_id` char(36) NULL,
  `last_error` text NULL,
  `updated_at` datetime(3) NOT NULL,
  PRIMARY KEY (`project_id`),
  KEY `idx_project_profile_states_active_task_id` (`active_task_id`)
);

INSERT INTO `project_profile_states` (`project_id`, `status`, `revision`, `active_task_id`, `last_error`, `updated_at`)
SELECT
  `id`,
  COALESCE(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(`profile`, '$.initialization_status')), ''), 'not_started'),
  COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(`profile`, '$.version')) AS SIGNED), 0),
  NULLIF(JSON_UNQUOTE(JSON_EXTRACT(`profile`, '$.analysis_task_id')), ''),
  COALESCE(JSON_UNQUOTE(JSON_EXTRACT(`profile`, '$.last_error')), ''),
  CURRENT_TIMESTAMP(3)
FROM `projects`
WHERE NOT EXISTS (
  SELECT 1 FROM `project_profile_states` `s` WHERE `s`.`project_id` = `projects`.`id`
);

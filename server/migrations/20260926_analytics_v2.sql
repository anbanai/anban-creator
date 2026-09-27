-- Content analytics v2. Raw legacy tables remain for audit/migration; reads use these projections.
CREATE TABLE IF NOT EXISTS `analytics_project_states` (
 `project_id` CHAR(36) NOT NULL,
 `revision` BIGINT NOT NULL DEFAULT 0,
 `active_generation` BIGINT NOT NULL DEFAULT 1,
 `status` VARCHAR(32) NOT NULL DEFAULT 'ready',
 `updated_at` DATETIME(3) NOT NULL,
 PRIMARY KEY (`project_id`), KEY `idx_analytics_state_status` (`status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS `analytics_contents` (
 `id` VARCHAR(100) NOT NULL, `project_id` CHAR(36) NOT NULL, `platform` VARCHAR(20) NOT NULL,
 `task_id` CHAR(36) NULL, `publication_id` CHAR(36) NULL, `post_id` CHAR(36) NULL,
 `title` VARCHAR(500) NOT NULL DEFAULT '', `content_type` VARCHAR(32) NOT NULL DEFAULT '',
 `status` VARCHAR(32) NOT NULL DEFAULT '', `url` VARCHAR(1000) NOT NULL DEFAULT '',
 `date` DATETIME(3) NULL, `created_at` DATETIME(3) NOT NULL, `updated_at` DATETIME(3) NOT NULL,
 PRIMARY KEY (`id`), KEY `idx_analytics_contents_project` (`project_id`,`platform`,`date`,`id`),
 KEY `idx_analytics_contents_task` (`project_id`,`task_id`), KEY `idx_analytics_contents_post` (`project_id`,`post_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS `analytics_observations` (
 `sequence` BIGINT NOT NULL AUTO_INCREMENT, `tracking_id` CHAR(36) NULL, `id` VARCHAR(100) NOT NULL,
 `project_id` CHAR(36) NOT NULL, `content_id` VARCHAR(100) NOT NULL, `batch_id` CHAR(36) NULL,
 `metric_basis` VARCHAR(16) NOT NULL, `stat_date` CHAR(10) NOT NULL, `source` VARCHAR(32) NOT NULL,
 `source_priority` INT NOT NULL DEFAULT 0, `effective_at` DATETIME(3) NOT NULL, `received_at` DATETIME(3) NOT NULL,
 `revoked_at` DATETIME(3) NULL, `delivered_users` BIGINT NULL, `read_users` BIGINT NULL, `share_users` BIGINT NULL,
 `collection_users` BIGINT NULL, `like_users` BIGINT NULL, `zaikan_users` BIGINT NULL, `comment_count` BIGINT NULL,
 `read_to_follow_users` BIGINT NULL, `exposure_count` BIGINT NULL, `view_count` BIGINT NULL, `like_count` BIGINT NULL,
 `collect_count` BIGINT NULL, `follower_gain_count` BIGINT NULL, `share_count` BIGINT NULL, `barrage_count` BIGINT NULL,
 `read_completion_rate` DECIMAL(38,18) NULL, `average_read_active_time` DECIMAL(38,18) NULL,
 `cover_click_rate` DECIMAL(38,18) NULL, `avg_watch_duration` DECIMAL(38,18) NULL, `delivery_completion_rate` DECIMAL(38,18) NULL,
 PRIMARY KEY (`sequence`), UNIQUE KEY `idx_analytics_observations_id` (`id`),
 KEY `idx_analytics_observations_winner` (`project_id`,`content_id`,`metric_basis`,`stat_date`,`source_priority`,`effective_at`,`sequence`),
 KEY `idx_analytics_observations_batch` (`project_id`,`batch_id`,`revoked_at`), KEY `idx_analytics_observations_tracking` (`tracking_id`,`stat_date`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS `analytics_observation_payloads` (`observation_id` VARCHAR(100) NOT NULL, `payload` LONGTEXT NOT NULL, PRIMARY KEY (`observation_id`)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS `analytics_buckets` (
 `project_id` CHAR(36) NOT NULL, `generation` BIGINT NOT NULL, `content_id` VARCHAR(100) NOT NULL, `metric_basis` VARCHAR(16) NOT NULL,
 `granularity` VARCHAR(8) NOT NULL, `bucket_start` CHAR(10) NOT NULL, `last_stat_date` CHAR(10) NOT NULL DEFAULT '', `observation_id` VARCHAR(100) NOT NULL DEFAULT '', `coverage` BIGINT NOT NULL DEFAULT 0,
 `delivered_users` BIGINT NULL, `read_users` BIGINT NULL, `share_users` BIGINT NULL, `collection_users` BIGINT NULL, `like_users` BIGINT NULL, `zaikan_users` BIGINT NULL, `comment_count` BIGINT NULL, `read_to_follow_users` BIGINT NULL, `exposure_count` BIGINT NULL, `view_count` BIGINT NULL, `like_count` BIGINT NULL, `collect_count` BIGINT NULL, `follower_gain_count` BIGINT NULL, `share_count` BIGINT NULL, `barrage_count` BIGINT NULL,
 `read_completion_rate` DECIMAL(38,18) NULL, `average_read_active_time` DECIMAL(38,18) NULL, `cover_click_rate` DECIMAL(38,18) NULL, `avg_watch_duration` DECIMAL(38,18) NULL, `delivery_completion_rate` DECIMAL(38,18) NULL,
 PRIMARY KEY (`project_id`,`generation`,`content_id`,`metric_basis`,`granularity`,`bucket_start`), KEY `idx_analytics_buckets_range` (`project_id`,`generation`,`metric_basis`,`granularity`,`bucket_start`,`content_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS `analytics_idempotencies` (`project_id` CHAR(36) NOT NULL, `idempotency_key` VARCHAR(128) NOT NULL, `request_hash` CHAR(64) NOT NULL, `batch_id` CHAR(36) NOT NULL, `created_at` DATETIME(3) NOT NULL, PRIMARY KEY (`project_id`,`idempotency_key`), KEY `idx_analytics_idempotency_batch` (`project_id`,`batch_id`)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
CREATE TABLE IF NOT EXISTS `analytics_rebuild_jobs` (`id` CHAR(36) NOT NULL, `project_id` CHAR(36) NOT NULL, `generation` BIGINT NOT NULL, `status` VARCHAR(24) NOT NULL, `cursor_source` VARCHAR(32) NOT NULL DEFAULT '', `cursor_content_id` VARCHAR(100) NOT NULL DEFAULT '', `cursor_basis` VARCHAR(16) NOT NULL DEFAULT '', `cursor_date` CHAR(10) NOT NULL DEFAULT '', `processed` BIGINT NOT NULL DEFAULT 0, `error` TEXT NULL, `report_json` JSON NULL, `created_at` DATETIME(3) NOT NULL, `updated_at` DATETIME(3) NOT NULL, PRIMARY KEY (`id`), KEY `idx_analytics_rebuild_jobs_project_status` (`project_id`,`status`,`updated_at`)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

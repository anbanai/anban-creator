-- WeChat account-level facts. These tables are separate from generic
-- per-content cumulative observations and retain report provenance.
CREATE TABLE IF NOT EXISTS `wechat_account_analytics_batches` (
 `id` CHAR(36) NOT NULL,
 `user_id` CHAR(36) NOT NULL,
 `project_id` CHAR(36) NOT NULL,
 `import_batch_id` CHAR(36) NULL,
 `asset_id` CHAR(36) NULL,
 `file_name` VARCHAR(255) NOT NULL DEFAULT '',
 `sha256` CHAR(64) NULL,
 `source` VARCHAR(255) NOT NULL DEFAULT '',
 `timezone` VARCHAR(64) NOT NULL DEFAULT 'Asia/Shanghai',
 `parser_version` VARCHAR(32) NOT NULL DEFAULT '',
 `status` VARCHAR(32) NOT NULL,
 `idempotency_key` VARCHAR(128) NULL,
 `request_hash` CHAR(64) NULL,
 `raw_provenance` JSON NULL,
 `received_at` DATETIME(3) NOT NULL,
 `effective_at` DATETIME(3) NOT NULL,
 `data_as_of_at` DATETIME(3) NULL,
 `revoked_at` DATETIME(3) NULL,
 `created_at` DATETIME(3) NOT NULL,
 `updated_at` DATETIME(3) NOT NULL,
 PRIMARY KEY (`id`),
 KEY `idx_wechat_account_analytics_batches_project` (`project_id`,`created_at`,`id`),
 KEY `idx_wechat_account_analytics_batches_winner` (`project_id`,`status`,`revoked_at`,`effective_at`,`received_at`,`id`),
 KEY `idx_wechat_account_analytics_batches_revoke` (`project_id`,`revoked_at`),
 UNIQUE KEY `uq_wechat_account_analytics_batches_idempotency` (`project_id`,`idempotency_key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS `wechat_account_analytics_days` (
 `id` CHAR(36) NOT NULL,
 `project_id` CHAR(36) NOT NULL,
 `batch_id` CHAR(36) NOT NULL,
 `stat_date` CHAR(10) NOT NULL,
 `read_users` BIGINT NULL,
 `share_users` BIGINT NULL,
 `original_read_users` BIGINT NULL,
 `like_users` BIGINT NULL,
 `collection_users` BIGINT NULL,
 `comment_count` BIGINT NULL,
 `follower_gain_count` BIGINT NULL,
 `published_articles` BIGINT NULL,
 `source` VARCHAR(255) NOT NULL DEFAULT '',
 `raw_data` JSON NULL,
 `created_at` DATETIME(3) NOT NULL,
 `updated_at` DATETIME(3) NOT NULL,
 PRIMARY KEY (`id`),
 KEY `idx_wechat_account_analytics_days_project_date` (`project_id`,`stat_date`,`batch_id`),
 KEY `idx_wechat_account_analytics_days_batch` (`project_id`,`batch_id`,`stat_date`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS `wechat_traffic_source_days` (
 `id` CHAR(36) NOT NULL,
 `project_id` CHAR(36) NOT NULL,
 `batch_id` CHAR(36) NOT NULL,
 `stat_date` CHAR(10) NOT NULL,
 `source` VARCHAR(40) NOT NULL,
 `original_label` VARCHAR(255) NOT NULL DEFAULT '',
 `read_users` BIGINT NULL,
 `raw_data` JSON NULL,
 `created_at` DATETIME(3) NOT NULL,
 `updated_at` DATETIME(3) NOT NULL,
 PRIMARY KEY (`id`),
 UNIQUE KEY `uq_wechat_traffic_source_days_batch_date_source` (`project_id`,`stat_date`,`batch_id`,`source`),
 KEY `idx_wechat_traffic_source_days_project_date` (`project_id`,`stat_date`,`batch_id`),
 KEY `idx_wechat_traffic_source_days_batch` (`project_id`,`batch_id`,`stat_date`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

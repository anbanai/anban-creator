ALTER TABLE `wechat_publications`
  ADD COLUMN `manual_publish_required` BOOLEAN NOT NULL DEFAULT FALSE AFTER `wechat_status_code`,
  ADD COLUMN `analytics_status` VARCHAR(32) NOT NULL DEFAULT 'not_available' AFTER `manual_publish_required`,
  ADD INDEX `idx_wechat_publications_manual_publish_required` (`manual_publish_required`);

ALTER TABLE `wechat_publications`
  DROP CHECK `chk_wechat_publication_status`;

UPDATE `wechat_publications`
  SET `status` = 'ambiguous'
  WHERE `status` = 'publish_submitting';

ALTER TABLE `wechat_publications`
  ADD CONSTRAINT `chk_wechat_publication_status` CHECK (`status` IN ('drafting','drafted','awaiting_manual_publish','ambiguous','publishing','published','needs_selection','publish_failed','unsupported'));

CREATE TABLE `wechat_account_capabilities` (
  `project_id` CHAR(36) NOT NULL,
  `capability` VARCHAR(64) NOT NULL,
  `status` VARCHAR(32) NOT NULL,
  `last_checked_at` DATETIME(3) NULL,
  `last_wechat_code` BIGINT NOT NULL DEFAULT 0,
  `created_at` DATETIME(3) NULL,
  `updated_at` DATETIME(3) NULL,
  PRIMARY KEY (`project_id`, `capability`),
  KEY `idx_wechat_account_capabilities_status` (`status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE `wechat_publication_attempts` (
  `id` CHAR(36) NOT NULL,
  `publication_id` CHAR(36) NOT NULL,
  `operation` VARCHAR(64) NOT NULL,
  `request_fingerprint` CHAR(64) NOT NULL DEFAULT '',
  `started_at` DATETIME(3) NOT NULL,
  `completed_at` DATETIME(3) NULL,
  `wechat_code` BIGINT NOT NULL DEFAULT 0,
  `wechat_request_id` VARCHAR(191) NOT NULL DEFAULT '',
  `durable_evidence` TEXT NULL,
  `result_status` VARCHAR(32) NOT NULL,
  `created_at` DATETIME(3) NULL,
  `updated_at` DATETIME(3) NULL,
  PRIMARY KEY (`id`),
  KEY `idx_wechat_publication_attempts_publication_id` (`publication_id`),
  KEY `idx_wechat_publication_attempts_operation` (`operation`),
  KEY `idx_wechat_publication_attempts_started_at` (`started_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE `wechat_analytics_import_batches` (
  `id` CHAR(36) NOT NULL,
  `user_id` CHAR(36) NOT NULL,
  `project_id` CHAR(36) NOT NULL,
  `asset_id` CHAR(36) NOT NULL,
  `file_name` VARCHAR(255) NOT NULL,
  `content_type` VARCHAR(120) NOT NULL,
  `file_size` BIGINT NOT NULL,
  `sha256` CHAR(64) NOT NULL,
  `source` VARCHAR(255) NOT NULL DEFAULT '',
  `received_at` DATETIME(3) NOT NULL,
  `data_as_of_at` DATETIME(3) NOT NULL,
  `timezone` VARCHAR(64) NOT NULL,
  `parser_version` VARCHAR(32) NOT NULL,
  `status` VARCHAR(32) NOT NULL,
  `total_rows` BIGINT NOT NULL DEFAULT 0,
  `matched_rows` BIGINT NOT NULL DEFAULT 0,
  `review_rows` BIGINT NOT NULL DEFAULT 0,
  `unmatched_rows` BIGINT NOT NULL DEFAULT 0,
  `invalid_rows` BIGINT NOT NULL DEFAULT 0,
  `error_summary` TEXT NULL,
  `created_at` DATETIME(3) NULL,
  `updated_at` DATETIME(3) NULL,
  PRIMARY KEY (`id`),
  KEY `idx_wechat_import_batches_project_id` (`project_id`),
  KEY `idx_wechat_import_batches_user_id` (`user_id`),
  KEY `idx_wechat_import_batches_data_as_of_at` (`data_as_of_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE `wechat_analytics_import_rows` (
  `id` CHAR(36) NOT NULL,
  `batch_id` CHAR(36) NOT NULL,
  `project_id` CHAR(36) NOT NULL,
  `source_row` BIGINT NOT NULL,
  `raw_data` JSON NOT NULL,
  `source` VARCHAR(255) NOT NULL DEFAULT '',
  `title` VARCHAR(500) NOT NULL,
  `normalized_title` VARCHAR(500) NOT NULL,
  `published_date` DATETIME(3) NULL,
  `article_url` VARCHAR(1000) NOT NULL DEFAULT '',
  `read_users` BIGINT NULL,
  `share_users` BIGINT NULL,
  `read_to_follow_users` BIGINT NULL,
  `delivered_users` BIGINT NULL,
  `delivery_completion_rate` DOUBLE NULL,
  `read_completion_rate` DOUBLE NULL,
  `parse_error` TEXT NULL,
  `match_status` VARCHAR(32) NOT NULL,
  `candidate_publication_id` CHAR(36) NULL,
  `publication_id` CHAR(36) NULL,
  `created_at` DATETIME(3) NULL,
  `updated_at` DATETIME(3) NULL,
  PRIMARY KEY (`id`),
  KEY `idx_wechat_import_rows_batch_id` (`batch_id`),
  KEY `idx_wechat_import_rows_project_id` (`project_id`),
  KEY `idx_wechat_import_rows_match_status` (`match_status`),
  KEY `idx_wechat_import_rows_publication_id` (`publication_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE `wechat_analytics_snapshots` (
  `id` CHAR(36) NOT NULL,
  `project_id` CHAR(36) NOT NULL,
  `publication_id` CHAR(36) NOT NULL,
  `batch_id` CHAR(36) NOT NULL,
  `import_row_id` CHAR(36) NOT NULL,
  `source` VARCHAR(255) NOT NULL DEFAULT '',
  `data_as_of_at` DATETIME(3) NOT NULL,
  `imported_at` DATETIME(3) NOT NULL,
  `read_users` BIGINT NULL,
  `share_users` BIGINT NULL,
  `read_to_follow_users` BIGINT NULL,
  `delivered_users` BIGINT NULL,
  `delivery_completion_rate` DOUBLE NULL,
  `read_completion_rate` DOUBLE NULL,
  `raw_data` JSON NOT NULL,
  `created_at` DATETIME(3) NULL,
  PRIMARY KEY (`id`),
  KEY `idx_wechat_snapshots_project_id` (`project_id`),
  KEY `idx_wechat_snapshots_publication_id` (`publication_id`),
  KEY `idx_wechat_snapshots_batch_id` (`batch_id`),
  KEY `idx_wechat_snapshots_data_as_of_at` (`data_as_of_at`),
  UNIQUE KEY `idx_wechat_analytics_snapshots_import_row_id` (`import_row_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

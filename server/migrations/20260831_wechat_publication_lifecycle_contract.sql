-- Run only after every server using the legacy task publication fields has retired.
-- There is no compatibility or backfill path: this is the clean lifecycle cutover.

ALTER TABLE `tasks`
  DROP COLUMN `published`,
  DROP COLUMN `published_at`,
  DROP COLUMN `publish_approval_state`,
  DROP COLUMN `pending_draft_articles`;

ALTER TABLE `task_executions`
  DROP INDEX `idx_task_executions_publishing_status`,
  CHANGE COLUMN `publishing_status` `draft_delivery_status` varchar(20) NOT NULL DEFAULT '',
  CHANGE COLUMN `publishing_result` `draft_delivery_result` json NULL,
  ADD INDEX `idx_task_executions_draft_delivery_status` (`draft_delivery_status`);

CREATE TABLE `wechat_publications` (
  `id` char(36) NOT NULL,
  `task_id` char(36) NOT NULL,
  `user_id` char(36) NOT NULL,
  `project_id` char(36) NOT NULL,
  `draft_media_id` varchar(191) NOT NULL DEFAULT '',
  `draft_title` varchar(500) NOT NULL DEFAULT '',
  `draft_author` varchar(500) NOT NULL DEFAULT '',
  `draft_digest` text NOT NULL,
  `draft_thumb_media_id` varchar(191) NOT NULL DEFAULT '',
  `draft_content_fingerprint` char(64) NOT NULL DEFAULT '',
  `draft_request_fingerprint` char(64) NOT NULL DEFAULT '',
  `source` varchar(32) NOT NULL,
  `status` varchar(32) NOT NULL,
  `publish_id` varchar(191) NOT NULL DEFAULT '',
  `msg_data_id` varchar(191) NOT NULL DEFAULT '',
  `msg_id` varchar(191) NOT NULL DEFAULT '',
  `article_id` varchar(191) NOT NULL DEFAULT '',
  `article_url` varchar(1000) NOT NULL DEFAULT '',
  `article_index` int NOT NULL DEFAULT 1,
  `wechat_status_code` int NOT NULL DEFAULT 0,
  `draft_created_at` datetime(3) NULL,
  `published_at` datetime(3) NULL,
  `next_check_at` datetime(3) NULL,
  `last_checked_at` datetime(3) NULL,
  `check_attempts` int NOT NULL DEFAULT 0,
  `last_error` text NOT NULL,
  `candidates` json NULL,
  `claim_token` char(36) NOT NULL DEFAULT '',
  `claimed_at` datetime(3) NULL,
  `submit_attempted_at` datetime(3) NULL,
  `created_at` datetime(3) NOT NULL,
  `updated_at` datetime(3) NOT NULL,
  PRIMARY KEY (`id`),
  CONSTRAINT `chk_wechat_publication_source` CHECK (`source` IN ('anban_api','wechat_console')),
  CONSTRAINT `chk_wechat_publication_status` CHECK (`status` IN ('drafting','drafted','publish_submitting','publishing','published','needs_selection','publish_failed','unsupported')),
  UNIQUE KEY `idx_wechat_publications_task_id` (`task_id`),
  KEY `idx_wechat_publications_user_id` (`user_id`),
  KEY `idx_wechat_publications_project_id` (`project_id`),
  KEY `idx_wechat_publications_draft_media_id` (`draft_media_id`),
  KEY `idx_wechat_publications_draft_content_fingerprint` (`draft_content_fingerprint`),
  KEY `idx_wechat_publications_source` (`source`),
  KEY `idx_wechat_publications_status` (`status`),
  KEY `idx_wechat_publications_publish_id` (`publish_id`),
  KEY `idx_wechat_publications_msg_data_id` (`msg_data_id`),
  KEY `idx_wechat_publications_msg_id` (`msg_id`),
  KEY `idx_wechat_publications_article_id` (`article_id`),
  KEY `idx_wechat_publications_draft_created_at` (`draft_created_at`),
  KEY `idx_wechat_publications_published_at` (`published_at`),
  KEY `idx_wechat_publications_next_check_at` (`next_check_at`),
  KEY `idx_wechat_publications_last_checked_at` (`last_checked_at`),
  KEY `idx_wechat_publications_claim_token` (`claim_token`),
  KEY `idx_wechat_publications_claimed_at` (`claimed_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE `wechat_project_reconcile_leases` (
  `project_id` char(36) NOT NULL,
  `lease_until_micros` bigint NOT NULL,
  `created_at` datetime(3) NOT NULL,
  `updated_at` datetime(3) NOT NULL,
  PRIMARY KEY (`project_id`),
  KEY `idx_wechat_project_reconcile_leases_lease_until_micros` (`lease_until_micros`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE `wechat_publication_bindings` (
  `project_id` char(36) NOT NULL,
  `article_id` varchar(191) NOT NULL,
  `publication_id` char(36) NOT NULL,
  `created_at` datetime(3) NOT NULL,
  PRIMARY KEY (`project_id`, `article_id`),
  UNIQUE KEY `idx_wechat_publication_bindings_publication_id` (`publication_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

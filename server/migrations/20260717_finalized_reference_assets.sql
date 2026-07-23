-- Forward-only cutover from expiring upload rows and URL references to
-- immutable, repository-owned assets. Run with old application instances
-- stopped after verifying the old schema exists and the new schema does not.

CREATE TABLE `upload_sessions` (
  `id` char(36) NOT NULL,
  `user_id` char(36) NOT NULL,
  `purpose` varchar(50) NOT NULL,
  `staging_key` varchar(500) NOT NULL,
  `file_name` varchar(255) NOT NULL,
  `content_type` varchar(120) NOT NULL,
  `size` bigint NOT NULL,
  `status` varchar(20) NOT NULL DEFAULT 'pending',
  `expires_at` datetime(3) NOT NULL,
  `finalization_token` char(36) DEFAULT NULL,
  `finalization_claimed_at` datetime(3) DEFAULT NULL,
  `promotion_source_etag` varchar(255) NOT NULL DEFAULT '',
  `finalization_etag` varchar(255) NOT NULL DEFAULT '',
  `asset_id` char(36) DEFAULT NULL,
  `finalized_at` datetime(3) DEFAULT NULL,
  `cleanup_claim_id` char(36) DEFAULT NULL,
  `cleanup_claimed_at` datetime(3) DEFAULT NULL,
  `next_cleanup_at` datetime(3) DEFAULT NULL,
  `expired_at` datetime(3) DEFAULT NULL,
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_upload_sessions_staging_key` (`staging_key`),
  KEY `idx_upload_sessions_user_id` (`user_id`),
  KEY `idx_upload_sessions_purpose` (`purpose`),
  KEY `idx_upload_sessions_status` (`status`),
  KEY `idx_upload_sessions_expires_at` (`expires_at`),
  KEY `idx_upload_sessions_finalization_token` (`finalization_token`),
  KEY `idx_upload_sessions_finalization_claimed_at` (`finalization_claimed_at`),
  KEY `idx_upload_sessions_asset_id` (`asset_id`),
  KEY `idx_upload_sessions_cleanup_claim_id` (`cleanup_claim_id`),
  KEY `idx_upload_sessions_cleanup_claimed_at` (`cleanup_claimed_at`),
  KEY `idx_upload_sessions_next_cleanup_at` (`next_cleanup_at`)
);

CREATE TABLE `assets` (
  `id` char(36) NOT NULL,
  `user_id` char(36) NOT NULL,
  `purpose` varchar(50) NOT NULL,
  `storage_key` varchar(500) NOT NULL,
  `file_name` varchar(255) NOT NULL,
  `content_type` varchar(120) NOT NULL,
  `size` bigint NOT NULL,
  `etag` varchar(255) NOT NULL,
  `created_at` datetime(3) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_assets_storage_key` (`storage_key`),
  KEY `idx_assets_user_id` (`user_id`),
  KEY `idx_assets_purpose` (`purpose`)
);

ALTER TABLE `projects` ADD COLUMN `reference_image_asset_id` char(36) DEFAULT NULL, ADD KEY `idx_projects_reference_image_asset_id` (`reference_image_asset_id`), DROP COLUMN `reference_image_url`;
ALTER TABLE `plans` ADD COLUMN `reference_image_asset_id` char(36) DEFAULT NULL, ADD KEY `idx_plans_reference_image_asset_id` (`reference_image_asset_id`), DROP COLUMN `reference_image_url`;
ALTER TABLE `tasks` ADD COLUMN `reference_image_asset_id` char(36) DEFAULT NULL, ADD KEY `idx_tasks_reference_image_asset_id` (`reference_image_asset_id`), DROP COLUMN `reference_image_url`;

DROP TABLE `pending_uploads`;

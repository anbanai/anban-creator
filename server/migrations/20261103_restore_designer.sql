-- Restore the standalone Designer image workbench without removing existing rows.
CREATE TABLE IF NOT EXISTS `designer_references` (
  `id` char(36) NOT NULL,
  `user_id` char(36) NOT NULL,
  `storage_key` varchar(500) NOT NULL,
  `file_name` varchar(255) NOT NULL,
  `content_type` varchar(120) NOT NULL,
  `size` bigint NOT NULL,
  `created_at` datetime(3) NULL,
  `updated_at` datetime(3) NULL,
  PRIMARY KEY (`id`),
  KEY `idx_designer_references_user_id` (`user_id`),
  KEY `idx_designer_references_storage_key` (`storage_key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS `image_generations` (
  `id` char(36) NOT NULL,
  `user_id` char(36) NOT NULL,
  `project_id` char(36) NOT NULL,
  `prompt` text NOT NULL,
  `revised_prompt` text NULL,
  `provider` varchar(32) NOT NULL,
  `provider_id` varchar(64) NULL,
  `model` varchar(64) NOT NULL,
  `quality` varchar(16) NULL,
  `size` varchar(32) NULL,
  `n` bigint DEFAULT 1,
  `output_format` varchar(16) NULL,
  `output_compression` bigint DEFAULT 0,
  `background` varchar(16) DEFAULT '',
  `background_opacity` bigint NULL,
  `watermark` boolean DEFAULT false,
  `status` varchar(16) NOT NULL DEFAULT 'generating',
  `error` text NULL,
  `input_tokens` bigint NULL,
  `output_tokens` bigint NULL,
  `text_input_tokens` bigint NULL,
  `text_cached_input_tokens` bigint NULL,
  `image_input_tokens` bigint NULL,
  `image_cached_input_tokens` bigint NULL,
  `image_output_tokens` bigint NULL,
  `total_tokens` bigint NULL,
  `reference_files` text NULL,
  `mask_file_id` char(36) NULL,
  `cost` bigint DEFAULT 0,
  `estimated_cost` bigint DEFAULT 0,
  `final_cost` bigint DEFAULT 0,
  `billing_mode` varchar(32) NULL,
  `billing_status` varchar(32) NULL,
  `billing_quote_id` char(36) NULL,
  `billing_charge_id` char(36) NULL,
  `request_fingerprint` char(64) NOT NULL,
  `price_snapshot` json NULL,
  `started_at` datetime(3) NULL,
  `execution_deadline` datetime(3) NULL,
  `completed_at` datetime(3) NULL,
  `created_at` datetime(3) NULL,
  `updated_at` datetime(3) NULL,
  PRIMARY KEY (`id`),
  KEY `idx_image_generations_user_id` (`user_id`),
  KEY `idx_image_generations_project_id` (`project_id`),
  KEY `idx_image_generations_status` (`status`),
  KEY `idx_image_generations_billing_quote_id` (`billing_quote_id`),
  UNIQUE KEY `idx_image_generations_billing_charge_id` (`billing_charge_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS `image_generation_results` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `generation_id` char(36) NOT NULL,
  `image_url` varchar(1024) NULL,
  `image_path` varchar(1024) NULL,
  `width` bigint NULL,
  `height` bigint NULL,
  `index` bigint NULL,
  `file_id` varchar(256) NULL,
  PRIMARY KEY (`id`),
  KEY `idx_image_generation_results_generation_id` (`generation_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Historical installations need the execution deadline added in place.
SET @designer_deadline_exists := (
  SELECT COUNT(*) FROM information_schema.columns
  WHERE table_schema = DATABASE() AND table_name = 'image_generations'
    AND column_name = 'execution_deadline'
);
SET @designer_deadline_sql := IF(
  @designer_deadline_exists = 0,
  'ALTER TABLE `image_generations` ADD COLUMN `execution_deadline` datetime(3) NULL',
  'SELECT 1'
);
PREPARE designer_deadline_stmt FROM @designer_deadline_sql;
EXECUTE designer_deadline_stmt;
DEALLOCATE PREPARE designer_deadline_stmt;

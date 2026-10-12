-- Administrator top-up requests are an auditable operation log. AutoMigrate
-- creates this table during startup; this script is also safe to apply to an
-- existing MySQL installation before or after the server upgrade.
CREATE TABLE IF NOT EXISTS `billing_topup_operations` (
  `id` char(36) NOT NULL,
  `user_id` char(36) NOT NULL,
  `admin_user_id` char(36) NOT NULL,
  `credits` bigint NOT NULL,
  `external_source_type` varchar(40) NOT NULL,
  `external_source_id` varchar(128) NOT NULL,
  `catalog_id` varchar(128) NOT NULL,
  `request_fingerprint` char(64) NOT NULL,
  `idempotency_scope` varchar(80) NOT NULL,
  `idempotency_key` varchar(128) NOT NULL,
  `status` varchar(20) NOT NULL,
  `replay_of_id` char(36) NULL,
  `error_code` varchar(80) NULL,
  `error_message` varchar(255) NULL,
  `entry_id` char(36) NULL,
  `paid_credits_added` bigint NOT NULL DEFAULT 0,
  `debt_repaid_credits` bigint NOT NULL DEFAULT 0,
  `created_at` datetime(3) NOT NULL,
  `updated_at` datetime(3) NOT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_billing_topup_operations_user_id` (`user_id`),
  KEY `idx_billing_topup_operations_admin_user_id` (`admin_user_id`),
  KEY `idx_billing_topup_operation_source` (`external_source_type`, `external_source_id`),
  UNIQUE KEY `idx_billing_topup_operation_key` (`idempotency_scope`, `idempotency_key`),
  KEY `idx_billing_topup_operations_status` (`status`),
  KEY `idx_billing_topup_operations_replay_of_id` (`replay_of_id`),
  KEY `idx_billing_topup_operations_entry_id` (`entry_id`),
  KEY `idx_billing_topup_operations_created_at` (`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

ALTER TABLE `billing_topup_operations`
  ADD COLUMN IF NOT EXISTS `replay_of_id` char(36) NULL;

SET @admin_topup_source_index_exists = (
  SELECT COUNT(*) FROM information_schema.STATISTICS
  WHERE TABLE_SCHEMA = DATABASE()
    AND TABLE_NAME = 'billing_topup_operations'
    AND INDEX_NAME = 'idx_billing_topup_operation_source'
);
SET @admin_topup_source_drop_sql = IF(
  @admin_topup_source_index_exists > 0,
  'ALTER TABLE `billing_topup_operations` DROP INDEX `idx_billing_topup_operation_source`',
  'SELECT 1'
);
PREPARE admin_topup_source_drop_stmt FROM @admin_topup_source_drop_sql;
EXECUTE admin_topup_source_drop_stmt;
DEALLOCATE PREPARE admin_topup_source_drop_stmt;

CREATE INDEX `idx_billing_topup_operation_source`
  ON `billing_topup_operations` (`external_source_type`, `external_source_id`);

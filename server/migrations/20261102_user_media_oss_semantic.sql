-- OSS AI Content Awareness metadata for media-library video assets.
-- Apply after the existing user_media_assets schema migration.
ALTER TABLE `user_media_assets`
  ADD COLUMN IF NOT EXISTS `summary` varchar(255) NULL,
  ADD COLUMN IF NOT EXISTS `analysis_provider` varchar(80) NULL,
  ADD COLUMN IF NOT EXISTS `indexed_at` datetime(3) NULL;
SET @oss_semantic_index_exists := (
  SELECT COUNT(*)
  FROM information_schema.statistics
  WHERE table_schema = DATABASE()
    AND table_name = 'user_media_assets'
    AND index_name = 'idx_user_media_assets_indexed_at'
);
SET @oss_semantic_index_sql := IF(
  @oss_semantic_index_exists = 0,
  'ALTER TABLE `user_media_assets` ADD INDEX `idx_user_media_assets_indexed_at` (`indexed_at`)',
  'SELECT 1'
);
PREPARE oss_semantic_index_stmt FROM @oss_semantic_index_sql;
EXECUTE oss_semantic_index_stmt;
DEALLOCATE PREPARE oss_semantic_index_stmt;

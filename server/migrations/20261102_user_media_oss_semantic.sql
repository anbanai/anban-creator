-- OSS AI Content Awareness metadata for media-library video assets.
-- Apply after the existing user_media_assets schema migration.
--
-- MySQL 8 has no `ADD COLUMN IF NOT EXISTS` (that is MariaDB-only), so the
-- ALTER is rebuilt from the live schema: only missing columns are added, and
-- re-running the script becomes a no-op.
SET @oss_semantic_missing_columns := (
  SELECT CONCAT_WS(', ',
      IF(COUNT(CASE WHEN column_name = 'summary' THEN 1 END) = 0,
        'ADD COLUMN `summary` varchar(255) NULL', NULL),
      IF(COUNT(CASE WHEN column_name = 'analysis_provider' THEN 1 END) = 0,
        'ADD COLUMN `analysis_provider` varchar(80) NULL', NULL),
      IF(COUNT(CASE WHEN column_name = 'indexed_at' THEN 1 END) = 0,
        'ADD COLUMN `indexed_at` datetime(3) NULL', NULL))
  FROM information_schema.columns
  WHERE table_schema = DATABASE()
    AND table_name = 'user_media_assets'
);
SET @oss_semantic_column_sql := IF(
  COALESCE(@oss_semantic_missing_columns, '') = '',
  'SELECT 1',
  CONCAT('ALTER TABLE `user_media_assets` ', @oss_semantic_missing_columns)
);
PREPARE oss_semantic_column_stmt FROM @oss_semantic_column_sql;
EXECUTE oss_semantic_column_stmt;
DEALLOCATE PREPARE oss_semantic_column_stmt;

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

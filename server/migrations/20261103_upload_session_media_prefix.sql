-- Freeze the prefix at prepare time so retries/cleanup survive config changes.
-- Empty legacy values retain assets/media-library/. Run before custom prefixes.
SET @media_prefix_column_exists := (
  SELECT COUNT(*) FROM information_schema.columns
  WHERE table_schema = DATABASE() AND table_name = 'upload_sessions'
    AND column_name = 'media_library_prefix'
);
SET @media_prefix_column_sql := IF(
  @media_prefix_column_exists = 0,
  'ALTER TABLE `upload_sessions` ADD COLUMN `media_library_prefix` varchar(120) NOT NULL DEFAULT ''''',
  'SELECT 1'
);
PREPARE media_prefix_column_stmt FROM @media_prefix_column_sql;
EXECUTE media_prefix_column_stmt;
DEALLOCATE PREPARE media_prefix_column_stmt;

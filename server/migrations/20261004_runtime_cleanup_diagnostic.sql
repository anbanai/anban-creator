-- Private cleanup failure evidence; safe on partially AutoMigrated MySQL schema.
SET @cleanup_diagnostic_exists = (
 SELECT COUNT(*) FROM information_schema.COLUMNS
 WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'task_executions'
   AND COLUMN_NAME = 'cleanup_diagnostic'
);
SET @cleanup_diagnostic_sql = IF(@cleanup_diagnostic_exists = 0,
 'ALTER TABLE `task_executions` ADD COLUMN `cleanup_diagnostic` text NULL',
 'SELECT 1');
PREPARE cleanup_diagnostic_stmt FROM @cleanup_diagnostic_sql;
EXECUTE cleanup_diagnostic_stmt;
DEALLOCATE PREPARE cleanup_diagnostic_stmt;

-- MySQL 8.0.16+; stop application writers and take a backup first.
-- Narrow forward repair for both old and partially AutoMigrated schemas.
-- No lifecycle row is remapped. Invalid existing statuses make ALTER fail.
-- Drop/add in one ALTER keeps the existing guard if validation fails.
SET @wechat_status_check_exists = (
  SELECT COUNT(*) FROM information_schema.TABLE_CONSTRAINTS
  WHERE CONSTRAINT_SCHEMA = DATABASE()
    AND TABLE_NAME = 'wechat_publications'
    AND CONSTRAINT_NAME = 'chk_wechat_publication_status'
    AND CONSTRAINT_TYPE = 'CHECK'
);
SET @wechat_status_check_sql = CONCAT(
  'ALTER TABLE `wechat_publications` ',
  IF(@wechat_status_check_exists > 0, 'DROP CHECK `chk_wechat_publication_status`, ', ''),
  'ADD CONSTRAINT `chk_wechat_publication_status` CHECK (`status` IN (''drafting'',''drafted'',''awaiting_manual_publish'',''ambiguous'',''publishing'',''published'',''needs_selection'',''publish_failed'',''unsupported'')) ENFORCED'
);
PREPARE wechat_status_check_stmt FROM @wechat_status_check_sql;
EXECUTE wechat_status_check_stmt;
DEALLOCATE PREPARE wechat_status_check_stmt;

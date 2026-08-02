-- Run only after every server pod using the legacy template fields is retired.
-- The startup migration performs the repeatable expand/backfill phase.
DROP PROCEDURE IF EXISTS `assert_seednote_templates_contract_ready`;
DROP PROCEDURE IF EXISTS `apply_seednote_templates_contract_schema`;

DELIMITER //

CREATE PROCEDURE `assert_seednote_templates_contract_ready`()
BEGIN
  IF EXISTS (
    SELECT 1 FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'templates' AND COLUMN_NAME = 'style_prompt'
  ) THEN
    SET @final_template_prompt_backfill_sql =
      'UPDATE `templates` SET `prompt` = `style_prompt` WHERE TRIM(COALESCE(`prompt`, '''')) = '''' AND TRIM(COALESCE(`style_prompt`, '''')) <> ''''';
    PREPARE final_template_prompt_backfill_statement FROM @final_template_prompt_backfill_sql;
    EXECUTE final_template_prompt_backfill_statement;
    DEALLOCATE PREPARE final_template_prompt_backfill_statement;
  END IF;

  IF EXISTS (
    SELECT 1 FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'projects' AND COLUMN_NAME = 'template_id'
  ) THEN
    SET @final_project_style_backfill_sql =
      'UPDATE `projects` p JOIN `templates` t ON t.`id` = p.`template_id` SET p.`style` = t.`prompt` WHERE TRIM(COALESCE(p.`style`, '''')) = '''' AND TRIM(COALESCE(p.`template_id`, '''')) <> '''' AND TRIM(COALESCE(t.`prompt`, '''')) <> ''''';
    PREPARE final_project_style_backfill_statement FROM @final_project_style_backfill_sql;
    EXECUTE final_project_style_backfill_statement;
    DEALLOCATE PREPARE final_project_style_backfill_statement;

  END IF;

  IF EXISTS (
    SELECT 1 FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'templates' AND COLUMN_NAME = 'style_prompt'
  ) THEN
    SET @template_prompt_backfill_sql =
      'SELECT COUNT(*) INTO @template_prompt_incomplete FROM `templates` WHERE TRIM(COALESCE(`prompt`, '''')) = '''' AND TRIM(COALESCE(`style_prompt`, '''')) <> ''''';
    PREPARE template_prompt_backfill_statement FROM @template_prompt_backfill_sql;
    EXECUTE template_prompt_backfill_statement;
    DEALLOCATE PREPARE template_prompt_backfill_statement;
    IF COALESCE(@template_prompt_incomplete, 0) > 0 THEN
      SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'template prompt backfill is incomplete';
    END IF;
  END IF;

  IF EXISTS (
    SELECT 1 FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'projects' AND COLUMN_NAME = 'template_id'
  ) THEN
    SET @project_style_backfill_sql =
      'SELECT COUNT(*) INTO @project_style_incomplete FROM `projects` p JOIN `templates` t ON t.`id` = p.`template_id` WHERE TRIM(COALESCE(p.`style`, '''')) = '''' AND TRIM(COALESCE(p.`template_id`, '''')) <> '''' AND TRIM(COALESCE(t.`prompt`, '''')) <> ''''';
    PREPARE project_style_backfill_statement FROM @project_style_backfill_sql;
    EXECUTE project_style_backfill_statement;
    DEALLOCATE PREPARE project_style_backfill_statement;
    IF COALESCE(@project_style_incomplete, 0) > 0 THEN
      SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'project visual style backfill is incomplete';
    END IF;
  END IF;
END//

DELIMITER ;

CALL `assert_seednote_templates_contract_ready`();
DROP PROCEDURE `assert_seednote_templates_contract_ready`;

DELIMITER //

CREATE PROCEDURE `apply_seednote_templates_contract_schema`()
BEGIN
  IF EXISTS (
    SELECT 1 FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'templates' AND COLUMN_NAME = 'style_prompt'
  ) THEN
    ALTER TABLE `templates` DROP COLUMN `style_prompt`;
  END IF;

  IF EXISTS (
    SELECT 1 FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'projects' AND COLUMN_NAME = 'template_id'
  ) THEN
    ALTER TABLE `projects` DROP COLUMN `template_id`;
  END IF;
END//

DELIMITER ;

CALL `apply_seednote_templates_contract_schema`();
DROP PROCEDURE `apply_seednote_templates_contract_schema`;

-- Run agent-profile-envs-backfill -verify-only immediately before this file.
DROP PROCEDURE IF EXISTS `assert_agent_profile_envs_contract_ready`;
DROP PROCEDURE IF EXISTS `apply_agent_profile_envs_contract_schema`;

DELIMITER //

CREATE PROCEDURE `assert_agent_profile_envs_contract_ready`()
BEGIN
  IF EXISTS (
    SELECT 1 FROM `tasks`
    WHERE COALESCE(`execution_profile`, '') NOT IN ('effective', 'balanced', 'quality')
       OR COALESCE(JSON_UNQUOTE(JSON_EXTRACT(`agent_profile_snapshot`, '$.schema_version')), '') <> '3'
       OR JSON_EXTRACT(`agent_profile_snapshot`, '$.envs') IS NULL
       OR JSON_EXTRACT(`agent_profile_snapshot`, '$.envs.ANTHROPIC_AUTH_TOKEN') IS NOT NULL
    LIMIT 1
  ) THEN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'tasks are not ready for agent profile env contract';
  END IF;

  IF EXISTS (
    SELECT 1 FROM `plans`
    WHERE COALESCE(`execution_profile`, '') NOT IN ('effective', 'balanced', 'quality')
    LIMIT 1
  ) THEN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'plans retain obsolete agent profile ids';
  END IF;

  IF EXISTS (
    SELECT 1 FROM `task_executions`
    WHERE COALESCE(`execution_profile`, '') NOT IN ('effective', 'balanced', 'quality')
       OR `profile_envs` IS NULL
       OR JSON_EXTRACT(`profile_envs`, '$.ANTHROPIC_AUTH_TOKEN') IS NOT NULL
    LIMIT 1
  ) THEN
    SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'task executions are not ready for agent profile env contract';
  END IF;
END//

DELIMITER ;

CALL `assert_agent_profile_envs_contract_ready`();
DROP PROCEDURE `assert_agent_profile_envs_contract_ready`;

DELIMITER //

CREATE PROCEDURE `apply_agent_profile_envs_contract_schema`()
BEGIN
  IF EXISTS (
    SELECT 1 FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'task_executions'
      AND COLUMN_NAME = 'profile_envs' AND IS_NULLABLE = 'YES'
  ) THEN
    ALTER TABLE `task_executions` MODIFY COLUMN `profile_envs` json NOT NULL;
  END IF;

  IF EXISTS (
    SELECT 1 FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'task_executions' AND COLUMN_NAME = 'model_matrix'
  ) THEN
    ALTER TABLE `task_executions` DROP COLUMN `model_matrix`;
  END IF;

  IF EXISTS (
    SELECT 1 FROM information_schema.COLUMNS
    WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'task_executions' AND COLUMN_NAME = 'claude_controls'
  ) THEN
    ALTER TABLE `task_executions` DROP COLUMN `claude_controls`;
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM information_schema.TABLE_CONSTRAINTS
    WHERE CONSTRAINT_SCHEMA = DATABASE() AND TABLE_NAME = 'task_executions'
      AND CONSTRAINT_NAME = 'chk_task_executions_execution_profile_v3'
  ) THEN
    ALTER TABLE `task_executions`
      ADD CONSTRAINT `chk_task_executions_execution_profile_v3` CHECK (`execution_profile` IN ('effective', 'balanced', 'quality'));
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM information_schema.TABLE_CONSTRAINTS
    WHERE CONSTRAINT_SCHEMA = DATABASE() AND TABLE_NAME = 'tasks'
      AND CONSTRAINT_NAME = 'chk_tasks_execution_profile_v3'
  ) THEN
    ALTER TABLE `tasks`
      ADD CONSTRAINT `chk_tasks_execution_profile_v3` CHECK (`execution_profile` IN ('effective', 'balanced', 'quality'));
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM information_schema.TABLE_CONSTRAINTS
    WHERE CONSTRAINT_SCHEMA = DATABASE() AND TABLE_NAME = 'plans'
      AND CONSTRAINT_NAME = 'chk_plans_execution_profile_v3'
  ) THEN
    ALTER TABLE `plans`
      ADD CONSTRAINT `chk_plans_execution_profile_v3` CHECK (`execution_profile` IN ('effective', 'balanced', 'quality'));
  END IF;
END//

DELIMITER ;

CALL `apply_agent_profile_envs_contract_schema`();
DROP PROCEDURE `apply_agent_profile_envs_contract_schema`;

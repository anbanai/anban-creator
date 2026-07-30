-- Run agent-profile-envs-backfill -verify-only immediately before this file.
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

ALTER TABLE `task_executions`
  MODIFY COLUMN `profile_envs` json NOT NULL,
  DROP COLUMN `model_matrix`,
  DROP COLUMN `claude_controls`;

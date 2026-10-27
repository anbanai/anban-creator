-- Existing deployments with the 20261026 interaction tables need durable
-- user-scoped attempt identity and commit-claim fencing before startup.
UPDATE assistant_workspaces w
JOIN assistant_sessions s ON s.workspace_id = w.id
SET runtime_status = 'closed',
    closed_at = COALESCE(w.closed_at, CURRENT_TIMESTAMP(6)),
    updated_at = CURRENT_TIMESTAMP(6)
WHERE s.workspace_id <> '';

ALTER TABLE assistant_interaction_attempts
  ADD COLUMN user_id char(36) NOT NULL DEFAULT '' AFTER interaction_id;

UPDATE assistant_interaction_attempts attempt
JOIN assistant_interactions interaction ON interaction.id = attempt.interaction_id
SET user_id = interaction.user_id
WHERE attempt.user_id = '';

ALTER TABLE assistant_interaction_attempts
  ADD COLUMN claim_token char(36) NOT NULL DEFAULT '' AFTER status,
  ADD COLUMN claim_expires_at datetime(6) NULL AFTER claim_token,
  ADD UNIQUE KEY idx_assistant_interaction_attempt_user_key (user_id, operation, attempt_id);

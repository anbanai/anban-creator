-- Durable home Assistant interaction boundary.
-- Apply after model AutoMigrate. Existing logical sessions are intentionally
-- not backfilled; they remain abandoned and can only be replaced by a new
-- interaction or a separately durable draft/checkpoint.

-- Retention policy:
--   interaction drafts and commit receipts: 30 DAY
--   interaction events and attempts: 24 HOUR
-- Cleanup is performed by the Server scheduler after these tables exist.

-- The homepage used AssistantWorkspace-backed logical sessions before this
-- boundary. Keep an auditable cutover record and fence those sessions without
-- copying their in-flight transcript into the new interaction tables. Standalone
-- /interactive sessions have an empty workspace_id and are intentionally left
-- outside this cutover.
CREATE TABLE IF NOT EXISTS assistant_interaction_legacy_migrations (
  legacy_session_id char(36) NOT NULL PRIMARY KEY,
  migration_key varchar(64) NOT NULL,
  migrated_at datetime(6) NOT NULL,
  KEY idx_assistant_interaction_legacy_migration_key (migration_key)
);

INSERT INTO assistant_interaction_legacy_migrations (legacy_session_id, migration_key, migrated_at)
SELECT s.id, 'homepage_interaction_cutover', CURRENT_TIMESTAMP(6)
FROM assistant_sessions s
WHERE s.workspace_id <> ''
  AND NOT EXISTS (
    SELECT 1 FROM assistant_interaction_legacy_migrations m
    WHERE m.legacy_session_id = s.id
  );

UPDATE assistant_sessions s
SET status = 'abandoned', closed_at = COALESCE(closed_at, CURRENT_TIMESTAMP(6)), updated_at = CURRENT_TIMESTAMP(6)
WHERE s.workspace_id <> ''
  AND s.status NOT IN ('closed', 'expired', 'interrupted', 'abandoned');

-- Old workspace rows are also counted by the standalone interactive-session
-- admission limit. Close them so abandoned homepage sessions cannot consume
-- capacity after the interaction cutover.
UPDATE assistant_workspaces w
JOIN assistant_sessions s ON s.workspace_id = w.id
SET w.runtime_status = 'closed',
    w.closed_at = COALESCE(w.closed_at, CURRENT_TIMESTAMP(6)),
    w.updated_at = CURRENT_TIMESTAMP(6)
WHERE s.workspace_id <> '';

CREATE TABLE IF NOT EXISTS assistant_interactions (
  id char(36) NOT NULL PRIMARY KEY,
  user_id char(36) NOT NULL,
  mode_id varchar(20) NOT NULL,
  project_id char(36) NOT NULL DEFAULT '',
  content_id char(36) NOT NULL DEFAULT '',
  scope varchar(192) NOT NULL DEFAULT '',
  active_scope varchar(192) NULL,
  lifecycle varchar(32) NOT NULL,
  generation bigint NOT NULL DEFAULT 1,
  command_sequence bigint unsigned NOT NULL DEFAULT 0,
  expires_at datetime(6) NOT NULL,
  last_activity_at datetime(6) NOT NULL,
  created_at datetime(6) NOT NULL,
  updated_at datetime(6) NOT NULL,
  closed_at datetime(6) NULL,
  KEY idx_assistant_interactions_user (user_id),
  KEY idx_assistant_interactions_scope (user_id, project_id, content_id, lifecycle),
  UNIQUE KEY idx_assistant_interactions_active_scope (active_scope),
  KEY idx_assistant_interactions_expiry (expires_at),
  KEY idx_assistant_interactions_activity (last_activity_at)
);

CREATE TABLE IF NOT EXISTS assistant_interaction_runtimes (
  id char(36) NOT NULL PRIMARY KEY,
  interaction_id char(36) NOT NULL,
  generation bigint NOT NULL,
  server_instance varchar(128) NOT NULL,
  runtime_id varchar(128) NOT NULL,
  logical_session char(36) NOT NULL,
  token_hash char(64) NOT NULL,
  status varchar(24) NOT NULL,
  started_at datetime(6) NOT NULL,
  closed_at datetime(6) NULL,
  created_at datetime(6) NOT NULL,
  updated_at datetime(6) NOT NULL,
  UNIQUE KEY idx_assistant_interaction_runtime_generation (interaction_id, generation),
  KEY idx_assistant_interaction_runtime_status (interaction_id, generation, status)
);

CREATE TABLE IF NOT EXISTS assistant_interaction_drafts (
  id char(36) NOT NULL PRIMARY KEY,
  interaction_id char(36) NOT NULL,
  mode_id varchar(20) NOT NULL,
  revision bigint NOT NULL,
  payload json NOT NULL,
  payload_digest char(64) NOT NULL,
  ready boolean NOT NULL DEFAULT false,
  confirmation_state varchar(24) NOT NULL,
  source_generation bigint NOT NULL,
  expires_at datetime(6) NOT NULL,
  created_at datetime(6) NOT NULL,
  updated_at datetime(6) NOT NULL,
  UNIQUE KEY idx_assistant_interaction_draft_revision (interaction_id, mode_id, revision),
  KEY idx_assistant_interaction_draft_expiry (expires_at)
);

CREATE TABLE IF NOT EXISTS assistant_interaction_events (
  id char(36) NOT NULL PRIMARY KEY,
  interaction_id char(36) NOT NULL,
  generation bigint NOT NULL,
  sequence bigint unsigned NOT NULL,
  type varchar(64) NOT NULL,
  data json NOT NULL,
  created_at datetime(6) NOT NULL,
  UNIQUE KEY idx_assistant_interaction_event_sequence (interaction_id, sequence),
  KEY idx_assistant_interaction_event_generation (interaction_id, generation, sequence),
  KEY idx_assistant_interaction_event_created (created_at)
);

CREATE TABLE IF NOT EXISTS assistant_interaction_attempts (
  id char(36) NOT NULL PRIMARY KEY,
  interaction_id char(36) NOT NULL,
  attempt_id varchar(128) NOT NULL,
  operation varchar(24) NOT NULL,
  generation bigint NOT NULL,
  payload_digest char(64) NOT NULL,
  status varchar(24) NOT NULL DEFAULT 'pending',
  result json NULL,
  error_code varchar(96) NOT NULL DEFAULT '',
  created_at datetime(6) NOT NULL,
  expires_at datetime(6) NOT NULL,
  UNIQUE KEY idx_assistant_interaction_attempt_key (interaction_id, attempt_id),
  KEY idx_assistant_interaction_attempt_expiry (expires_at)
);

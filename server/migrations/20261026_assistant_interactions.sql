-- Durable home Assistant interaction boundary.
-- Apply after model AutoMigrate. Existing logical sessions are intentionally
-- not backfilled; they remain abandoned and can only be replaced by a new
-- interaction or a separately durable draft/checkpoint.

-- Retention policy:
--   interaction drafts and commit receipts: 30 DAY
--   interaction events and attempts: 24 HOUR
-- Cleanup is performed by the Server scheduler after these tables exist.

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
  sequence bigint unsigned NOT NULL,
  type varchar(64) NOT NULL,
  data json NOT NULL,
  created_at datetime(6) NOT NULL,
  UNIQUE KEY idx_assistant_interaction_event_sequence (interaction_id, sequence),
  KEY idx_assistant_interaction_event_created (created_at)
);

CREATE TABLE IF NOT EXISTS assistant_interaction_attempts (
  id char(36) NOT NULL PRIMARY KEY,
  interaction_id char(36) NOT NULL,
  attempt_id varchar(128) NOT NULL,
  operation varchar(24) NOT NULL,
  generation bigint NOT NULL,
  payload_digest char(64) NOT NULL,
  result json NULL,
  error_code varchar(96) NOT NULL DEFAULT '',
  created_at datetime(6) NOT NULL,
  expires_at datetime(6) NOT NULL,
  UNIQUE KEY idx_assistant_interaction_attempt_key (interaction_id, attempt_id),
  KEY idx_assistant_interaction_attempt_expiry (expires_at)
);

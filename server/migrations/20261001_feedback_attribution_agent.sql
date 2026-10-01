-- Feedback attribution target and managed-Agent linkage.
ALTER TABLE feedback_jobs
  ADD COLUMN target_content_id VARCHAR(100) NOT NULL DEFAULT '',
  ADD COLUMN trigger_source VARCHAR(32) NOT NULL DEFAULT '',
  ADD COLUMN task_id CHAR(36) NOT NULL DEFAULT '',
  ADD COLUMN execution_id CHAR(36) NOT NULL DEFAULT '';

CREATE INDEX idx_feedback_jobs_target ON feedback_jobs (project_id, target_content_id, analytics_revision);
CREATE INDEX idx_feedback_jobs_execution ON feedback_jobs (task_id, execution_id);

ALTER TABLE feedback_insights
  ADD COLUMN execution_id CHAR(36) NOT NULL DEFAULT '',
  ADD COLUMN target_content_id VARCHAR(100) NOT NULL DEFAULT '',
  ADD COLUMN baseline_scope VARCHAR(64) NOT NULL DEFAULT '',
  ADD COLUMN trigger_source VARCHAR(32) NOT NULL DEFAULT '',
  ADD COLUMN promotion_status VARCHAR(16) NOT NULL DEFAULT 'none',
  ADD COLUMN confirmed_by CHAR(36) NOT NULL DEFAULT '',
  ADD COLUMN confirmed_at DATETIME(3) NULL,
  ADD COLUMN validated_at DATETIME(3) NULL,
  ADD COLUMN memory_revision BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN agent_pack_digest CHAR(64) NOT NULL DEFAULT '',
  ADD COLUMN analysis_artifact_path VARCHAR(500) NOT NULL DEFAULT '',
  ADD COLUMN evidence_artifact_path VARCHAR(500) NOT NULL DEFAULT '',
  ADD COLUMN analysis_artifact_hash CHAR(64) NOT NULL DEFAULT '',
  ADD COLUMN evidence_artifact_hash CHAR(64) NOT NULL DEFAULT '';

CREATE INDEX idx_feedback_insights_target ON feedback_insights (project_id, target_content_id, analytics_revision);
CREATE INDEX idx_feedback_insights_promotion ON feedback_insights (project_id, promotion_status);
CREATE INDEX idx_feedback_insights_execution ON feedback_insights (execution_id);

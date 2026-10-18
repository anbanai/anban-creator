-- Feedback attribution jobs retain the immutable context consumed by the
-- managed Agent. Apply after 20261001_feedback_attribution_agent.sql.
ALTER TABLE feedback_jobs
  ADD COLUMN frozen_context_json JSON NULL;

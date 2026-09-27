-- Deploy the Server containing recoverWechatImportRows on ALL workers first.
-- Requires the existing analytics v2 tables, Redis and the Asynq analysis worker.
-- Run this entire transaction in one SQL session. On ANY error: ROLLBACK.
-- This queues all WeChat projects with saved import rows. No data is deleted.
-- Re-running is safe: the fixed per-project v2 receipt prevents another job.
-- A failed v2 job is re-queued so a transient worker/database failure can be
-- recovered without manual deletes. Published/active jobs remain unchanged.
-- Do not run the old analytics_legacy_sql_backfill script again after recovery.

START TRANSACTION;

-- Upsert also locks each target state, serializing imports and concurrent runs.
INSERT INTO analytics_project_states
  (project_id, revision, active_generation, status, updated_at)
SELECT p.id, 0, 0, 'ready', UTC_TIMESTAMP(3)
FROM projects p
WHERE p.platform = 'article'
  AND EXISTS (SELECT 1 FROM wechat_analytics_import_rows r WHERE r.project_id = p.id)
ORDER BY p.id
ON DUPLICATE KEY UPDATE project_id = VALUES(project_id);

INSERT INTO analytics_rebuild_jobs
  (id, project_id, generation, status, cursor_source, cursor_content_id,
   cursor_basis, cursor_date, processed, error, report_json, created_at, updated_at)
SELECT MD5(CONCAT('wechat-import-history-v2:', s.project_id)),
       s.project_id, s.active_generation + 1, 'queued', '', '', '', '', 0,
       NULL, NULL, UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)
FROM analytics_project_states s
JOIN projects p ON p.id = s.project_id AND p.platform = 'article'
WHERE s.status = 'ready'
  AND EXISTS (SELECT 1 FROM wechat_analytics_import_rows r WHERE r.project_id = s.project_id)
  AND NOT EXISTS (
    SELECT 1 FROM analytics_rebuild_jobs j
    WHERE j.project_id = s.project_id AND j.status IN ('queued', 'running')
  )
ON DUPLICATE KEY UPDATE
  generation = IF(analytics_rebuild_jobs.status = 'failed', VALUES(generation), analytics_rebuild_jobs.generation),
  cursor_source = IF(analytics_rebuild_jobs.status = 'failed', '', analytics_rebuild_jobs.cursor_source),
  cursor_content_id = IF(analytics_rebuild_jobs.status = 'failed', '', analytics_rebuild_jobs.cursor_content_id),
  cursor_basis = IF(analytics_rebuild_jobs.status = 'failed', '', analytics_rebuild_jobs.cursor_basis),
  cursor_date = IF(analytics_rebuild_jobs.status = 'failed', '', analytics_rebuild_jobs.cursor_date),
  processed = IF(analytics_rebuild_jobs.status = 'failed', 0, analytics_rebuild_jobs.processed),
  error = IF(analytics_rebuild_jobs.status = 'failed', NULL, analytics_rebuild_jobs.error),
  report_json = IF(analytics_rebuild_jobs.status = 'failed', NULL, analytics_rebuild_jobs.report_json),
  updated_at = IF(analytics_rebuild_jobs.status = 'failed', VALUES(updated_at), analytics_rebuild_jobs.updated_at),
  status = IF(analytics_rebuild_jobs.status = 'failed', 'queued', analytics_rebuild_jobs.status);

UPDATE analytics_project_states s
JOIN analytics_rebuild_jobs j
  ON j.id = MD5(CONCAT('wechat-import-history-v2:', s.project_id))
 AND j.project_id = s.project_id
SET s.status = 'rebuilding', s.updated_at = UTC_TIMESTAMP(3)
WHERE j.status = 'queued' AND s.status = 'ready'
  AND j.generation > s.active_generation;

COMMIT;

-- This query can also be run independently in a later/new SQL session.
-- queued/running is NOT completion. published + ready is required.
SELECT j.project_id, j.id AS job_id, j.status AS job_status,
       s.status AS project_status, s.active_generation, j.generation,
       j.processed, j.report_json, j.error, j.updated_at
FROM analytics_rebuild_jobs j
JOIN analytics_project_states s ON s.project_id = j.project_id
WHERE j.id = MD5(CONCAT('wechat-import-history-v2:', j.project_id))
ORDER BY j.project_id;

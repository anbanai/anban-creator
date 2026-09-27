-- Analytics v2 legacy backfill for environments where the Go rebuild command
-- cannot be run.  Requires MySQL 8.0+ and the tables from
-- 20260926_analytics_v2.sql.
--
-- Before running:
--   1. Take a MySQL backup and stop imports/automatic collectors.
--   2. Set @project_id to one project UUID, or leave it NULL to migrate every
--      project represented in the two legacy import tables.
--   3. Run this file as one session.  Temporary tables and user variables are
--      session scoped.
--
-- The script is rerunnable.  It replaces only observations created by this
-- script (legacy:wechat_import:* and legacy:seednote_import:*) and publishes a
-- new analytics generation after all projections have been rebuilt.  Legacy
-- tables are never deleted.

SET @project_id := NULL;
SET SESSION time_zone = '+00:00';
SET SESSION group_concat_max_len = 1048576;

START TRANSACTION;

DROP TEMPORARY TABLE IF EXISTS tmp_analytics_scope;
CREATE TEMPORARY TABLE tmp_analytics_scope (
  project_id CHAR(36) NOT NULL PRIMARY KEY
) ENGINE=InnoDB;

INSERT INTO tmp_analytics_scope(project_id)
SELECT DISTINCT s.project_id
FROM wechat_analytics_snapshots s
WHERE (@project_id IS NULL OR s.project_id = @project_id)
UNION
SELECT DISTINCT p.project_id
FROM seednote_metric_versions v
JOIN seednote_posts p ON p.id = v.post_id
WHERE (@project_id IS NULL OR p.project_id = @project_id);

-- A project must not be written by the application while this SQL rebuild is
-- running.  The application treats this status as a write gate.
INSERT INTO analytics_project_states(project_id, revision, active_generation, status, updated_at)
SELECT project_id, 0, 1, 'rebuilding', UTC_TIMESTAMP(3)
FROM tmp_analytics_scope
ON DUPLICATE KEY UPDATE status = 'rebuilding', updated_at = UTC_TIMESTAMP(3);

DROP TEMPORARY TABLE IF EXISTS tmp_analytics_generation;
CREATE TEMPORARY TABLE tmp_analytics_generation (
  project_id CHAR(36) NOT NULL PRIMARY KEY,
  generation BIGINT NOT NULL
) ENGINE=InnoDB;

INSERT INTO tmp_analytics_generation(project_id, generation)
SELECT s.project_id, s.active_generation + 1
FROM analytics_project_states s
JOIN tmp_analytics_scope x ON x.project_id = s.project_id;

-- Remove rows from an earlier SQL run, while leaving all non-legacy facts in
-- place.  The new generation is built from the complete observation set below.
DELETE p
FROM analytics_observation_payloads p
JOIN analytics_observations o ON o.id = p.observation_id
JOIN tmp_analytics_scope x ON x.project_id = o.project_id
WHERE o.id LIKE 'legacy:wechat_import:%'
   OR o.id LIKE 'legacy:seednote_import:%';

DELETE o
FROM analytics_observations o
JOIN tmp_analytics_scope x ON x.project_id = o.project_id
WHERE o.id LIKE 'legacy:wechat_import:%'
   OR o.id LIKE 'legacy:seednote_import:%';

DROP TEMPORARY TABLE IF EXISTS tmp_analytics_wechat;
CREATE TEMPORARY TABLE tmp_analytics_wechat ENGINE=InnoDB AS
SELECT
  s.id AS legacy_id,
  s.project_id,
  COALESCE(NULLIF(ir.task_id, ''), NULLIF(pub_task.task_id, ''), '') AS task_id,
  s.publication_id,
  s.batch_id,
  CASE WHEN COALESCE(NULLIF(ir.task_id, ''), NULLIF(pub_task.task_id, '')) IS NOT NULL
       THEN CONCAT('task:', COALESCE(NULLIF(ir.task_id, ''), NULLIF(pub_task.task_id, '')))
       ELSE CONCAT('wechat_publication:', s.publication_id)
  END AS content_id,
  DATE_FORMAT(DATE_ADD(s.data_as_of_at, INTERVAL 8 HOUR), '%Y-%m-%d') AS stat_date,
  s.data_as_of_at AS effective_at,
  s.imported_at AS received_at,
  b.revoked_at,
  s.read_users,
  s.share_users,
  s.read_to_follow_users,
  s.delivered_users,
  s.delivery_completion_rate,
  s.read_completion_rate,
  s.raw_data
FROM wechat_analytics_snapshots s
LEFT JOIN wechat_analytics_import_batches b ON b.id = s.batch_id
LEFT JOIN wechat_analytics_import_rows ir ON ir.id = s.import_row_id
LEFT JOIN wechat_publications pub_task ON pub_task.id = s.publication_id AND pub_task.project_id = s.project_id
JOIN tmp_analytics_scope x ON x.project_id = s.project_id;

DROP TEMPORARY TABLE IF EXISTS tmp_analytics_seednote;
CREATE TEMPORARY TABLE tmp_analytics_seednote ENGINE=InnoDB AS
SELECT
  v.id AS legacy_id,
  p.project_id,
  v.post_id,
  v.batch_id,
  CONCAT('seednote_post:', v.post_id) AS content_id,
  LOWER(TRIM(b.metric_basis)) AS metric_basis,
  DATE_FORMAT(DATE_ADD(v.data_as_of_at, INTERVAL 8 HOUR), '%Y-%m-%d') AS stat_date,
  v.data_as_of_at AS effective_at,
  v.imported_at AS received_at,
  b.revoked_at,
  v.exposure_count,
  v.view_count,
  v.cover_click_rate,
  v.like_count,
  v.comment_count,
  v.collect_count,
  v.follower_gain_count,
  v.share_count,
  v.avg_watch_duration,
  v.barrage_count,
  v.raw_data
FROM seednote_metric_versions v
JOIN seednote_posts p ON p.id = v.post_id
LEFT JOIN seednote_import_batches b ON b.id = v.batch_id
JOIN tmp_analytics_scope x ON x.project_id = p.project_id;

-- Refresh list metadata from operational tables.  Analytics facts themselves
-- remain immutable and contain no copied title/url as their source of truth.
INSERT INTO analytics_contents(
  id, project_id, platform, task_id, publication_id, post_id,
  title, content_type, status, url, date, created_at, updated_at
)
SELECT
  w.content_id, w.project_id, 'article', NULLIF(w.task_id, ''), NULLIF(w.publication_id, ''), NULL,
  COALESCE(NULLIF(MAX(t.title), ''), NULLIF(MAX(t.topic), ''), NULLIF(MAX(p.draft_title), ''), ''),
  COALESCE(NULLIF(MAX(t.type), ''), 'article'),
  COALESCE(NULLIF(MAX(p.status), ''), NULLIF(MAX(t.status), ''), ''),
  COALESCE(NULLIF(MAX(p.article_url), ''), ''),
  COALESCE(MAX(p.published_at), MAX(t.created_at), MAX(w.effective_at)),
  COALESCE(MAX(t.created_at), MAX(p.created_at), MAX(w.effective_at), UTC_TIMESTAMP(3)),
  UTC_TIMESTAMP(3)
FROM tmp_analytics_wechat w
LEFT JOIN tasks t ON t.id = NULLIF(w.task_id, '') AND t.project_id = w.project_id
LEFT JOIN wechat_publications p ON p.id = NULLIF(w.publication_id, '') AND p.project_id = w.project_id
GROUP BY w.content_id, w.project_id, NULLIF(w.task_id, ''), NULLIF(w.publication_id, '')
ON DUPLICATE KEY UPDATE
  project_id = VALUES(project_id), platform = VALUES(platform), task_id = VALUES(task_id),
  publication_id = VALUES(publication_id), title = VALUES(title), content_type = VALUES(content_type),
  status = VALUES(status), url = VALUES(url), date = VALUES(date), updated_at = UTC_TIMESTAMP(3);

INSERT INTO analytics_contents(
  id, project_id, platform, task_id, publication_id, post_id,
  title, content_type, status, url, date, created_at, updated_at
)
SELECT
  n.content_id, n.project_id, 'seednote', NULL, NULL, n.post_id,
  COALESCE(NULLIF(MAX(p.title), ''), ''),
  COALESCE(NULLIF(MAX(p.genre), ''), ''),
  'recorded',
  COALESCE(NULLIF(MAX(p.note_url), ''), ''),
  COALESCE(MAX(p.first_published_at), MAX(n.effective_at)),
  COALESCE(MAX(p.created_at), MAX(n.effective_at), UTC_TIMESTAMP(3)),
  UTC_TIMESTAMP(3)
FROM tmp_analytics_seednote n
JOIN seednote_posts p ON p.id = n.post_id AND p.project_id = n.project_id
WHERE n.metric_basis IN ('daily', 'cumulative')
GROUP BY n.content_id, n.project_id, n.post_id
ON DUPLICATE KEY UPDATE
  project_id = VALUES(project_id), platform = VALUES(platform), post_id = VALUES(post_id),
  title = VALUES(title), content_type = VALUES(content_type), status = VALUES(status),
  url = VALUES(url), date = VALUES(date), updated_at = UTC_TIMESTAMP(3);

-- Insert immutable observations and their raw payloads.  NULL is kept as NULL;
-- a source-provided zero remains zero.
INSERT INTO analytics_observations(
  id, project_id, content_id, batch_id, metric_basis, stat_date, source,
  source_priority, effective_at, received_at, revoked_at,
  delivered_users, read_users, share_users, read_to_follow_users,
  read_completion_rate, delivery_completion_rate
)
SELECT
  CONCAT('legacy:wechat_import:', legacy_id), project_id, content_id, batch_id,
  'cumulative', stat_date, 'wechat_import', 100, effective_at, received_at, revoked_at,
  delivered_users, read_users, share_users, read_to_follow_users,
  CAST(read_completion_rate AS DECIMAL(38,18)),
  CAST(delivery_completion_rate AS DECIMAL(38,18))
FROM tmp_analytics_wechat
ORDER BY legacy_id;

INSERT INTO analytics_observations(
  id, project_id, content_id, batch_id, metric_basis, stat_date, source,
  source_priority, effective_at, received_at, revoked_at,
  exposure_count, view_count, like_count, comment_count, collect_count,
  follower_gain_count, share_count, barrage_count, cover_click_rate, avg_watch_duration
)
SELECT
  CONCAT('legacy:seednote_import:', legacy_id), project_id, content_id, batch_id,
  metric_basis, stat_date, 'seednote_import', 200, effective_at, received_at, revoked_at,
  exposure_count, view_count, like_count, comment_count, collect_count,
  follower_gain_count, share_count, barrage_count,
  CAST(cover_click_rate AS DECIMAL(38,18)),
  CAST(avg_watch_duration AS DECIMAL(38,18))
FROM tmp_analytics_seednote
WHERE metric_basis IN ('daily', 'cumulative')
ORDER BY legacy_id;

INSERT INTO analytics_observation_payloads(observation_id, payload)
SELECT CONCAT('legacy:wechat_import:', legacy_id), CAST(raw_data AS CHAR)
FROM tmp_analytics_wechat
ON DUPLICATE KEY UPDATE payload = VALUES(payload);

INSERT INTO analytics_observation_payloads(observation_id, payload)
SELECT CONCAT('legacy:seednote_import:', legacy_id), CAST(raw_data AS CHAR)
FROM tmp_analytics_seednote
WHERE metric_basis IN ('daily', 'cumulative')
ON DUPLICATE KEY UPDATE payload = VALUES(payload);

-- Winning observation per content/day/basis.  Revoked batches are excluded.
DROP TEMPORARY TABLE IF EXISTS tmp_analytics_winners;
CREATE TEMPORARY TABLE tmp_analytics_winners ENGINE=InnoDB AS
SELECT
  project_id, content_id, metric_basis, stat_date, id AS observation_id,
  delivered_users, read_users, share_users, collection_users, like_users,
  zaikan_users, comment_count, read_to_follow_users, exposure_count, view_count,
  like_count, collect_count, follower_gain_count,
  share_count, barrage_count, read_completion_rate,
  average_read_active_time, cover_click_rate, avg_watch_duration,
  delivery_completion_rate
FROM (
  SELECT o.*,
         ROW_NUMBER() OVER (
           PARTITION BY o.project_id, o.content_id, o.metric_basis, o.stat_date
           ORDER BY o.source_priority DESC, o.effective_at DESC, o.sequence DESC
         ) AS winner_rank
  FROM analytics_observations o
  JOIN tmp_analytics_scope x ON x.project_id = o.project_id
  WHERE o.revoked_at IS NULL AND o.metric_basis IN ('daily', 'cumulative')
) ranked
WHERE winner_rank = 1;

DROP TEMPORARY TABLE IF EXISTS tmp_analytics_content_buckets;
CREATE TEMPORARY TABLE tmp_analytics_content_buckets ENGINE=InnoDB AS
SELECT
  w.project_id, g.generation, w.content_id, w.metric_basis,
  CAST('day' AS CHAR(8)) AS granularity, w.stat_date AS bucket_start, w.stat_date AS last_stat_date,
  w.observation_id, CAST(1 AS SIGNED) AS coverage,
  w.delivered_users, w.read_users, w.share_users, w.collection_users, w.like_users,
  w.zaikan_users, w.comment_count, w.read_to_follow_users, w.exposure_count,
  w.view_count, w.like_count, w.collect_count, w.follower_gain_count,
  w.share_count, w.barrage_count, w.read_completion_rate,
  w.average_read_active_time, w.cover_click_rate, w.avg_watch_duration,
  w.delivery_completion_rate
FROM tmp_analytics_winners w
JOIN tmp_analytics_generation g ON g.project_id = w.project_id;

-- Materialize period rows in separate statements because MySQL cannot reopen a
-- temporary table twice inside one UNION query.
DROP TEMPORARY TABLE IF EXISTS tmp_analytics_period_rows;
CREATE TEMPORARY TABLE tmp_analytics_period_rows ENGINE=InnoDB AS
SELECT w.*, g.generation, CAST('week' AS CHAR(8)) AS granularity,
       DATE_FORMAT(DATE_SUB(STR_TO_DATE(w.stat_date, '%Y-%m-%d'), INTERVAL WEEKDAY(STR_TO_DATE(w.stat_date, '%Y-%m-%d')) DAY), '%Y-%m-%d') AS bucket_start
FROM tmp_analytics_winners w
JOIN tmp_analytics_generation g ON g.project_id = w.project_id;

INSERT INTO tmp_analytics_period_rows
SELECT w.*, g.generation, CAST('month' AS CHAR(8)) AS granularity,
       DATE_FORMAT(STR_TO_DATE(w.stat_date, '%Y-%m-%d'), '%Y-%m-01') AS bucket_start
FROM tmp_analytics_winners w
JOIN tmp_analytics_generation g ON g.project_id = w.project_id;

-- Cumulative period buckets use the latest valid day in the period.
DROP TEMPORARY TABLE IF EXISTS tmp_analytics_cumulative_period;
CREATE TEMPORARY TABLE tmp_analytics_cumulative_period ENGINE=InnoDB AS
SELECT project_id, generation, content_id, metric_basis, granularity, bucket_start,
       stat_date, observation_id,
       delivered_users, read_users, share_users, collection_users, like_users,
       zaikan_users, comment_count, read_to_follow_users, exposure_count, view_count,
       like_count, collect_count, follower_gain_count, share_count, barrage_count,
       read_completion_rate, average_read_active_time, cover_click_rate,
       avg_watch_duration, delivery_completion_rate
FROM (
  SELECT p.*, ROW_NUMBER() OVER (
    PARTITION BY p.project_id, p.content_id, p.metric_basis, p.granularity, p.bucket_start
    ORDER BY p.stat_date DESC, p.observation_id DESC
  ) AS rn
  FROM tmp_analytics_period_rows p
  WHERE p.metric_basis = 'cumulative'
) ranked
WHERE rn = 1;

INSERT INTO tmp_analytics_content_buckets
SELECT project_id, generation, content_id, metric_basis, granularity, bucket_start,
       stat_date, observation_id, 1,
       delivered_users, read_users, share_users, collection_users, like_users,
       zaikan_users, comment_count, read_to_follow_users, exposure_count, view_count,
       like_count, collect_count, follower_gain_count, share_count, barrage_count,
       read_completion_rate, average_read_active_time, cover_click_rate,
       avg_watch_duration, delivery_completion_rate
FROM tmp_analytics_cumulative_period;

-- Daily period buckets sum only additive integer metrics.  Ratios and averages
-- stay NULL unless a source supplied a reliable aggregate (legacy imports do
-- not supply the required numerator/denominator pair).
INSERT INTO tmp_analytics_content_buckets
SELECT project_id, generation, content_id, metric_basis, granularity, bucket_start,
       MAX(stat_date), '', COUNT(*),
       SUM(delivered_users), SUM(read_users), SUM(share_users), SUM(collection_users), SUM(like_users),
       SUM(zaikan_users), SUM(comment_count), SUM(read_to_follow_users), SUM(exposure_count), SUM(view_count),
       SUM(like_count), SUM(collect_count), SUM(follower_gain_count), SUM(share_count), SUM(barrage_count),
       NULL, NULL, NULL, NULL, NULL
FROM tmp_analytics_period_rows p
WHERE metric_basis = 'daily'
GROUP BY project_id, generation, content_id, metric_basis, granularity, bucket_start;

-- Replace only the target generation's content buckets, then derive account
-- buckets from those rows.  This keeps cumulative values from being summed over
-- dates while still summing independent content series.
DELETE b
FROM analytics_buckets b
JOIN tmp_analytics_generation g ON g.project_id = b.project_id AND g.generation = b.generation
WHERE b.content_id <> '';

INSERT INTO analytics_buckets(
  project_id, generation, content_id, metric_basis, granularity, bucket_start,
  last_stat_date, observation_id, coverage,
  delivered_users, read_users, share_users, collection_users, like_users,
  zaikan_users, comment_count, read_to_follow_users, exposure_count, view_count,
  like_count, collect_count, follower_gain_count, share_count, barrage_count,
  read_completion_rate, average_read_active_time, cover_click_rate,
  avg_watch_duration, delivery_completion_rate
)
SELECT project_id, generation, content_id, metric_basis, granularity, bucket_start,
       last_stat_date, observation_id, coverage,
       delivered_users, read_users, share_users, collection_users, like_users,
       zaikan_users, comment_count, read_to_follow_users, exposure_count, view_count,
       like_count, collect_count, follower_gain_count, share_count, barrage_count,
       read_completion_rate, average_read_active_time, cover_click_rate,
       avg_watch_duration, delivery_completion_rate
FROM tmp_analytics_content_buckets
ON DUPLICATE KEY UPDATE
  last_stat_date = VALUES(last_stat_date), observation_id = VALUES(observation_id), coverage = VALUES(coverage),
  delivered_users = VALUES(delivered_users), read_users = VALUES(read_users), share_users = VALUES(share_users),
  collection_users = VALUES(collection_users), like_users = VALUES(like_users), zaikan_users = VALUES(zaikan_users),
  comment_count = VALUES(comment_count), read_to_follow_users = VALUES(read_to_follow_users),
  exposure_count = VALUES(exposure_count), view_count = VALUES(view_count), like_count = VALUES(like_count),
  collect_count = VALUES(collect_count), follower_gain_count = VALUES(follower_gain_count),
  share_count = VALUES(share_count), barrage_count = VALUES(barrage_count),
  read_completion_rate = VALUES(read_completion_rate), average_read_active_time = VALUES(average_read_active_time),
  cover_click_rate = VALUES(cover_click_rate), avg_watch_duration = VALUES(avg_watch_duration),
  delivery_completion_rate = VALUES(delivery_completion_rate);

DELETE b
FROM analytics_buckets b
JOIN tmp_analytics_generation g ON g.project_id = b.project_id AND g.generation = b.generation
WHERE b.content_id = '';

INSERT INTO analytics_buckets(
  project_id, generation, content_id, metric_basis, granularity, bucket_start,
  last_stat_date, observation_id, coverage,
  delivered_users, read_users, share_users, collection_users, like_users,
  zaikan_users, comment_count, read_to_follow_users, exposure_count, view_count,
  like_count, collect_count, follower_gain_count, share_count, barrage_count,
  read_completion_rate, average_read_active_time, cover_click_rate,
  avg_watch_duration, delivery_completion_rate
)
SELECT project_id, generation, '', metric_basis, granularity, bucket_start,
       MAX(last_stat_date), '', COUNT(*),
       SUM(delivered_users), SUM(read_users), SUM(share_users), SUM(collection_users), SUM(like_users),
       SUM(zaikan_users), SUM(comment_count), SUM(read_to_follow_users), SUM(exposure_count), SUM(view_count),
       SUM(like_count), SUM(collect_count), SUM(follower_gain_count), SUM(share_count), SUM(barrage_count),
       NULL, NULL, NULL, NULL, NULL
FROM tmp_analytics_content_buckets
GROUP BY project_id, generation, metric_basis, granularity, bucket_start
ON DUPLICATE KEY UPDATE
  last_stat_date = VALUES(last_stat_date), coverage = VALUES(coverage),
  delivered_users = VALUES(delivered_users), read_users = VALUES(read_users), share_users = VALUES(share_users),
  collection_users = VALUES(collection_users), like_users = VALUES(like_users), zaikan_users = VALUES(zaikan_users),
  comment_count = VALUES(comment_count), read_to_follow_users = VALUES(read_to_follow_users),
  exposure_count = VALUES(exposure_count), view_count = VALUES(view_count), like_count = VALUES(like_count),
  collect_count = VALUES(collect_count), follower_gain_count = VALUES(follower_gain_count),
  share_count = VALUES(share_count), barrage_count = VALUES(barrage_count),
  read_completion_rate = VALUES(read_completion_rate), average_read_active_time = VALUES(average_read_active_time),
  cover_click_rate = VALUES(cover_click_rate), avg_watch_duration = VALUES(avg_watch_duration),
  delivery_completion_rate = VALUES(delivery_completion_rate);

-- Materialize the source counts once.  Reusing a temporary table twice in one
-- SELECT is rejected by MySQL with "Can't reopen table".
DROP TEMPORARY TABLE IF EXISTS tmp_analytics_report;
CREATE TEMPORARY TABLE tmp_analytics_report ENGINE=InnoDB AS
SELECT x.project_id,
       COALESCE(w.wechat_imports, 0) AS wechat_imports,
       COALESCE(n.seednote_imports, 0) AS seednote_imports,
       COALESCE(n.seednote_basis_unknown, 0) AS seednote_basis_unknown
FROM tmp_analytics_scope x
LEFT JOIN (
  SELECT project_id, COUNT(*) AS wechat_imports
  FROM tmp_analytics_wechat
  GROUP BY project_id
) w ON w.project_id = x.project_id
LEFT JOIN (
  SELECT project_id,
         SUM(metric_basis IN ('daily', 'cumulative')) AS seednote_imports,
         SUM(metric_basis IS NULL OR metric_basis NOT IN ('daily', 'cumulative')) AS seednote_basis_unknown
  FROM tmp_analytics_seednote
  GROUP BY project_id
) n ON n.project_id = x.project_id;

-- Publish the fully rebuilt generation atomically from the reader's point of
-- view.  Existing active generations remain available until this statement.
UPDATE analytics_project_states s
JOIN tmp_analytics_generation g ON g.project_id = s.project_id
SET s.active_generation = g.generation,
    s.status = 'ready',
    s.revision = s.revision + 1,
    s.updated_at = UTC_TIMESTAMP(3);

-- Persist a SQL migration receipt/report for operational audit.
INSERT INTO analytics_rebuild_jobs(
  id, project_id, generation, status, cursor_source, processed, report_json,
  created_at, updated_at
)
SELECT
  UUID(), g.project_id, g.generation, 'published', 'sql_backfill',
  COUNT(o.id),
  CONCAT(
    '{"wechat_imports":', MAX(r.wechat_imports),
    ',"seednote_imports":', MAX(r.seednote_imports),
    ',"seednote_basis_unknown":', MAX(r.seednote_basis_unknown),
    ',"inserted":', COUNT(o.id),
    ',"revoked":', (SELECT COUNT(*) FROM analytics_observations ro WHERE ro.project_id = g.project_id AND ro.revoked_at IS NOT NULL AND (ro.id LIKE 'legacy:wechat_import:%' OR ro.id LIKE 'legacy:seednote_import:%')),
    '}'
  ),
  UTC_TIMESTAMP(3), UTC_TIMESTAMP(3)
FROM tmp_analytics_generation g
JOIN tmp_analytics_report r ON r.project_id = g.project_id
LEFT JOIN analytics_observations o
  ON o.project_id = g.project_id
 AND (o.id LIKE 'legacy:wechat_import:%' OR o.id LIKE 'legacy:seednote_import:%')
GROUP BY g.project_id, g.generation;

COMMIT;

-- Verification output.  The first result set reports source/target counts; the
-- second reports unknown Seednote basis rows that were intentionally excluded.
SELECT
  x.project_id,
  s.active_generation,
  s.status,
  (SELECT COUNT(*) FROM analytics_observations o WHERE o.project_id = x.project_id) AS observations,
  (SELECT COUNT(*) FROM analytics_buckets b WHERE b.project_id = x.project_id AND b.generation = s.active_generation) AS buckets,
  (SELECT COUNT(*) FROM analytics_contents c WHERE c.project_id = x.project_id) AS contents
FROM tmp_analytics_scope x
JOIN analytics_project_states s ON s.project_id = x.project_id
ORDER BY x.project_id;

SELECT project_id, COUNT(*) AS seednote_basis_unknown
FROM tmp_analytics_seednote
WHERE metric_basis IS NULL OR metric_basis NOT IN ('daily', 'cumulative')
GROUP BY project_id;

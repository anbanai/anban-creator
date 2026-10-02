-- Durable evidence required by the WeChat picture-message draft contract.
-- Apply once after the existing WeChat publication lifecycle migrations and
-- before serving wechat-picture tasks.

ALTER TABLE `wechat_publications`
  ADD COLUMN  `draft_article_type` varchar(16) NOT NULL DEFAULT 'news' AFTER `project_id`,
  ADD COLUMN  `draft_image_media_ids` json NULL AFTER `draft_request_fingerprint`,
  ADD COLUMN  `draft_cover_crop_percent_list` json NULL AFTER `draft_image_media_ids`;

ALTER TABLE `task_files`
  ADD COLUMN  `publication_upload_status` varchar(24) NOT NULL DEFAULT '',
  ADD COLUMN  `publication_upload_error` text NOT NULL,
  ADD COLUMN  `publication_upload_attempted_at` datetime(6) NULL;

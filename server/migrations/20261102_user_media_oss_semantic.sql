-- OSS AI Content Awareness metadata for media-library video assets.
-- Apply after the existing user_media_assets schema migration.
ALTER TABLE `user_media_assets`
  ADD COLUMN IF NOT EXISTS `summary` varchar(255) NULL,
  ADD COLUMN IF NOT EXISTS `analysis_provider` varchar(80) NULL,
  ADD COLUMN IF NOT EXISTS `indexed_at` datetime(3) NULL;
ALTER TABLE `user_media_assets`
  ADD INDEX `idx_user_media_assets_indexed_at` (`indexed_at`);

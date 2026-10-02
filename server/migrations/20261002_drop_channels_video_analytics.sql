-- Retires the WeChat Channels public-video analytics surface. Both tables were
-- created only by GORM AutoMigrate for the removed channels-video tracking
-- feature, which depended on a third-party provider that is no longer part of
-- the product. No rows are migrated because the captured metrics have no
-- equivalent Server-owned analytics contract.

DROP TABLE IF EXISTS `channels_metric_snapshots`;
DROP TABLE IF EXISTS `channels_video_trackings`;

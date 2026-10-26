-- Add official Open Platform user growth facts without changing the existing
-- imported article analytics. Apply after 20261009_wechat_account_analytics.sql.
ALTER TABLE `wechat_account_analytics_days`
 ADD COLUMN `follower_cancel_count` BIGINT NULL AFTER `follower_gain_count`,
 ADD COLUMN `follower_net_gain_count` BIGINT NULL AFTER `follower_cancel_count`;

ALTER TABLE `wechat_traffic_source_days`
 ADD COLUMN `new_followers` BIGINT NULL AFTER `read_users`,
 ADD COLUMN `cancelled_followers` BIGINT NULL AFTER `new_followers`;

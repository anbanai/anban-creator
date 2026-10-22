-- Article images published for user-assisted copy workflows use a distinct,
-- durable Anban URL. This is never populated for WeChat-owned material.
ALTER TABLE `task_files`
  ADD COLUMN `platform_image_url` varchar(700) NULL AFTER `wechat_url`;

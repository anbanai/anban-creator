-- Profile Lab persists an explicit capability selection and project-scoped
-- account sources. NULL keeps existing projects on the legacy derived view.
-- Apply after AutoMigrate; every operation is additive and repeatable.
ALTER TABLE `projects` ADD COLUMN IF NOT EXISTS `agent_ids` JSON NULL;
ALTER TABLE `projects` ADD COLUMN IF NOT EXISTS `profile_setup` JSON NULL;
ALTER TABLE `project_profile_revisions` ADD COLUMN IF NOT EXISTS `profile_snapshot` JSON NULL;

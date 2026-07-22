-- Persist the source project for immutable task input reuse by clones.
ALTER TABLE `tasks` ADD COLUMN `input_source_project_id` char(36) NOT NULL DEFAULT '', ADD KEY `idx_tasks_input_source_project_id` (`input_source_project_id`);

-- Durable barriers prevent task/project resurrection while external cleanup is
-- in progress. Existing rows remain mutable because NULL means not deleting.

ALTER TABLE `tasks`
  ADD COLUMN `deleting_at` datetime(3) NULL,
  ADD INDEX `idx_tasks_deleting_at` (`deleting_at`);

ALTER TABLE `projects`
  ADD COLUMN `deleting_at` datetime(3) NULL,
  ADD INDEX `idx_projects_deleting_at` (`deleting_at`);

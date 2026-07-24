-- Forward-only cutover from Kubernetes-specific execution identity columns to
-- provider-neutral runtime dispatch identity. Run after stopping old servers.

ALTER TABLE `task_executions`
  DROP INDEX `idx_task_executions_job_name`,
  CHANGE COLUMN `namespace` `runtime_scope` varchar(63),
  CHANGE COLUMN `job_name` `runtime_workload` varchar(63),
  CHANGE COLUMN `pod_uid` `runtime_instance_id` varchar(64),
  ADD INDEX `idx_task_executions_runtime_workload` (`runtime_workload`);

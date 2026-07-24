-- Seal authoritative workspace manifests against later MCP artifact upserts.
ALTER TABLE `task_executions`
  ADD COLUMN `manifest_sealed` boolean NOT NULL DEFAULT false;

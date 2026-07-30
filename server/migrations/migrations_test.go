package migrations

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPendingStartupMigrationSQLContract(t *testing.T) {
	raw, err := os.ReadFile("20260716_pending_startup_migrations.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)

	required := []string{
		"ALTER TABLE `task_files`",
		"DROP CHECK `chk_task_file_state`",
		"ADD CONSTRAINT `chk_task_file_state` CHECK (`state` IN ('pending', 'published', 'collected', 'superseded'))",
		"ALTER TABLE `task_executions`",
		"DROP CHECK `chk_task_execution_manifest_status`",
		"ADD CONSTRAINT `chk_task_execution_manifest_status` CHECK (`manifest_status` IN ('', 'pending', 'published', 'collected', 'discarded', 'rejected'))",
		"DELETE older",
		"older.`created_at` < newer.`created_at`",
		"older.`created_at` = newer.`created_at` AND older.`id` < newer.`id`",
		"CREATE UNIQUE INDEX `idx_agent_feedback_task_agent` ON `agent_feedbacks` (`task_id`, `agent_name`)",
	}
	for _, fragment := range required {
		if !strings.Contains(sql, fragment) {
			t.Errorf("migration SQL missing %q", fragment)
		}
	}
	if strings.Contains(sql, "DROP CONSTRAINT") {
		t.Fatal("migration SQL uses unsupported MySQL DROP CONSTRAINT syntax")
	}
}

func TestRemoveUserTextModelConfigMigration(t *testing.T) {
	raw, err := os.ReadFile("20260729_remove_user_text_model_config.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToLower(string(raw))
	if !strings.Contains(sql, "drop column text_config_json") {
		t.Fatal("migration must drop user_model_configs.text_config_json")
	}
}

func TestWorkspaceManifestSealMigration(t *testing.T) {
	raw, err := os.ReadFile("20260723_workspace_manifest_seal.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	for _, fragment := range []string{
		"ALTER TABLE `task_executions`",
		"ADD COLUMN `manifest_sealed` boolean NOT NULL DEFAULT false",
	} {
		if !strings.Contains(strings.ToLower(sql), strings.ToLower(fragment)) {
			t.Errorf("workspace manifest seal migration missing %q", fragment)
		}
	}
	for _, forbidden := range []string{"DROP COLUMN", "DROP TABLE", "UPDATE `task_executions`"} {
		if strings.Contains(strings.ToUpper(sql), strings.ToUpper(forbidden)) {
			t.Errorf("workspace manifest seal migration contains destructive or backfill statement %q", forbidden)
		}
	}
}

func TestDeletionAuthorityMigration(t *testing.T) {
	raw, err := os.ReadFile("20260724_deletion_authority.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToLower(string(raw))
	for _, fragment := range []string{
		"alter table `tasks`",
		"add column `deleting_at` datetime(3) null",
		"add index `idx_tasks_deleting_at` (`deleting_at`)",
		"alter table `projects`",
		"add index `idx_projects_deleting_at` (`deleting_at`)",
	} {
		if !strings.Contains(sql, fragment) {
			t.Errorf("deletion authority migration missing %q", fragment)
		}
	}
}

func TestFinalizedReferenceAssetsMigration(t *testing.T) {
	raw, err := os.ReadFile("20260717_finalized_reference_assets.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	required := []string{
		"CREATE TABLE `upload_sessions`",
		"UNIQUE KEY `idx_upload_sessions_staging_key` (`staging_key`)",
		"`promotion_source_etag` varchar(255) NOT NULL DEFAULT ''",
		"`finalization_etag` varchar(255) NOT NULL DEFAULT ''",
		"KEY `idx_upload_sessions_user_id` (`user_id`)",
		"KEY `idx_upload_sessions_purpose` (`purpose`)",
		"KEY `idx_upload_sessions_status` (`status`)",
		"KEY `idx_upload_sessions_expires_at` (`expires_at`)",
		"KEY `idx_upload_sessions_finalization_token` (`finalization_token`)",
		"KEY `idx_upload_sessions_finalization_claimed_at` (`finalization_claimed_at`)",
		"KEY `idx_upload_sessions_asset_id` (`asset_id`)",
		"KEY `idx_upload_sessions_cleanup_claim_id` (`cleanup_claim_id`)",
		"KEY `idx_upload_sessions_cleanup_claimed_at` (`cleanup_claimed_at`)",
		"KEY `idx_upload_sessions_next_cleanup_at` (`next_cleanup_at`)",
		"CREATE TABLE `assets`",
		"UNIQUE KEY `idx_assets_storage_key` (`storage_key`)",
		"KEY `idx_assets_user_id` (`user_id`)",
		"KEY `idx_assets_purpose` (`purpose`)",
		"ALTER TABLE `projects` ADD COLUMN `reference_image_asset_id` char(36) DEFAULT NULL, ADD KEY `idx_projects_reference_image_asset_id` (`reference_image_asset_id`), DROP COLUMN `reference_image_url`;",
		"ALTER TABLE `plans` ADD COLUMN `reference_image_asset_id` char(36) DEFAULT NULL, ADD KEY `idx_plans_reference_image_asset_id` (`reference_image_asset_id`), DROP COLUMN `reference_image_url`;",
		"ALTER TABLE `tasks` ADD COLUMN `reference_image_asset_id` char(36) DEFAULT NULL, ADD KEY `idx_tasks_reference_image_asset_id` (`reference_image_asset_id`), DROP COLUMN `reference_image_url`;",
		"DROP TABLE `pending_uploads`",
	}
	for _, fragment := range required {
		if !strings.Contains(sql, fragment) {
			t.Errorf("migration SQL missing %q", fragment)
		}
	}
	if got := strings.Count(sql, "DROP COLUMN `reference_image_url`"); got != 3 {
		t.Errorf("reference URL columns dropped %d times, want 3", got)
	}
	for _, forbidden := range []string{"pending/", "reference_image_url LIKE", "SUBSTRING", "REPLACE("} {
		if strings.Contains(strings.ToUpper(sql), strings.ToUpper(forbidden)) {
			t.Errorf("migration SQL contains forbidden URL migration fragment %q", forbidden)
		}
	}
	if regexp.MustCompile(`(?is)\bINSERT\s+INTO\b.*\bSELECT\b`).MatchString(sql) {
		t.Fatal("migration SQL must not backfill with INSERT ... SELECT")
	}

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open schema parser: %v", err)
	}
	for _, value := range []any{&model.UploadSession{}, &model.Asset{}} {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(value); err != nil {
			t.Fatalf("parse %T schema: %v", value, err)
		}
		for _, field := range stmt.Schema.Fields {
			if field.DBName != "" && !strings.Contains(sql, "`"+field.DBName+"`") {
				t.Errorf("migration SQL missing %s.%s", stmt.Schema.Table, field.DBName)
			}
		}
		for _, index := range stmt.Schema.ParseIndexes() {
			if !strings.Contains(sql, "`"+index.Name+"`") {
				t.Errorf("migration SQL missing %s index %s", stmt.Schema.Table, index.Name)
			}
		}
	}
}

func TestRuntimeDispatchIdentityMigration(t *testing.T) {
	raw, err := os.ReadFile("20260722_runtime_dispatch_identity.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	required := []string{
		"CHANGE COLUMN `namespace` `runtime_scope` varchar(63)",
		"CHANGE COLUMN `job_name` `runtime_workload` varchar(63)",
		"CHANGE COLUMN `pod_uid` `runtime_instance_id` varchar(64)",
		"DROP INDEX `idx_task_executions_job_name`",
		"ADD INDEX `idx_task_executions_runtime_workload` (`runtime_workload`)",
	}
	for _, fragment := range required {
		if !strings.Contains(sql, fragment) {
			t.Errorf("migration SQL missing %q", fragment)
		}
	}
	for _, forbidden := range []string{"IF EXISTS", "IF NOT EXISTS", "ADD COLUMN", "UPDATE `task_executions`", "NOT NULL", "DEFAULT"} {
		if strings.Contains(strings.ToUpper(sql), strings.ToUpper(forbidden)) {
			t.Errorf("migration SQL contains compatibility fragment %q", forbidden)
		}
	}
}

func TestCloneInputSourceProjectMigration(t *testing.T) {
	raw, err := os.ReadFile("20260722_clone_input_source_project.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	for _, fragment := range []string{
		"ALTER TABLE `tasks`",
		"ADD COLUMN `input_source_project_id` char(36) NOT NULL DEFAULT ''",
		"ADD KEY `idx_tasks_input_source_project_id` (`input_source_project_id`)",
	} {
		if !strings.Contains(sql, fragment) {
			t.Errorf("migration SQL missing %q", fragment)
		}
	}

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open schema parser: %v", err)
	}
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(&model.Task{}); err != nil {
		t.Fatalf("parse task schema: %v", err)
	}
	field := stmt.Schema.LookUpField("InputSourceProjectID")
	if field == nil || field.DBName != "input_source_project_id" {
		t.Fatalf("clone source project field = %#v, want input_source_project_id", field)
	}
	for _, index := range stmt.Schema.ParseIndexes() {
		if index.Name == "idx_tasks_input_source_project_id" {
			return
		}
	}
	t.Fatal("task schema missing idx_tasks_input_source_project_id")
}

func TestAgentExecutionProfilesMigration(t *testing.T) {
	raw, err := os.ReadFile("20260728_agent_execution_profiles.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	const fingerprint = "c7f2d8997f92b789fe732f3398f16183cdb085eb09e7a1eb8b3480fefcc8fa9d"
	for _, fragment := range []string{
		"ALTER TABLE `tasks`",
		"ADD COLUMN `execution_profile` varchar(40) NULL",
		"ADD COLUMN `agent_profile_snapshot` json NULL",
		"ADD COLUMN `agent_profile_fingerprint` char(64) NULL",
		"UPDATE `tasks`",
		"SET `execution_profile` = 'balanced'",
		"'schema_version', 2",
		"'models', JSON_OBJECT(",
		"'default', 'doubao-seed-evolving'",
		"'claude', JSON_OBJECT()",
		fingerprint,
		"MODIFY COLUMN `execution_profile` varchar(40) NOT NULL",
		"MODIFY COLUMN `agent_profile_snapshot` json NOT NULL",
		"MODIFY COLUMN `agent_profile_fingerprint` char(64) NOT NULL",
		"ALTER TABLE `plans`",
		"UPDATE `plans` SET `execution_profile` = 'balanced'",
		"ALTER TABLE `task_executions`",
		"ADD COLUMN `execution_profile` varchar(40) NULL",
		"ADD COLUMN `provider` varchar(80) NULL",
		"ADD COLUMN `model_matrix` json NULL",
		"ADD COLUMN `claude_controls` json NULL",
		"ADD COLUMN `profile_fingerprint` char(64) NULL",
		"UPDATE `task_executions` AS `execution`",
		"INNER JOIN `tasks` AS `task` ON `task`.`id` = `execution`.`task_id`",
		"MODIFY COLUMN `execution_profile` varchar(40) NOT NULL",
		"MODIFY COLUMN `provider` varchar(80) NOT NULL",
		"MODIFY COLUMN `model_matrix` json NOT NULL",
		"MODIFY COLUMN `claude_controls` json NOT NULL",
		"MODIFY COLUMN `profile_fingerprint` char(64) NOT NULL",
		"DROP COLUMN `model_id`",
		"DROP COLUMN `protocol`",
		"DROP COLUMN `reasoning_effort`",
		"DROP COLUMN `context_window`",
		"ALTER TABLE `billing_skus`",
		"ADD COLUMN `execution_profile` varchar(40) NOT NULL DEFAULT ''",
		"ADD INDEX `idx_billing_skus_execution_profile` (`execution_profile`)",
		"ADD INDEX `idx_billing_skus_catalog_operation_profile` (`catalog_id`, `operation`, `execution_profile`)",
		"ALTER TABLE `billing_quotes`",
		"ADD COLUMN `agent_profile_snapshot` json NULL",
	} {
		if !strings.Contains(sql, fragment) {
			t.Errorf("agent execution profiles migration missing %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"DEFAULT 'cost_effective'",
		"DEFAULT 'balanced'",
		"ADD COLUMN `provider` varchar(80) NOT NULL",
		"SHA2(CAST(agent_profile_snapshot AS CHAR)",
	} {
		if strings.Contains(sql, forbidden) {
			t.Errorf("agent execution profiles migration contains premature/default compatibility fragment %q", forbidden)
		}
	}
	if strings.Index(sql, "UPDATE `tasks`") > strings.Index(sql, "MODIFY COLUMN `execution_profile` varchar(40) NOT NULL") {
		t.Fatal("tasks are constrained before balanced backfill")
	}
	if strings.Index(sql, "UPDATE `task_executions` AS `execution`") > strings.Index(sql, "MODIFY COLUMN `model_matrix` json NOT NULL") {
		t.Fatal("task executions are constrained before snapshot backfill")
	}
	if strings.Index(sql, "MODIFY COLUMN `profile_fingerprint` char(64) NOT NULL") > strings.Index(sql, "DROP COLUMN `model_id`") {
		t.Fatal("legacy execution columns are dropped before final constraints")
	}
}

func TestAgentProfileEnvsMigrationContracts(t *testing.T) {
	expandRaw, err := os.ReadFile("20260730_agent_profile_envs_expand.sql")
	if err != nil {
		t.Fatal(err)
	}
	expand := string(expandRaw)
	for _, required := range []string{"ALTER TABLE `task_executions`", "ADD COLUMN `profile_envs` json NULL"} {
		if !strings.Contains(expand, required) {
			t.Fatalf("expand migration missing %q", required)
		}
	}
	for _, forbidden := range []string{"DROP COLUMN", "NOT NULL", "UPDATE `", "DELETE FROM"} {
		if strings.Contains(strings.ToUpper(expand), strings.ToUpper(forbidden)) {
			t.Fatalf("expand migration contains %q", forbidden)
		}
	}

	contractRaw, err := os.ReadFile("20260730_agent_profile_envs_contract.sql")
	if err != nil {
		t.Fatal(err)
	}
	contract := string(contractRaw)
	for _, required := range []string{
		"SIGNAL SQLSTATE '45000'", "COALESCE(JSON_UNQUOTE(JSON_EXTRACT(`agent_profile_snapshot`, '$.schema_version')), '') <> '3'", "$.envs", "$.ANTHROPIC_AUTH_TOKEN",
		"MODIFY COLUMN `profile_envs` json NOT NULL", "DROP COLUMN `model_matrix`", "DROP COLUMN `claude_controls`",
		"DROP PROCEDURE IF EXISTS `assert_agent_profile_envs_contract_ready`",
		"CONSTRAINT `chk_tasks_execution_profile_v3` CHECK (`execution_profile` IN ('effective', 'balanced', 'quality'))",
		"CONSTRAINT `chk_plans_execution_profile_v3` CHECK (`execution_profile` IN ('effective', 'balanced', 'quality'))",
		"CONSTRAINT `chk_task_executions_execution_profile_v3` CHECK (`execution_profile` IN ('effective', 'balanced', 'quality'))",
		"information_schema.COLUMNS", "information_schema.TABLE_CONSTRAINTS",
		"IF EXISTS (", "IF NOT EXISTS (",
		"DROP PROCEDURE IF EXISTS `apply_agent_profile_envs_contract_schema`",
	} {
		if !strings.Contains(contract, required) {
			t.Fatalf("contract migration missing %q", required)
		}
	}
	for _, guardedChange := range []string{
		"MODIFY COLUMN `profile_envs` json NOT NULL",
		"DROP COLUMN `model_matrix`",
		"DROP COLUMN `claude_controls`",
		"ADD CONSTRAINT `chk_task_executions_execution_profile_v3`",
		"ADD CONSTRAINT `chk_tasks_execution_profile_v3`",
		"ADD CONSTRAINT `chk_plans_execution_profile_v3`",
	} {
		if strings.Count(contract, guardedChange) != 1 {
			t.Fatalf("contract migration change %q must appear exactly once", guardedChange)
		}
	}
	if strings.Index(contract, "SIGNAL SQLSTATE '45000'") > strings.Index(contract, "MODIFY COLUMN `profile_envs` json NOT NULL") {
		t.Fatal("contract migration changes schema before preflight assertions")
	}

	for name, raw := range map[string]string{"expand": expand, "contract": contract} {
		for _, forbidden := range []string{"UPDATE `billing_skus`", "UPDATE `billing_quotes`", "UPDATE `billing_charges`", "UPDATE `billing_wallet_entries`"} {
			if strings.Contains(raw, forbidden) {
				t.Fatalf("%s migration mutates billing history with %s", name, forbidden)
			}
		}
	}
}

func TestAgentProfileEnvsQuoteExpiryIsNarrow(t *testing.T) {
	raw, err := os.ReadFile("20260730_agent_profile_envs_expire_quotes.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	for _, required := range []string{
		"UPDATE `billing_quotes`", "SET `expires_at` = CURRENT_TIMESTAMP(3)",
		"WHERE `consumed_at` IS NULL", "AND `expires_at` > CURRENT_TIMESTAMP(3)",
		"JSON_UNQUOTE(JSON_EXTRACT(`agent_profile_snapshot`, '$.schema_version')) = '2'",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("quote expiry migration missing %q", required)
		}
	}
	for _, forbidden := range []string{"billing_skus", "billing_charges", "billing_wallet_entries", "DELETE", "consumed_at` IS NOT NULL"} {
		if strings.Contains(strings.ToUpper(sql), strings.ToUpper(forbidden)) {
			t.Fatalf("quote expiry migration contains %q", forbidden)
		}
	}
}

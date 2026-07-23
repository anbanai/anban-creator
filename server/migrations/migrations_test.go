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

func TestCloneInputSourceProjectMigration(t *testing.T) {
	raw, err := os.ReadFile("20260722_clone_input_source_project.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	required := []string{
		"ALTER TABLE `tasks`",
		"ADD COLUMN `input_source_project_id` char(36) NOT NULL DEFAULT ''",
		"ADD KEY `idx_tasks_input_source_project_id` (`input_source_project_id`)",
	}
	for _, fragment := range required {
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

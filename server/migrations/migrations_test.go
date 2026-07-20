package migrations

import (
	"os"
	"strings"
	"testing"
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

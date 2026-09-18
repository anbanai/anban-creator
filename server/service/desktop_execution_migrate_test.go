package service

import (
	"context"
	"testing"
)

func TestMigrateDesktopExecutionRemovalConvertsLegacyRows(t *testing.T) {
	db := newMigrateTestDB(t)
	for _, statement := range []string{
		`CREATE TABLE tasks (id TEXT PRIMARY KEY, status TEXT NOT NULL, execution_target TEXT, local_claim_deadline DATETIME, executor_info TEXT)`,
		`CREATE TABLE task_executions (id TEXT PRIMARY KEY, task_id TEXT NOT NULL, target TEXT NOT NULL, status TEXT NOT NULL, terminal_reason TEXT NOT NULL DEFAULT '', result TEXT, completed_at DATETIME, finalization_status TEXT NOT NULL DEFAULT '', cleanup_status TEXT NOT NULL DEFAULT '')`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec(`INSERT INTO tasks (id, status, execution_target, executor_info) VALUES ('pending', 'pending', 'local', '{}'), ('running', 'running', 'local_claimed', '{}')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO task_executions (id, task_id, target, status) VALUES ('execution-1', 'running', 'local_claimed', 'running')`).Error; err != nil {
		t.Fatal(err)
	}

	if err := MigrateDesktopExecutionRemoval(context.Background(), db, nil); err != nil {
		t.Fatal(err)
	}

	var pendingStatus string
	if err := db.Raw(`SELECT status FROM tasks WHERE id = 'pending'`).Scan(&pendingStatus).Error; err != nil {
		t.Fatal(err)
	}
	if pendingStatus != "pending" {
		t.Fatalf("pending local task status = %q, want pending", pendingStatus)
	}
	var execution struct {
		Status             string
		TerminalReason     string
		FinalizationStatus string
		CleanupStatus      string
		Result             string
	}
	if err := db.Raw(`SELECT status, terminal_reason, finalization_status, cleanup_status, result FROM task_executions WHERE id = 'execution-1'`).Scan(&execution).Error; err != nil {
		t.Fatal(err)
	}
	if execution.Status != "failed" || execution.TerminalReason != "infrastructure_cancelled" || execution.FinalizationStatus != "" || execution.CleanupStatus != "done" || execution.Result == "" {
		t.Fatalf("legacy execution was not terminalized: %+v", execution)
	}
	for _, column := range []string{"execution_target", "local_claim_deadline", "executor_info"} {
		if db.Migrator().HasColumn("tasks", column) {
			t.Fatalf("tasks.%s still exists", column)
		}
	}

	if err := MigrateDesktopExecutionRemoval(context.Background(), db, nil); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateDesktopExecutionRemovalAllowsFreshSchema(t *testing.T) {
	db := newMigrateTestDB(t)
	if err := MigrateDesktopExecutionRemoval(context.Background(), db, nil); err != nil {
		t.Fatal(err)
	}
}

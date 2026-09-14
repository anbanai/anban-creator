package model

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openTaskExecutionTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	return db
}

func TestTaskExecutionMigrationAndCurrentAttempt(t *testing.T) {
	db := openTaskExecutionTestDB(t)
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	if !db.Migrator().HasTable(&TaskExecution{}) {
		t.Fatal("task_executions missing")
	}
	if !db.Migrator().HasColumn(&Task{}, "CurrentExecutionID") {
		t.Fatal("current_execution_id missing")
	}
	if !db.Migrator().HasColumn(&Task{}, "ProgressSequence") {
		t.Fatal("progress_sequence missing")
	}
	if !db.Migrator().HasColumn(&Project{}, "AgentConfig") || !db.Migrator().HasColumn(&Plan{}, "AgentInput") || !db.Migrator().HasColumn(&Task{}, "AgentInput") {
		t.Fatal("Agent Pack extension JSON columns missing")
	}
	if !db.Migrator().HasColumn(&TaskExecution{}, "DispatchClaimToken") ||
		!db.Migrator().HasColumn(&TaskExecution{}, "DispatchClaimedAt") {
		t.Fatal("dispatch lease columns missing")
	}
	for _, column := range []string{"ParentExecutionID", "ResumeSessionID", "AgentPackID", "AgentPackVersion", "AgentPackDigest", "AgentPackProgressContract", "AgentPackDeliveryContract", "AgentPackRequiredArtifactContract", "RuntimeAdapter", "RuntimeProfile", "RuntimeImage", "ManifestSealed", "FinalizationStatus", "FinalizationToken", "CleanupStatus", "CleanupToken", "CleanupNextAt", "DraftDeliveryStatus", "DraftDeliveryResult", "Result"} {
		if !db.Migrator().HasColumn(&TaskExecution{}, column) {
			t.Fatalf("task execution durability column %s missing", column)
		}
	}
	for _, column := range []string{"RuntimeScope", "RuntimeWorkload", "RuntimeInstanceID"} {
		if !db.Migrator().HasColumn(&TaskExecution{}, column) {
			t.Fatalf("task execution runtime identity column %s missing", column)
		}
	}
	for _, column := range []string{"Namespace", "JobName", "PodUID"} {
		if db.Migrator().HasColumn(&TaskExecution{}, column) {
			t.Fatalf("legacy task execution runtime identity column %s remains", column)
		}
	}
	if !db.Migrator().HasIndex(&TaskExecution{}, "idx_task_executions_runtime_workload") {
		t.Fatal("runtime workload index missing")
	}
	if db.Migrator().HasIndex(&TaskExecution{}, "idx_task_executions_job_name") {
		t.Fatal("legacy job name index remains")
	}
	rows, err := db.Raw("PRAGMA table_info(task_executions)").Rows()
	if err != nil {
		t.Fatalf("read task execution columns: %v", err)
	}
	defer rows.Close()
	wantTypes := map[string]string{"runtime_scope": "varchar(63)", "runtime_workload": "varchar(63)", "runtime_instance_id": "varchar(64)"}
	seen := map[string]bool{}
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		wantType, ok := wantTypes[name]
		if !ok {
			continue
		}
		seen[name] = true
		if columnType != wantType || notNull != 0 || defaultValue.Valid {
			t.Errorf("column %s type=%q not_null=%d default=%v, want type=%q nullable with no default", name, columnType, notNull, defaultValue, wantType)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for name := range wantTypes {
		if !seen[name] {
			t.Errorf("column metadata missing for %s", name)
		}
	}
}

func TestRuntimeIdentityCarriesProviderNeutralValues(t *testing.T) {
	identity := RuntimeIdentity{Scope: "docker", Workload: "creator-agent-exec-1", InstanceID: "container-1"}
	if identity.Scope != "docker" || identity.Workload != "creator-agent-exec-1" || identity.InstanceID != "container-1" {
		t.Fatalf("runtime identity = %+v", identity)
	}
}

func TestTaskExecutionPublicJSONHidesRawDiagnostics(t *testing.T) {
	execution := TaskExecution{
		ID: "execution-1", Diagnostics: datatypes.JSON(`{"provider_body":"sensitive"}`),
		Result: datatypes.JSON(`{"log_text":"internal"}`),
	}
	raw, err := json.Marshal(execution)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"diagnostics", "provider_body", "sensitive", "log_text", "internal"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("public execution JSON exposed %q: %s", forbidden, raw)
		}
	}
}

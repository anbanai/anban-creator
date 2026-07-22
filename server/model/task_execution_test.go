package model

import (
	"testing"

	"github.com/google/uuid"
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
	if !db.Migrator().HasColumn(&TaskExecution{}, "DispatchClaimToken") ||
		!db.Migrator().HasColumn(&TaskExecution{}, "DispatchClaimedAt") {
		t.Fatal("dispatch lease columns missing")
	}
	for _, column := range []string{"ParentExecutionID", "ResumeSessionID", "RuntimeProfile", "RuntimeImage", "FinalizationStatus", "FinalizationToken", "CleanupStatus", "CleanupToken", "CleanupNextAt", "PublishingStatus", "PublishingResult", "Result"} {
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
}

func TestRuntimeIdentityCarriesProviderNeutralValues(t *testing.T) {
	identity := RuntimeIdentity{Scope: "docker", Workload: "creator-agent-exec-1", InstanceID: "container-1"}
	if identity.Scope != "docker" || identity.Workload != "creator-agent-exec-1" || identity.InstanceID != "container-1" {
		t.Fatalf("runtime identity = %+v", identity)
	}
}

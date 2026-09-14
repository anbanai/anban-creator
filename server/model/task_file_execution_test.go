package model

import (
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestTaskFileExecutionScopedUniqueIndex(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	if err := db.AutoMigrate(&TaskFile{}); err != nil {
		t.Fatalf("auto migrate task files: %v", err)
	}
	if !db.Migrator().HasIndex(&TaskFile{}, "idx_task_file_execution_path") {
		t.Fatal("execution unique index missing")
	}

	rows := []*TaskFile{
		{ID: "f1", TaskID: "t1", ExecutionID: "e1", State: TaskFileStatePending, Role: FileRoleOther, FilePath: "output/a.md", FileName: "a.md"},
		{ID: "f2", TaskID: "t1", ExecutionID: "e2", State: TaskFileStatePending, Role: FileRoleOther, FilePath: "output/a.md", FileName: "a.md"},
	}
	for _, row := range rows {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("create %s: %v", row.ID, err)
		}
	}
	duplicate := *rows[1]
	duplicate.ID = "f3"
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("duplicate task/execution/path unexpectedly succeeded")
	}
}

func TestTaskFileDefaultsToPublishedForLegacyWriters(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	row := &TaskFile{ID: "f1", TaskID: "t1", Role: FileRoleOther, FilePath: "a.md", FileName: "a.md"}
	if err := db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
	if row.State != TaskFileStateDelivered {
		t.Fatalf("state = %q, want published", row.State)
	}
	invalid := &TaskFile{ID: "f2", TaskID: "t1", State: "invalid", Role: FileRoleOther, FilePath: "b.md", FileName: "b.md"}
	if err := db.Create(invalid).Error; err == nil {
		t.Fatal("invalid task file state unexpectedly persisted")
	}
}

func TestTaskArtifactCollectionStates(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	if err := db.AutoMigrate(&TaskFile{}, &TaskExecution{}); err != nil {
		t.Fatalf("auto migrate artifact schema: %v", err)
	}
	if err := db.Create(&TaskExecution{
		ID: "e-collected", TaskID: "t-collected", Attempt: 1, Target: "kubernetes",
		Status: TaskExecutionFailed, ManifestStatus: "retained",
	}).Error; err != nil {
		t.Fatalf("create collected execution: %v", err)
	}
	if err := db.Create(&TaskFile{
		ID: "f-collected", TaskID: "t-collected", ExecutionID: "e-collected",
		State: "retained", Role: FileRoleOther, FilePath: "output/failure-state.json", FileName: "failure-state.json",
	}).Error; err != nil {
		t.Fatalf("create collected task file: %v", err)
	}
}

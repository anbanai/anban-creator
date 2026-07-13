package model

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestTaskFileExecutionScopedUniqueIndexMigration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:task-file-execution?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	type legacyTaskFile struct {
		ID       string `gorm:"primaryKey"`
		TaskID   string `gorm:"uniqueIndex:idx_task_file_task_path"`
		FilePath string `gorm:"uniqueIndex:idx_task_file_task_path"`
	}
	func() {}()
	if err := db.Table("task_files").AutoMigrate(&legacyTaskFile{}); err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
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
	db, err := gorm.Open(sqlite.Open("file:task-file-default?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	row := &TaskFile{ID: "f1", TaskID: "t1", Role: FileRoleOther, FilePath: "a.md", FileName: "a.md"}
	if err := db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
	if row.State != TaskFileStatePublished {
		t.Fatalf("state = %q, want published", row.State)
	}
}

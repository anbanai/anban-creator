package model

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type legacyTaskWorkspaceSchema struct {
	ID          string     `gorm:"type:varchar(36);primaryKey"`
	CleanedUpAt *time.Time `gorm:"index"`
}

func (legacyTaskWorkspaceSchema) TableName() string { return "tasks" }

func TestTaskSchemaDoesNotCreateCleanedUpAt(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := db.AutoMigrate(&Task{}); err != nil {
		t.Fatalf("migrate task: %v", err)
	}
	if db.Migrator().HasColumn(&Task{}, "cleaned_up_at") {
		t.Fatal("tasks.cleaned_up_at exists, want NAS workspaces to have no application cleanup marker")
	}
}

func TestMigrateDurableTaskWorkspaceSchemaDropsLegacyCleanupColumn(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := db.AutoMigrate(&legacyTaskWorkspaceSchema{}); err != nil {
		t.Fatalf("create legacy task schema: %v", err)
	}
	if !db.Migrator().HasColumn("tasks", "cleaned_up_at") {
		t.Fatal("legacy tasks.cleaned_up_at missing before migration")
	}

	if err := AutoMigrate(db); err != nil {
		t.Fatalf("application auto migration: %v", err)
	}
	if db.Migrator().HasColumn("tasks", "cleaned_up_at") {
		t.Fatal("tasks.cleaned_up_at exists after migration")
	}
}

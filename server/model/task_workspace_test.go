package model

import (
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

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

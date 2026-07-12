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
}

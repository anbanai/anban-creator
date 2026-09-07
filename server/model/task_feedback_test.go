package model

import (
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestTaskFeedbackBusinessKeyIsUnique(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&TaskFeedback{}); err != nil {
		t.Fatalf("auto migrate task feedback: %v", err)
	}

	first := &TaskFeedback{ID: uuid.NewString(), TaskID: uuid.NewString(), UserID: uuid.NewString(), Rating: 5}
	if err := db.Create(first).Error; err != nil {
		t.Fatalf("create feedback: %v", err)
	}
	duplicate := &TaskFeedback{ID: uuid.NewString(), TaskID: first.TaskID, UserID: first.UserID, Rating: 1}
	if err := db.Create(duplicate).Error; err == nil {
		t.Fatal("database accepted duplicate task/user feedback")
	}
}

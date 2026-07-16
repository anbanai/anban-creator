package model

import (
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAgentFeedbackBusinessKeyIsUnique(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&AgentFeedback{}); err != nil {
		t.Fatalf("auto migrate agent feedback: %v", err)
	}

	feedback := &AgentFeedback{ID: uuid.NewString(), TaskID: "task-1", AgentName: "seednote"}
	if err := db.Create(feedback).Error; err != nil {
		t.Fatalf("create feedback: %v", err)
	}
	duplicate := &AgentFeedback{ID: uuid.NewString(), TaskID: feedback.TaskID, AgentName: feedback.AgentName}
	if err := db.Create(duplicate).Error; err == nil {
		t.Fatal("database accepted duplicate task/agent feedback")
	}
}

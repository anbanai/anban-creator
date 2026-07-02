package service

import (
	"context"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAgentFeedbackCreateAllowsLocalTaskID(t *testing.T) {
	repo := setupAgentFeedbackRepo(t)
	svc := NewAgentFeedbackService(repo, nil)

	feedback, err := svc.Create(context.Background(), "local-video-"+uuid.NewString(), "video", `{"quality":8}`, "", "", "local run")
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if feedback.ID == "" || !strings.HasPrefix(feedback.TaskID, "local-video-") {
		t.Fatalf("unexpected feedback: %#v", feedback)
	}
}

func setupAgentFeedbackRepo(t *testing.T) repository.Repository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return repository.New(db)
}

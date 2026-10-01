package service

import (
	"errors"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPlanEntriesSupportMultipleAgentsAndRejectDuplicates(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.Project{}, &model.Plan{}, &model.PlanEntry{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	userID := uuid.NewString()
	if err := repo.Users().Create(t.Context(), &model.User{ID: userID, Email: userID + "@example.com"}); err != nil {
		t.Fatal(err)
	}
	projectID := uuid.NewString()
	if err := repo.Projects().Create(t.Context(), &model.Project{ID: projectID, UserID: userID, Name: "shared", Status: model.ProjectStatusActive}); err != nil {
		t.Fatal(err)
	}
	planID := uuid.NewString()
	if err := repo.Plans().Create(t.Context(), &model.Plan{ID: planID, UserID: userID, ProjectID: projectID, ExecutionProfile: "effective", CronExpr: "0 9 * * *", Status: model.PlanStatusActive, NextRunAt: planEntryTime(time.Now().Add(time.Hour))}); err != nil {
		t.Fatal(err)
	}
	svc := NewPlanService(repo, nil)
	a, err := svc.CreateEntry(t.Context(), CreatePlanEntryParams{UserID: userID, PlanID: planID, AgentID: "wechat-article", Channel: "wechat-article", TaskKind: model.TaskKindContentGeneration, ExecutionProfile: "effective"})
	if err != nil {
		t.Fatal(err)
	}
	if a.AgentID != "wechat-article" {
		t.Fatalf("entry agent = %q", a.AgentID)
	}
	if _, err := svc.CreateEntry(t.Context(), CreatePlanEntryParams{UserID: userID, PlanID: planID, AgentID: "seednote", Channel: "seednote", TaskKind: model.TaskKindContentGeneration, ExecutionProfile: "effective"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateEntry(t.Context(), CreatePlanEntryParams{UserID: userID, PlanID: planID, AgentID: "wechat-article", Channel: "wechat-article", TaskKind: model.TaskKindContentGeneration, ExecutionProfile: "effective"}); !errors.Is(err, ErrDuplicatePlanAgent) {
		t.Fatalf("duplicate error = %v", err)
	}
	entries, err := svc.ListEntries(t.Context(), userID, planID)
	if err != nil || len(entries) != 2 {
		t.Fatalf("entries = %d, err = %v", len(entries), err)
	}
}

func planEntryTime(value time.Time) *time.Time { return &value }

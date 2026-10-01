package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openMultiAgentMigrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:multi-agent-migration-"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestMigrateMultiAgentChannelIdentityBackfillsSupportedRowsAndEntries(t *testing.T) {
	db := openMultiAgentMigrationDB(t)
	projectID := uuid.NewString()
	planID := uuid.NewString()
	if err := db.Create(&model.Project{ID: projectID, UserID: "user-1", Platform: model.PlatformWechat, Name: "legacy", Status: model.ProjectStatusActive}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Task{ID: uuid.NewString(), UserID: "user-1", ProjectID: projectID, Type: model.TaskTypeWechatArticle, Status: model.TaskStatusPending}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.Plan{ID: planID, UserID: "user-1", ProjectID: projectID, Type: model.TaskTypeWechatArticle, ExecutionProfile: "effective", CronExpr: "0 9 * * *", Status: model.PlanStatusActive, NextRunAt: func() *time.Time { v := time.Now().Add(time.Hour); return &v }()}).Error; err != nil {
		t.Fatal(err)
	}
	if err := MigrateMultiAgentChannelIdentity(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	var task model.Task
	if err := db.First(&task).Error; err != nil {
		t.Fatal(err)
	}
	if task.AgentID != model.AgentIDArticle || task.Channel != model.ChannelArticle || task.TaskKind != model.TaskKindContentGeneration {
		t.Fatalf("task identity = %q/%q/%q", task.AgentID, task.Channel, task.TaskKind)
	}
	var entries []model.PlanEntry
	if err := db.Where("plan_id = ?", planID).Find(&entries).Error; err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].AgentID != model.AgentIDArticle || entries[0].Channel != model.ChannelArticle {
		t.Fatalf("plan entries = %#v", entries)
	}
}

func TestAssertMultiAgentChannelReadinessAllowsHistoricalPluginIdentities(t *testing.T) {
	db := openMultiAgentMigrationDB(t)
	if err := db.Create(&model.Task{ID: uuid.NewString(), UserID: "user-1", Type: "montage", AgentID: "montage", Channel: "montage", TaskKind: model.TaskKindContentGeneration, Status: model.TaskStatusPending}).Error; err != nil {
		t.Fatal(err)
	}
	if err := AssertMultiAgentChannelReadiness(context.Background(), db); err != nil {
		t.Fatalf("historical plugin identity should remain executable: %v", err)
	}
	if err := db.Model(&model.Task{}).Where("type = ?", "montage").Updates(map[string]any{"channel": "wechat-article"}).Error; err != nil {
		t.Fatal(err)
	}
	err := AssertMultiAgentChannelReadiness(context.Background(), db)
	if err == nil || !strings.Contains(err.Error(), "outside supported product identities") {
		t.Fatalf("mismatched identity error = %v", err)
	}
}

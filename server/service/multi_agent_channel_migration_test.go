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

func TestMigrateMultiAgentChannelIdentityBackfillsEveryHistoricalTaskType(t *testing.T) {
	db := openMultiAgentMigrationDB(t)
	types := []struct {
		legacyType string
		agentID    string
		channel    string
		taskKind   string
	}{
		{model.TaskTypeWechatArticle, model.AgentIDArticle, model.ChannelArticle, model.TaskKindContentGeneration},
		{model.PlatformSeednote, model.AgentIDSeednote, model.ChannelSeednote, model.TaskKindContentGeneration},
		{model.TaskTypeWechatPicture, model.AgentIDWechatPicture, model.ChannelWechatPicture, model.TaskKindContentGeneration},
		{model.TaskTypeViralAnalysis, model.AgentIDSeednote, model.ChannelSeednote, model.TaskKindViralAnalysis},
		{model.TaskTypeProfileAnalysis, model.AgentIDProfileAnalysis, model.ChannelProfileAnalysis, model.TaskKindProfileAnalysis},
		{model.PlatformMontage, model.AgentIDMontage, model.ChannelMontage, model.TaskKindContentGeneration},
		{model.PlatformHypit, model.AgentIDHypit, model.ChannelHypit, model.PlatformHypit},
		{model.PlatformMoments, model.PlatformMoments, model.ChannelMoments, model.PlatformMoments},
		{model.PlatformEcommerce, model.PlatformEcommerce, model.ChannelEcommerce, model.PlatformEcommerce},
		{model.PlatformWhiteboardAnimation, model.AgentIDWhiteboard, model.ChannelWhiteboard, model.TaskKindContentGeneration},
		{"channels-video", model.AgentIDMontage, model.ChannelMontage, model.TaskKindContentGeneration},
	}
	for _, tt := range types {
		if err := db.Create(&model.Task{ID: uuid.NewString(), UserID: "user-1", Type: tt.legacyType, Status: model.TaskStatusPending}).Error; err != nil {
			t.Fatal(err)
		}
	}
	// A partially migrated row must be completed as well; checking only agent_id
	// leaves this exact row failing the startup readiness check.
	if err := db.Create(&model.Task{ID: uuid.NewString(), UserID: "user-1", Type: model.PlatformMontage, AgentID: model.AgentIDMontage, Status: model.TaskStatusPending}).Error; err != nil {
		t.Fatal(err)
	}

	if err := MigrateMultiAgentChannelIdentity(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	var tasks []model.Task
	if err := db.Order("created_at").Find(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	if len(tasks) != len(types)+1 {
		t.Fatalf("tasks = %d, want %d", len(tasks), len(types)+1)
	}
	for i, tt := range types {
		if got := tasks[i]; got.AgentID != tt.agentID || got.Channel != tt.channel || got.TaskKind != tt.taskKind {
			t.Errorf("task %q identity = %q/%q/%q, want %q/%q/%q", got.Type, got.AgentID, got.Channel, got.TaskKind, tt.agentID, tt.channel, tt.taskKind)
		}
	}
	partial := tasks[len(tasks)-1]
	if partial.AgentID != model.AgentIDMontage || partial.Channel != model.ChannelMontage || partial.TaskKind != model.TaskKindContentGeneration {
		t.Errorf("partial task identity = %q/%q/%q", partial.AgentID, partial.Channel, partial.TaskKind)
	}
}

func TestMigrateMultiAgentChannelIdentityCompletesPartialExecutionIdentity(t *testing.T) {
	db := openMultiAgentMigrationDB(t)
	taskID := uuid.NewString()
	if err := db.Create(&model.Task{ID: taskID, UserID: "user-1", Type: model.PlatformMontage, Status: model.TaskStatusRunning}).Error; err != nil {
		t.Fatal(err)
	}
	executionID := uuid.NewString()
	if err := db.Create(&model.TaskExecution{
		ID: executionID, TaskID: taskID, Attempt: 1, AgentID: model.AgentIDMontage,
		ExecutionProfile: "effective", Provider: "claude", ProfileEnvs: map[string]string{},
		ProfileFingerprint: "fingerprint", Target: "kubernetes", Status: model.TaskExecutionRunning,
	}).Error; err != nil {
		t.Fatal(err)
	}

	if err := MigrateMultiAgentChannelIdentity(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	var execution model.TaskExecution
	if err := db.First(&execution, "id = ?", executionID).Error; err != nil {
		t.Fatal(err)
	}
	if execution.AgentID != model.AgentIDMontage || execution.Channel != model.ChannelMontage || execution.TaskKind != model.TaskKindContentGeneration {
		t.Fatalf("execution identity = %q/%q/%q", execution.AgentID, execution.Channel, execution.TaskKind)
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

func TestAssertMultiAgentChannelReadinessIgnoresTerminalRowsWithoutIdentity(t *testing.T) {
	db := openMultiAgentMigrationDB(t)
	if err := db.Create(&model.Task{ID: uuid.NewString(), UserID: "user-1", Type: "removed-workflow", Status: model.TaskStatusCompleted}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.TaskExecution{
		ID: uuid.NewString(), TaskID: uuid.NewString(), Attempt: 1,
		ExecutionProfile: "effective", Provider: "claude", ProfileEnvs: map[string]string{},
		ProfileFingerprint: "fingerprint", Target: "kubernetes", Status: model.TaskExecutionSucceeded,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := AssertMultiAgentChannelReadiness(context.Background(), db); err != nil {
		t.Fatalf("terminal historical rows should not block readiness: %v", err)
	}
}

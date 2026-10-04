package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
	"gorm.io/datatypes"
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
	if err := db.Create(&model.Plan{ID: planID, UserID: "user-1", ProjectID: projectID, ExecutionProfile: "effective", CronExpr: "0 9 * * *", Status: model.PlanStatusActive, NextRunAt: func() *time.Time { v := time.Now().Add(time.Hour); return &v }()}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ALTER TABLE plans ADD COLUMN type varchar(40)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("plans").Where("id = ?", planID).Update("type", "article").Error; err != nil {
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
		{model.PlatformMontage, model.AgentIDMontage, model.ChannelMontage, model.PlatformMontage},
		{model.PlatformHypit, model.AgentIDHypit, model.ChannelHypit, model.PlatformHypit},
		{model.PlatformMoments, model.PlatformMoments, model.ChannelMoments, model.PlatformMoments},
		{model.PlatformEcommerce, model.PlatformEcommerce, model.ChannelEcommerce, model.PlatformEcommerce},
		{model.PlatformWhiteboardAnimation, model.AgentIDWhiteboard, model.ChannelWhiteboard, model.PlatformWhiteboardAnimation},
		{"channels-video", model.AgentIDMontage, model.ChannelMontage, model.PlatformMontage},
	}
	for _, tt := range types {
		if err := db.Create(&model.Task{ID: uuid.NewString(), UserID: "user-1", Type: tt.legacyType, Status: model.TaskStatusPending}).Error; err != nil {
			t.Fatal(err)
		}
	}
	// A partially migrated row must be completed as well; checking only agent_id
	// leaves this exact row failing the startup readiness check.
	if err := db.Create(&model.Task{ID: uuid.NewString(), UserID: "user-1", AgentID: model.AgentIDMontage, Status: model.TaskStatusPending}).Error; err != nil {
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
	if partial.AgentID != model.AgentIDMontage || partial.Channel != model.ChannelMontage || partial.TaskKind != model.PlatformMontage {
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
	if execution.AgentID != model.AgentIDMontage || execution.Channel != model.ChannelMontage || execution.TaskKind != model.PlatformMontage {
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

func TestProjectPlanCutoverMigratesCredentialsAndRemovesLegacyData(t *testing.T) {
	db := openMultiAgentMigrationDB(t)
	ctx := context.Background()
	for _, ddl := range []string{
		"ALTER TABLE projects ADD COLUMN config json", "ALTER TABLE projects ADD COLUMN agent_config json",
		"CREATE TABLE project_agent_configs (id varchar(36), project_id varchar(36), config json)",
		"ALTER TABLE plans ADD COLUMN type varchar(40)", "ALTER TABLE plans ADD COLUMN agent_input json",
		"ALTER TABLE plans ADD COLUMN montage_input json", "ALTER TABLE plans ADD COLUMN article_with_cover boolean",
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatal(err)
		}
	}
	project := &model.Project{ID: uuid.NewString(), UserID: "u", Platform: model.PlatformWechat, Name: "Account"}
	if err := db.Create(project).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("projects").Where("id = ?", project.ID).Updates(map[string]any{"config": `{"wechat_app_id":"test-app","wechat_secret":"test-secret"}`, "agent_config": `{"obsolete":true}`}).Error; err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: uuid.NewString(), UserID: "u", Type: model.TaskTypeWechatArticle, Status: model.TaskStatusCompleted}
	if err := db.Create(task).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(task).Update("project_snapshot", `{"project_name":"Frozen","agent_config":{"obsolete":true},"keywords":"keep"}`).Error; err != nil {
		t.Fatal(err)
	}
	planIDs := map[string]string{}
	for _, kind := range []string{"article", "seednote", "wechat-picture", "montage", "removed-plugin"} {
		id := uuid.NewString()
		planIDs[kind] = id
		if err := db.Create(&model.Plan{ID: id, UserID: "u", ProjectID: project.ID, ExecutionProfile: "balanced", Status: model.PlanStatusActive}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Table("plans").Where("id = ?", id).Update("type", kind).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := MigrateMultiAgentChannelIdentity(ctx, db); err != nil {
			t.Fatal(err)
		}
		if err := MigrateProjectAgentConfigRemoval(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	for _, column := range []string{"config", "agent_config"} {
		if db.Migrator().HasColumn("projects", column) {
			t.Fatalf("projects.%s retained", column)
		}
	}
	for _, column := range []string{"type", "agent_input", "montage_input", "article_with_cover"} {
		if db.Migrator().HasColumn("plans", column) {
			t.Fatalf("plans.%s retained", column)
		}
	}
	if db.Migrator().HasTable("project_agent_configs") {
		t.Fatal("Agent config table retained")
	}
	var config model.ProjectChannelConfig
	if err := db.Where("project_id = ? AND channel = ?", project.ID, model.ChannelArticle).First(&config).Error; err != nil {
		t.Fatal(err)
	}
	if config.Config.Data()["wechat_app_id"] != "test-app" || config.Config.Data()["wechat_secret"] != "test-secret" {
		t.Fatal("credentials not preserved")
	}
	var pictureConfig model.ProjectChannelConfig
	if err := db.Where("project_id = ? AND channel = ?", project.ID, model.ChannelWechatPicture).First(&pictureConfig).Error; err != nil {
		t.Fatal(err)
	}
	if pictureConfig.Config.Data()["wechat_secret"] != "test-secret" {
		t.Fatal("picture credentials not migrated")
	}
	var snapshot string
	if err := db.Table("tasks").Where("id = ?", task.ID).Select("project_snapshot").Scan(&snapshot).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(snapshot, "agent_config") || !strings.Contains(snapshot, "keep") {
		t.Fatalf("snapshot cleanup = %s", snapshot)
	}
	for kind, id := range planIDs {
		var plan model.Plan
		if err := db.First(&plan, "id = ?", id).Error; err != nil {
			t.Fatal(err)
		}
		var entries []model.PlanEntry
		if err := db.Where("plan_id = ?", id).Find(&entries).Error; err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 {
			t.Fatalf("%s entries = %d", kind, len(entries))
		}
		unsupported := kind == "montage" || kind == "removed-plugin"
		if unsupported {
			if plan.Status != model.PlanStatusPaused || entries[0].Status != model.PlanEntryStatusPaused {
				t.Fatalf("unsupported %s remained active", kind)
			}
		} else if entries[0].TaskKind != model.TaskKindContentGeneration || plan.Status != model.PlanStatusActive {
			t.Fatalf("%s not normalized", kind)
		}
	}
}

func TestCutoverPausesUnsupportedExistingEntryAndPreservesPictureCredentials(t *testing.T) {
	db := openMultiAgentMigrationDB(t)
	ctx := context.Background()
	if err := db.Exec("ALTER TABLE plans ADD COLUMN type varchar(40)").Error; err != nil {
		t.Fatal(err)
	}
	plan := &model.Plan{ID: uuid.NewString(), UserID: "u", ProjectID: "p", Status: model.PlanStatusActive}
	if err := db.Create(plan).Error; err != nil {
		t.Fatal(err)
	}
	entry := &model.PlanEntry{ID: uuid.NewString(), PlanID: plan.ID, AgentID: model.AgentIDSeednote, Channel: model.ChannelSeednote, TaskKind: model.TaskKindViralAnalysis, Status: model.PlanEntryStatusActive}
	if err := db.Create(entry).Error; err != nil {
		t.Fatal(err)
	}
	for _, channel := range []string{model.ChannelArticle, model.ChannelWechatPicture} {
		row := &model.ProjectChannelConfig{ID: uuid.NewString(), ProjectID: "p", Channel: channel}
		row.Config = datatypes.NewJSONType(map[string]any{"wechat_app_id": channel})
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := MigrateMultiAgentChannelIdentity(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := db.First(entry, "id = ?", entry.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(plan, "id = ?", plan.ID).Error; err != nil {
		t.Fatal(err)
	}
	if entry.Status != model.PlanEntryStatusPaused || plan.Status != model.PlanStatusPaused {
		t.Fatal("unsupported task kind remained active")
	}
	var picture model.ProjectChannelConfig
	if err := db.Where("channel = ?", model.ChannelWechatPicture).First(&picture).Error; err != nil {
		t.Fatal(err)
	}
	if picture.Config.Data()["wechat_app_id"] != model.ChannelWechatPicture {
		t.Fatal("dedicated picture credentials overwritten")
	}
	// Re-running after cutover must not restore a deliberately removed connector.
	if err := db.Delete(&picture).Error; err != nil {
		t.Fatal(err)
	}
	if err := MigrateMultiAgentChannelIdentity(ctx, db); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&model.ProjectChannelConfig{}).Where("channel = ?", model.ChannelWechatPicture).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("startup reintroduced deleted picture connector")
	}
}

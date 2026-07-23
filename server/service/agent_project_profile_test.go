package service

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/resources"
)

func TestAgentProjectProfileUsesTaskSnapshotWithoutImageRouteMetadata(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "profile.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.Project{}, &model.Task{}, &model.TaskFile{}, &model.Template{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	logger := zerolog.Nop()
	projectSvc := NewProjectService(repo, &logger)
	taskSvc := NewTaskService(repo, nil, nil, nil, &logger, "", nil, "", nil, nil)
	svc := NewAgentProjectProfileService(projectSvc, taskSvc, resources.Manager(), config.MontageConfig{})

	userID := uuid.NewString()
	project := &model.Project{
		ID: uuid.NewString(), UserID: userID, Platform: model.PlatformSeednote,
		Name: "current", Instructions: "current instructions", VisualStyle: "current style",
	}
	if err := repo.Projects().Create(context.Background(), project); err != nil {
		t.Fatal(err)
	}
	task := &model.Task{
		ID: uuid.NewString(), UserID: userID, ProjectID: project.ID,
		Type: model.PlatformSeednote, Status: model.TaskStatusPending,
		ImageModelKey: "server-owned-route",
	}
	task.SetProjectSnapshot(model.ProjectSnapshot{
		ProjectName: "snapshot", Platform: model.PlatformSeednote,
		Instructions: "snapshot instructions", VisualStyle: "snapshot style",
		ReferenceImageAssetID: "asset-id",
	})
	if err := repo.Tasks().Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}

	profile, err := svc.Get(context.Background(), AgentProjectProfileRequest{
		UserID: userID, ProjectID: project.ID, TaskID: task.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := (*profile)["name"]; got != "snapshot" {
		t.Fatalf("name = %v, want snapshot", got)
	}
	if got := (*profile)["visual_style"]; got != "snapshot style" {
		t.Fatalf("visual_style = %v, want snapshot style", got)
	}
	if got := (*profile)["visual_style_source"]; got != "snapshot" {
		t.Fatalf("visual_style_source = %v, want snapshot", got)
	}
	resolved := (*profile)["resolved_profile"].(map[string]any)
	if got := resolved["reference_image_path"]; got != ".anban-creator/reference.png" {
		t.Fatalf("reference_image_path = %v", got)
	}
	raw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"image_generation", "image_model", "server-owned-route", "selection_reason", "provider", "model"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("profile exposes route metadata %q: %s", forbidden, raw)
		}
	}
}

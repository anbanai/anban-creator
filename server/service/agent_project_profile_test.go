package service

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
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

func TestAgentProjectProfileReturnsPublicImageCapabilityMetadata(t *testing.T) {
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
	taskSvc := newTestTaskService(repo, nil, nil, &logger, "", nil, nil)
	svc := NewAgentProjectProfileService(projectSvc, taskSvc, resources.Manager(), config.MontageConfig{}, agentProjectProfileImageCapabilityResolver())

	userID := uuid.NewString()
	project := &model.Project{
		ID: uuid.NewString(), UserID: userID, Platform: model.PlatformSeednote,
		Name: "current", Instructions: "current instructions", VisualStyle: "current style",
	}
	project.SetAgentConfig(map[string]any{"audience": "current"})
	if err := repo.Projects().Create(context.Background(), project); err != nil {
		t.Fatal(err)
	}
	task := &model.Task{
		ID: uuid.NewString(), UserID: userID, ProjectID: project.ID,
		Type: model.PlatformSeednote, Status: model.TaskStatusPending,
		ImageCapabilityKey: "server-owned-route", ImageRatio: "3:4",
	}
	task.SetProjectSnapshot(model.ProjectSnapshot{
		ProjectName: "snapshot", Platform: model.PlatformSeednote,
		Instructions: "snapshot instructions", VisualStyle: "snapshot style",
		ReferenceImageAssetID: "asset-id", ImageRatio: "3:4",
		AgentConfig: map[string]any{"audience": "snapshot"},
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
	if got := (*profile)["agent_config"].(map[string]any)["audience"]; got != "snapshot" {
		t.Fatalf("agent_config.audience = %v, want frozen snapshot", got)
	}
	resolved := (*profile)["resolved_profile"].(map[string]any)
	if got := resolved["agent_config"].(map[string]any)["audience"]; got != "snapshot" {
		t.Fatalf("resolved_profile.agent_config.audience = %v, want frozen snapshot", got)
	}
	if got := resolved["reference_image_path"]; got != ".anban-creator/reference.png" {
		t.Fatalf("reference_image_path = %v", got)
	}
	if got := resolved["image_ratio"]; got != "3:4" {
		t.Fatalf("effective image_ratio = %v, want task value 3:4", got)
	}
	if got := resolved["image_capability_key"]; got != "server-owned-route" {
		t.Fatalf("image_capability_key = %v, want task value server-owned-route", got)
	}
	wantRatios := []string{"3:4", "1:1", "4:3"}
	if got := resolved["allowed_image_ratios"]; !reflect.DeepEqual(got, wantRatios) {
		t.Fatalf("allowed_image_ratios = %#v, want %#v", got, wantRatios)
	}
	if _, exists := resolved["supported_sizes"]; exists {
		t.Fatal("resolved profile must not expose supported_sizes")
	}
	raw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		"image_generation", "image_model", "selection_reason", "provider", "model",
		"private-provider", "private-model", "https://internal-route.invalid/v1", "private-api-key", "private-billing-sku",
	} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("profile exposes route metadata %q: %s", forbidden, raw)
		}
	}
}

func TestAgentProjectProfileUsesDefaultImageCapability(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "profile-default.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.Project{}, &model.Task{}, &model.TaskFile{}, &model.Template{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	logger := zerolog.Nop()
	projectSvc := NewProjectService(repo, &logger)
	taskSvc := newTestTaskService(repo, nil, nil, &logger, "", nil, nil)
	svc := NewAgentProjectProfileService(projectSvc, taskSvc, resources.Manager(), config.MontageConfig{}, agentProjectProfileImageCapabilityResolver())

	userID := uuid.NewString()
	project := &model.Project{ID: uuid.NewString(), UserID: userID, Platform: model.PlatformSeednote, Name: "default capability"}
	if err := repo.Projects().Create(context.Background(), project); err != nil {
		t.Fatal(err)
	}
	task := &model.Task{
		ID: uuid.NewString(), UserID: userID, ProjectID: project.ID,
		Type: model.PlatformSeednote, Status: model.TaskStatusPending,
	}
	task.SetProjectSnapshot(model.SnapshotProject(project))
	if err := repo.Tasks().Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}

	profile, err := svc.Get(context.Background(), AgentProjectProfileRequest{UserID: userID, ProjectID: project.ID, TaskID: task.ID})
	if err != nil {
		t.Fatal(err)
	}
	resolved := (*profile)["resolved_profile"].(map[string]any)
	if got := resolved["image_capability_key"]; got != "default-route" {
		t.Fatalf("image_capability_key = %v, want configured default default-route", got)
	}
	if got, want := resolved["allowed_image_ratios"], []string{"3:4", "1:1", "4:3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("allowed_image_ratios = %#v, want %#v", got, want)
	}
}

func agentProjectProfileImageCapabilityResolver() *ImageCapabilityResolver {
	return NewImageCapabilityResolver(nil, &config.Config{ModelRoutes: config.ModelRoutesConfig{
		ImageGeneration: config.ImageGenerationRoutesConfig{
			DefaultCapability: "default-route",
			Capabilities: map[string]config.ImageGenerationRouteConfig{
				"default-route": {
					Enabled: true, MinTier: "free",
					DesignerFeatures: config.DesignerProviderCapabilities{SizePresets: []string{"1:1", "4:3"}},
				},
				"server-owned-route": {
					Provider: "private-provider", Model: "private-model", BaseURL: "https://internal-route.invalid/v1",
					APIKey: "private-api-key", BillingSKU: "private-billing-sku", Enabled: true, MinTier: "free",
					DesignerFeatures: config.DesignerProviderCapabilities{SizePresets: []string{"1:1", "3:4", "16:9"}},
				},
			},
		},
	}})
}

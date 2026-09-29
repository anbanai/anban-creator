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
	"gorm.io/datatypes"
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
	if got := resolved["project_style_reference_path"]; got != ".anban-creator/project-style-reference.png" {
		t.Fatalf("project_style_reference_path = %v", got)
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

func TestAgentProjectProfileIncludesMatchingFeedbackStrategy(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "profile-feedback.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.Project{}, &model.Task{}, &model.TaskFile{}, &model.Template{}, &model.StrategySnapshot{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	logger := zerolog.Nop()
	projectSvc := NewProjectService(repo, &logger)
	taskSvc := newTestTaskService(repo, nil, nil, &logger, "", nil, nil)
	svc := NewAgentProjectProfileService(projectSvc, taskSvc, resources.Manager(), config.MontageConfig{}, agentProjectProfileImageCapabilityResolver(), repo)
	userID := uuid.NewString()
	project := &model.Project{ID: uuid.NewString(), UserID: userID, Platform: model.PlatformArticle, Name: "feedback project"}
	if err := repo.Projects().Create(context.Background(), project); err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: project.ID, Type: model.PlatformArticle, Status: model.TaskStatusPending, ImageCapabilityKey: "server-owned-route"}
	if err := repo.Tasks().Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.StrategySnapshot{ID: uuid.NewString(), ProjectID: project.ID, Platform: model.PlatformArticle, Revision: 1, SourceRevision: 3, Digest: "digest", Status: "active", ApplicableTasks: `["article"]`, Recommendations: `["keep"]`, Evidence: `{"sample_count":10}`, Confidence: "medium", Limitations: "advisory"}).Error; err != nil {
		t.Fatal(err)
	}
	profile, err := svc.Get(context.Background(), AgentProjectProfileRequest{UserID: userID, ProjectID: project.ID, TaskID: task.ID})
	if err != nil {
		t.Fatal(err)
	}
	feedback, ok := (*profile)["feedback_strategy"].(map[string]any)
	if !ok || feedback["available"] != true || feedback["strategy_revision"] != int64(1) {
		t.Fatalf("feedback_strategy = %#v, want active matching strategy", (*profile)["feedback_strategy"])
	}
}

func TestAgentProjectProfileRejectsTaskWithoutFrozenImageCapability(t *testing.T) {
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
	if profile != nil || err == nil || !strings.Contains(err.Error(), "frozen image capability") {
		t.Fatalf("Get = %#v, %v; want missing frozen capability rejection", profile, err)
	}

	profile, err = svc.Get(context.Background(), AgentProjectProfileRequest{UserID: userID, ProjectID: project.ID})
	if err != nil {
		t.Fatalf("project-only profile: %v", err)
	}
	resolved := (*profile)["resolved_profile"].(map[string]any)
	if got := resolved["image_capability_key"]; got != "default-route" {
		t.Fatalf("project-only image_capability_key = %v, want configured default default-route", got)
	}
	if got, want := resolved["allowed_image_ratios"], []string{"3:4", "1:1", "4:3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("allowed_image_ratios = %#v, want %#v", got, want)
	}
}

func TestAgentProjectProfileOnlyExposesConfirmedAccountProfile(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "profile-confirmation.db")), &gorm.Config{})
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
	project := &model.Project{ID: uuid.NewString(), UserID: userID, Platform: model.PlatformArticle, Name: "profile gate"}
	draft := model.NewProjectProfile()
	draft.Dimensions.Identity.Content["name"] = "inferred"
	project.Profile = datatypes.NewJSONType(draft)
	if err := repo.Projects().Create(context.Background(), project); err != nil {
		t.Fatal(err)
	}

	got, err := svc.Get(context.Background(), AgentProjectProfileRequest{UserID: userID, ProjectID: project.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := (*got)["account_profile"]; ok {
		t.Fatal("unconfirmed profile leaked as account_profile")
	}
	resolved := (*got)["resolved_profile"].(map[string]any)
	if _, ok := resolved["account_profile"]; ok {
		t.Fatal("unconfirmed profile leaked in resolved_profile")
	}
	if resolved["account_profile_status"] != model.ProfileStatusDraft {
		t.Fatalf("account_profile_status = %v, want draft", resolved["account_profile_status"])
	}

	draft.Status = model.ProfileStatusConfirmed
	draft.Version = 1
	project.Profile = datatypes.NewJSONType(draft)
	if err := repo.Projects().Update(context.Background(), project); err != nil {
		t.Fatal(err)
	}
	got, err = svc.Get(context.Background(), AgentProjectProfileRequest{UserID: userID, ProjectID: project.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := (*got)["account_profile"]; !ok {
		t.Fatal("confirmed profile missing from account_profile")
	}
	if _, ok := (*got)["resolved_profile"].(map[string]any)["account_profile"]; !ok {
		t.Fatal("confirmed profile missing from resolved_profile")
	}
}

func agentProjectProfileImageCapabilityResolver() *ImageCapabilityResolver {
	return NewImageCapabilityResolver(nil, &config.Config{ModelRoutes: config.ModelRoutesConfig{
		ImageGeneration: config.ImageGenerationRoutesConfig{
			DefaultCapability: "default-route",
			Capabilities: map[string]config.ImageGenerationRouteConfig{
				"default-route": {
					Enabled: true, MinTier: "free",
					GenerationFeatures: config.ImageGenerationFeatures{SizePresets: []string{"1:1", "4:3"}},
				},
				"server-owned-route": {
					Provider: "private-provider", Model: "private-model", BaseURL: "https://internal-route.invalid/v1",
					APIKey: "private-api-key", BillingSKU: "private-billing-sku", Enabled: true, MinTier: "free",
					GenerationFeatures: config.ImageGenerationFeatures{SizePresets: []string{"1:1", "3:4", "16:9"}},
				},
			},
		},
	}})
}

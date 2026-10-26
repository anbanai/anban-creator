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
	if _, exists := (*profile)["agent_config"]; exists {
		t.Fatal("profile must not expose project-level Agent configuration")
	}
	if _, exists := resolved["agent_config"]; exists {
		t.Fatal("resolved profile must not expose project-level Agent configuration")
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

func TestSixCreativeObjectsReadTheSameFrozenSavedDraftProfile(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "shared-profile.db")), &gorm.Config{})
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
	svc := NewAgentProjectProfileService(projectSvc, taskSvc, nil, config.MontageConfig{}, agentProjectProfileImageCapabilityResolver())
	profile := model.NewProjectProfile()
	profile.Status, profile.Version = model.ProfileStatusDraft, 2
	profile.ApplicationSource = model.ProfileApplicationSourceSystemAnalysis
	profile.InitializationStatus = model.ProfileInitializationRunning
	profile.Dimensions.Identity.Content["positioning"] = "推断定位"
	profile.Dimensions.Identity.Sources = []string{"[推断待确认]"}
	profile.Dimensions.Preferences.Content["red_lines"] = "禁止编造成交案例"
	profile.Dimensions.Preferences.Sources = []string{"[用户确认]"}
	project := &model.Project{ID: uuid.NewString(), UserID: uuid.NewString(), Name: "共享画像", Platform: model.PlatformWechat, Profile: datatypes.NewJSONType(profile)}
	if err := repo.Projects().Create(t.Context(), project); err != nil {
		t.Fatal(err)
	}
	types := []string{model.TaskTypeWechatArticle, model.TaskTypeWechatPicture, model.PlatformSeednote, model.PlatformMontage, model.PlatformHypit, model.PlatformWhiteboardAnimation}
	for _, taskType := range types {
		t.Run(taskType, func(t *testing.T) {
			task := &model.Task{ID: uuid.NewString(), UserID: project.UserID, ProjectID: project.ID, Type: taskType, Status: model.TaskStatusPending, ImageCapabilityKey: "server-owned-route"}
			task.SetProjectSnapshot(model.SnapshotProject(project))
			if err := repo.Tasks().Create(t.Context(), task); err != nil {
				t.Fatal(err)
			}
			got, err := svc.Get(t.Context(), AgentProjectProfileRequest{UserID: project.UserID, ProjectID: project.ID, TaskID: task.ID})
			if err != nil {
				t.Fatal(err)
			}
			applied, ok := (*got)["account_profile"].(model.ProjectProfile)
			if !ok || !reflect.DeepEqual(applied, profile) {
				t.Fatalf("%s account_profile = %#v, want shared frozen profile", taskType, (*got)["account_profile"])
			}
			resolved := (*got)["resolved_profile"].(map[string]any)
			if !reflect.DeepEqual(resolved["account_profile"], profile) || resolved["uses_project_snapshot"] != true {
				t.Fatalf("%s resolved_profile = %#v", taskType, resolved)
			}
			// The account profile is the task's immutable context, even after the
			// current project profile changes during a running generation.
			updated := *project
			changed := profile
			changed.Version = 3
			updated.Profile = datatypes.NewJSONType(changed)
			if err := repo.Projects().Update(t.Context(), &updated); err != nil {
				t.Fatal(err)
			}
			frozen, err := svc.Get(t.Context(), AgentProjectProfileRequest{UserID: project.UserID, ProjectID: project.ID, TaskID: task.ID})
			if err != nil || !reflect.DeepEqual((*frozen)["account_profile"], profile) {
				t.Fatalf("%s context changed after project edit: %#v, %v", taskType, frozen, err)
			}
			if _, err := svc.Get(t.Context(), AgentProjectProfileRequest{UserID: uuid.NewString(), ProjectID: project.ID, TaskID: task.ID}); err == nil {
				t.Fatal("different user read project profile")
			}
			other := &model.Project{ID: uuid.NewString(), UserID: project.UserID, Name: "other", Platform: model.PlatformWechat}
			if err := repo.Projects().Create(t.Context(), other); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.Get(t.Context(), AgentProjectProfileRequest{UserID: project.UserID, ProjectID: other.ID, TaskID: task.ID}); err == nil {
				t.Fatal("different project read task profile")
			}
		})
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
	project := &model.Project{ID: uuid.NewString(), UserID: userID, Platform: model.PlatformWechat, Name: "feedback project"}
	if err := repo.Projects().Create(context.Background(), project); err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: project.ID, Type: model.TaskTypeWechatArticle, AgentID: model.AgentIDArticle, Channel: model.ChannelArticle, TaskKind: model.TaskKindContentGeneration, Status: model.TaskStatusPending, ImageCapabilityKey: "server-owned-route"}
	if err := repo.Tasks().Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.StrategySnapshot{ID: uuid.NewString(), ProjectID: project.ID, Platform: model.PlatformWechat, Revision: 1, SourceRevision: 3, Digest: "digest", Status: "active", ApplicableTasks: `["content_generation"]`, Recommendations: `["keep"]`, Evidence: `{"sample_count":10}`, Confidence: "medium", Limitations: "advisory"}).Error; err != nil {
		t.Fatal(err)
	}
	profile, err := svc.Get(context.Background(), AgentProjectProfileRequest{UserID: userID, ProjectID: project.ID, TaskID: task.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"available_themes", "available_writers", "available_article_templates"} {
		if reflect.ValueOf((*profile)[key]).Len() == 0 {
			t.Fatalf("article task must retain %s", key)
		}
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

func TestAgentProjectProfileExposesSavedAccountProfileAcrossLifecycleStates(t *testing.T) {
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
	project := &model.Project{ID: uuid.NewString(), UserID: userID, Platform: model.PlatformWechat, Name: "profile gate"}
	draft := model.NewProjectProfile()
	draft.Dimensions.Identity.Content["name"] = "inferred"
	project.Profile = datatypes.NewJSONType(draft)
	if err := repo.Projects().Create(context.Background(), project); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name, status, initialization string
		version                      int64
		absent                       bool
	}{
		{"draft ready", model.ProfileStatusDraft, model.ProfileInitializationReady, 7, false},
		{"draft uninitialized", model.ProfileStatusDraft, model.ProfileInitializationNotStarted, 0, false},
		{"confirmed", model.ProfileStatusConfirmed, model.ProfileInitializationReady, 1, false},
		{"absent", model.ProfileStatusDraft, model.ProfileInitializationNotStarted, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			draft.Status, draft.InitializationStatus, draft.Version = tc.status, tc.initialization, tc.version
			draft.Dimensions.Identity.Sources = []string{"[推断待确认]"}
			project.Profile = datatypes.NewJSONType(draft)
			if tc.absent {
				project.Profile = datatypes.NewJSONType(model.ProjectProfile{})
			}
			if err := repo.Projects().Update(context.Background(), project); err != nil {
				t.Fatal(err)
			}
			got, err := svc.Get(context.Background(), AgentProjectProfileRequest{UserID: userID, ProjectID: project.ID})
			if err != nil {
				t.Fatal(err)
			}
			account, ok := (*got)["account_profile"].(model.ProjectProfile)
			if !ok || account.SchemaVersion != 1 || account.Status != tc.status || account.InitializationStatus != tc.initialization || account.Version != tc.version {
				t.Fatalf("account_profile = %#v", (*got)["account_profile"])
			}
			if !tc.absent && (account.Dimensions.Identity.Content["name"] != "inferred" || !reflect.DeepEqual(account.Dimensions.Identity.Sources, []string{"[推断待确认]"})) {
				t.Fatalf("saved dimensions or provenance lost: %#v", account)
			}
			resolved := (*got)["resolved_profile"].(map[string]any)
			if !reflect.DeepEqual(resolved["account_profile"], account) {
				t.Fatalf("resolved account_profile = %#v", resolved)
			}
			if tc.status == model.ProfileStatusDraft && resolved["account_profile_status"] != model.ProfileStatusDraft {
				t.Fatalf("account_profile_status = %v, want draft", resolved["account_profile_status"])
			}
		})
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

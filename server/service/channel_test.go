package service

import (
	"context"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/app/writer"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

func setupTestProjectService(t *testing.T) (*ProjectService, repository.Repository) {
	t.Helper()
	db := setupTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	return NewProjectService(repo, &logger), repo
}

// Create/Update store ONLY what the user set — they do NOT bake the article
// writer default. The platform writer default (writer.DefaultStyleName) is
// injected at RESOLUTION time (resolver.ResolveStyle) so every consumer (MCP,
// prompt, settings.json) agrees on the single source of truth. These tests pin
// that contract: the stored fields stay empty, and the default surfaces only via
// ResolveStyle.
func TestProjectServiceDoesNotBakeArticleWriter(t *testing.T) {
	for _, platform := range []string{model.PlatformArticle} {
		t.Run(platform, func(t *testing.T) {
			svc, _ := setupTestProjectService(t)
			ch, err := svc.Create(context.Background(), "user-1", &model.Project{
				Platform: platform,
				Name:     "Default Style Project",
			})
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			// The visual dimension (图片视觉) must NEVER be seeded with a writer key
			// (the dan-koe → Victorian-woodcut bug), and the writer key is empty on
			// the stored project too — the default is applied at resolution, not here.
			if ch.Writer != "" {
				t.Fatalf("Writer = %q, want empty — Create must not bake the default", ch.Writer)
			}
			if ch.VisualStyle != "" {
				t.Fatalf("VisualStyle = %q, want empty — writer key must not leak into visual style", ch.VisualStyle)
			}
			// The article writer default surfaces at resolution, the single place
			// every delivery channel reads it.
			if got := ResolveStyle(ch, nil).Writer; got != writer.DefaultStyleName {
				t.Fatalf("ResolveStyle Writer = %q, want %q", got, writer.DefaultStyleName)
			}
		})
	}
}

func TestProjectServiceDoesNotDefaultSeednoteStyle(t *testing.T) {
	svc, _ := setupTestProjectService(t)
	ch, err := svc.Create(context.Background(), "user-1", &model.Project{
		Platform: model.PlatformSeednote,
		Name:     "Seednote Project",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ch.VisualStyle != "" {
		t.Fatalf("VisualStyle = %q, want empty", ch.VisualStyle)
	}
}

func TestProjectServiceVideoIgnoresVisualStyleOnCreateAndUpdate(t *testing.T) {
	svc, _ := setupTestProjectService(t)
	ch, err := svc.Create(context.Background(), "user-1", &model.Project{
		Platform:     model.PlatformVideo,
		Name:         "Video Project",
		Instructions: "品牌、人设、账号、产品基础信息和视频风格都写在这里",
		VisualStyle:  "旧视频风格入口不应保存",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ch.VisualStyle != "" {
		t.Fatalf("created video VisualStyle = %q, want empty because video profile belongs to Instructions", ch.VisualStyle)
	}

	ch.VisualStyle = "存量旧视频风格入口"
	if err := svc.repo.Projects().Update(context.Background(), ch); err != nil {
		t.Fatalf("seed legacy visual style: %v", err)
	}

	updated, err := svc.Update(context.Background(), "user-1", ch.ID, &model.Project{
		Platform:        model.PlatformVideo,
		Name:            "Video Project Updated",
		Instructions:    "更新后的项目定位",
		InstructionsSet: true,
		VisualStyle:     "更新请求里的旧视频风格入口也应忽略",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.VisualStyle != "" {
		t.Fatalf("updated video VisualStyle = %q, want empty because video profile belongs to Instructions", updated.VisualStyle)
	}
	if updated.Instructions != "更新后的项目定位" {
		t.Fatalf("updated Instructions = %q, want 更新后的项目定位", updated.Instructions)
	}
}

func TestProjectServiceUpdateDoesNotBakeArticleWriter(t *testing.T) {
	svc, _ := setupTestProjectService(t)
	created, err := svc.Create(context.Background(), "user-1", &model.Project{
		Platform: model.PlatformArticle,
		Name:     "Article Project",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := svc.Update(context.Background(), "user-1", created.ID, &model.Project{
		Platform:    model.PlatformArticle,
		Name:        "Article Project",
		VisualStyle: "",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	// Update does not bake the default either; it surfaces at resolution.
	if updated.Writer != "" {
		t.Fatalf("Writer = %q, want empty", updated.Writer)
	}
	if updated.VisualStyle != "" {
		t.Fatalf("VisualStyle = %q, want empty", updated.VisualStyle)
	}
	if got := ResolveStyle(updated, nil).Writer; got != writer.DefaultStyleName {
		t.Fatalf("ResolveStyle Writer = %q, want %q", got, writer.DefaultStyleName)
	}
}

func TestProjectServiceUpdatePreservesExistingWriterWhenOmitted(t *testing.T) {
	svc, _ := setupTestProjectService(t)
	created, err := svc.Create(context.Background(), "user-1", &model.Project{
		Platform: model.PlatformArticle,
		Name:     "Article Project",
		Writer:   "casual-science",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Update omits Writer entirely; the stored value must be preserved
	// (PATCH "non-empty = set" semantics).
	updated, err := svc.Update(context.Background(), "user-1", created.ID, &model.Project{
		Platform: model.PlatformArticle,
		Name:     "Renamed Article Project",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Writer != "casual-science" {
		t.Fatalf("Writer = %q, want casual-science", updated.Writer)
	}
}

func TestProjectServiceUpdateInstructionsSetControlsClear(t *testing.T) {
	svc, _ := setupTestProjectService(t)
	created, err := svc.Create(context.Background(), "user-1", &model.Project{
		Platform:        model.PlatformSeednote,
		Name:            "Seednote Project",
		Instructions:    "原定位",
		InstructionsSet: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := svc.Update(context.Background(), "user-1", created.ID, &model.Project{
		Name: "Renamed",
	})
	if err != nil {
		t.Fatalf("Update omitted instructions: %v", err)
	}
	if updated.Instructions != "原定位" {
		t.Fatalf("Instructions after omitted update = %q, want 原定位", updated.Instructions)
	}

	updated, err = svc.Update(context.Background(), "user-1", created.ID, &model.Project{
		Instructions:    "",
		InstructionsSet: true,
	})
	if err != nil {
		t.Fatalf("Update clearing instructions: %v", err)
	}
	if updated.Instructions != "" {
		t.Fatalf("Instructions after explicit clear = %q, want empty", updated.Instructions)
	}
}

func TestProjectServiceUpdatePersistsRequirePublishApproval(t *testing.T) {
	svc, _ := setupTestProjectService(t)
	created, err := svc.Create(context.Background(), "user-1", &model.Project{
		Platform: model.PlatformArticle,
		Name:     "Article",
		Config: model.ProjectConfig{
			EnablePublishing:       true,
			RequirePublishApproval: false,
		},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := svc.Update(context.Background(), "user-1", created.ID, &model.Project{
		Config: model.ProjectConfig{
			EnablePublishing:       true,
			RequirePublishApproval: true,
		},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !updated.Config.RequirePublishApproval {
		t.Fatal("RequirePublishApproval was not persisted on update")
	}
}

func TestProjectServiceRejectsUnconfiguredVideoPolicyOnCreateAndUpdate(t *testing.T) {
	svc, _ := setupTestProjectService(t)
	svc.SetVideoCatalog(VideoModelCatalog{
		"configured-video": {
			Key:                  "configured-video",
			ModelID:              "provider-configured-video",
			SupportedResolutions: []string{"720p"},
			SupportedRatios:      []string{"9:16"},
			MinDuration:          1,
			MaxDuration:          15,
			NoInputPricePerSecond: map[string]float64{
				"720p": 1,
			},
		},
	})

	create := &model.Project{
		Platform: model.PlatformVideo,
		Name:     "Video",
	}
	create.SetVideoDefaults(model.VideoDefaults{
		Purpose:    VideoPurposePlanting,
		ModelKey:   "missing-video",
		Resolution: "720p",
		Ratio:      "9:16",
		Duration:   5,
		Preflight:  true,
	})
	create.SetVideoModelPolicy(model.VideoModelPolicy{
		AllowedModels: []string{"missing-video"},
		DefaultModel:  "missing-video",
		MaxResolution: "720p",
		MaxDuration:   15,
	})
	if _, err := svc.Create(context.Background(), "user-1", create); err == nil || !strings.Contains(err.Error(), "模型未配置或不可用") {
		t.Fatalf("Create error = %v, want unconfigured model rejection", err)
	}

	ok := &model.Project{
		Platform: model.PlatformVideo,
		Name:     "Video",
	}
	ok.SetVideoDefaults(model.VideoDefaults{
		Purpose:    VideoPurposePlanting,
		ModelKey:   "configured-video",
		Resolution: "720p",
		Ratio:      "9:16",
		Duration:   5,
		Preflight:  true,
	})
	ok.SetVideoModelPolicy(model.VideoModelPolicy{
		AllowedModels: []string{"configured-video"},
		DefaultModel:  "configured-video",
		MaxResolution: "720p",
		MaxDuration:   15,
	})
	created, err := svc.Create(context.Background(), "user-1", ok)
	if err != nil {
		t.Fatalf("Create configured project: %v", err)
	}

	update := &model.Project{}
	update.SetVideoDefaults(model.VideoDefaults{
		Purpose:    VideoPurposePlanting,
		ModelKey:   "missing-video",
		Resolution: "720p",
		Ratio:      "9:16",
		Duration:   5,
		Preflight:  true,
	})
	update.SetVideoModelPolicy(model.VideoModelPolicy{
		AllowedModels: []string{"configured-video", "missing-video"},
		DefaultModel:  "missing-video",
		MaxResolution: "720p",
		MaxDuration:   15,
	})
	update.VideoProfileSet = true
	if _, err := svc.Update(context.Background(), "user-1", created.ID, update); err == nil || !strings.Contains(err.Error(), "模型未配置或不可用") {
		t.Fatalf("Update error = %v, want unconfigured model rejection", err)
	}
}

package service

import (
	"context"
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

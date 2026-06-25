package service

import (
	"context"
	"testing"

	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/app/writer"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
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

func TestProjectServiceDefaultsArticleStyle(t *testing.T) {
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
			// The writer dimension (写作风格) defaults to the platform writer;
			// the visual dimension (图片视觉) must stay empty — it must NEVER
			// be seeded with a writer key (the dan-koe → Victorian-woodcut bug).
			if ch.WritingStyle != writer.DefaultStyleName {
				t.Fatalf("WritingStyle = %q, want %q", ch.WritingStyle, writer.DefaultStyleName)
			}
			if ch.Style != "" {
				t.Fatalf("Style (visual) = %q, want empty — writer key must not leak into visual style", ch.Style)
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
	if ch.Style != "" {
		t.Fatalf("Style = %q, want empty", ch.Style)
	}
}

func TestProjectServiceUpdateDefaultsArticleStyle(t *testing.T) {
	svc, _ := setupTestProjectService(t)
	created, err := svc.Create(context.Background(), "user-1", &model.Project{
		Platform: model.PlatformArticle,
		Name:     "Article Project",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := svc.Update(context.Background(), "user-1", created.ID, &model.Project{
		Platform: model.PlatformArticle,
		Name:     "Article Project",
		Style:    "",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	// Update re-applies defaults: the writer dimension is defaulted, the visual
	// dimension stays empty.
	if updated.WritingStyle != writer.DefaultStyleName {
		t.Fatalf("WritingStyle = %q, want %q", updated.WritingStyle, writer.DefaultStyleName)
	}
	if updated.Style != "" {
		t.Fatalf("Style (visual) = %q, want empty", updated.Style)
	}
}

func TestProjectServiceUpdatePreservesExistingArticleStyleWhenStyleOmitted(t *testing.T) {
	svc, _ := setupTestProjectService(t)
	created, err := svc.Create(context.Background(), "user-1", &model.Project{
		Platform: model.PlatformArticle,
		Name:     "Article Project",
		Style:    "casual-science",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := svc.Update(context.Background(), "user-1", created.ID, &model.Project{
		Platform: model.PlatformArticle,
		Name:     "Renamed Article Project",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Style != "casual-science" {
		t.Fatalf("Style = %q, want casual-science", updated.Style)
	}
}

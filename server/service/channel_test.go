package service

import (
	"context"
	"testing"

	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/app/writer"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
)

func setupTestChannelService(t *testing.T) (*ChannelService, repository.Repository) {
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
	return NewChannelService(repo, &logger), repo
}

func TestChannelServiceDefaultsArticleAndXLSStyle(t *testing.T) {
	for _, platform := range []string{model.PlatformArticle, model.PlatformXLS} {
		t.Run(platform, func(t *testing.T) {
			svc, _ := setupTestChannelService(t)
			ch, err := svc.Create(context.Background(), "user-1", &model.Channel{
				Platform: platform,
				Name:     "Default Style Channel",
			})
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			if ch.Style != writer.DefaultStyleName {
				t.Fatalf("Style = %q, want %q", ch.Style, writer.DefaultStyleName)
			}
		})
	}
}

func TestChannelServiceDoesNotDefaultSeednoteStyle(t *testing.T) {
	svc, _ := setupTestChannelService(t)
	ch, err := svc.Create(context.Background(), "user-1", &model.Channel{
		Platform: model.PlatformSeednote,
		Name:     "Seednote Channel",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ch.Style != "" {
		t.Fatalf("Style = %q, want empty", ch.Style)
	}
}

func TestChannelServiceUpdateDefaultsArticleStyle(t *testing.T) {
	svc, _ := setupTestChannelService(t)
	created, err := svc.Create(context.Background(), "user-1", &model.Channel{
		Platform: model.PlatformArticle,
		Name:     "Article Channel",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := svc.Update(context.Background(), "user-1", created.ID, &model.Channel{
		Platform: model.PlatformArticle,
		Name:     "Article Channel",
		Style:    "",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Style != writer.DefaultStyleName {
		t.Fatalf("Style = %q, want %q", updated.Style, writer.DefaultStyleName)
	}
}

func TestChannelServiceUpdatePreservesExistingArticleStyleWhenStyleOmitted(t *testing.T) {
	svc, _ := setupTestChannelService(t)
	created, err := svc.Create(context.Background(), "user-1", &model.Channel{
		Platform: model.PlatformArticle,
		Name:     "Article Channel",
		Style:    "casual-science",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := svc.Update(context.Background(), "user-1", created.ID, &model.Channel{
		Platform: model.PlatformArticle,
		Name:     "Renamed Article Channel",
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Style != "casual-science" {
		t.Fatalf("Style = %q, want casual-science", updated.Style)
	}
}

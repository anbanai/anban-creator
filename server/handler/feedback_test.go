package handler

import (
	"testing"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/royalrick/anbanwriter/server/service"
)

func setupFeedbackTest(t *testing.T) (*FeedbackHandler, repository.Repository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&model.Feedback{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	repo := repository.New(db)
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	svc := service.NewFeedbackService(repo, &logger)
	return NewFeedbackHandler(svc, &logger), repo
}

func TestFeedbackService_Create(t *testing.T) {
	_, repo := setupFeedbackTest(t)
	ctx := t.Context()

	t.Run("valid bug feedback", func(t *testing.T) {
		logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
		svc := service.NewFeedbackService(repo, &logger)

		fb, err := svc.Create(ctx, "user-1", model.FeedbackTypeBug, "Something is broken")
		if err != nil {
			t.Fatalf("create feedback: %v", err)
		}
		if fb.ID == "" {
			t.Error("expected non-empty ID")
		}
		if fb.UserID != "user-1" {
			t.Errorf("expected user_id 'user-1', got %q", fb.UserID)
		}
		if fb.Type != model.FeedbackTypeBug {
			t.Errorf("expected type 'bug', got %q", fb.Type)
		}
	})

	t.Run("valid suggestion feedback", func(t *testing.T) {
		logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
		svc := service.NewFeedbackService(repo, &logger)

		fb, err := svc.Create(ctx, "user-1", model.FeedbackTypeSuggestion, "Add dark mode")
		if err != nil {
			t.Fatalf("create feedback: %v", err)
		}
		if fb.Type != model.FeedbackTypeSuggestion {
			t.Errorf("expected type 'suggestion', got %q", fb.Type)
		}
	})

	t.Run("empty content rejected", func(t *testing.T) {
		logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
		svc := service.NewFeedbackService(repo, &logger)

		_, err := svc.Create(ctx, "user-1", model.FeedbackTypeBug, "")
		if err == nil {
			t.Error("expected error for empty content")
		}
	})

	t.Run("invalid type rejected", func(t *testing.T) {
		logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
		svc := service.NewFeedbackService(repo, &logger)

		_, err := svc.Create(ctx, "user-1", "invalid", "test content")
		if err == nil {
			t.Error("expected error for invalid type")
		}
	})
}

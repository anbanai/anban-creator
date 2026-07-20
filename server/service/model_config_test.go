package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"

	appconfig "github.com/anbanai/anban-creator/app/config"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

func TestPresetToImageAPIConfigUsesPresetTimeout(t *testing.T) {
	base := &srvconfig.Config{ImageAPI: srvconfig.ImageAPIConfig{
		Cover:   &appconfig.ImageAPI{TimeoutSec: 30},
		Content: &appconfig.ImageAPI{TimeoutSec: 45},
	}}
	preset := &srvconfig.ImageModelPreset{
		Provider: "volcengine", Model: "seedream", Timeout: 2 * time.Minute,
	}

	got := presetToImageAPIConfig(preset, base)
	if got == nil || got.Cover.TimeoutSec != 120 || got.Content.TimeoutSec != 120 {
		t.Fatalf("preset runtime timeouts = %#v/%#v, want 120/120", got.Cover, got.Content)
	}
}

func setupTestModelConfigService(t *testing.T) (*ModelConfigService, repository.Repository) {
	t.Helper()
	db := setupTestDB(t)
	if err := db.AutoMigrate(&model.UserModelConfig{}); err != nil {
		t.Fatalf("migrate model config: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	cfg := &srvconfig.Config{
		ImageAPI: srvconfig.ImageAPIConfig{
			Cover: &appconfig.ImageAPI{
				Provider: "gemini",
				Key:      "system-key",
				BaseURL:  "https://system.example",
				Model:    "system-image-model",
			},
			Content: &appconfig.ImageAPI{
				Provider: "gemini",
				Key:      "system-key",
				BaseURL:  "https://system.example",
				Model:    "system-image-model",
			},
		},
	}
	return NewModelConfigService(repo, cfg, &logger), repo
}

func TestModelConfigIncompleteImageOverrideDoesNotOverrideSystemDefault(t *testing.T) {
	svc, _ := setupTestModelConfigService(t)
	ctx := context.Background()
	userID := "user-incomplete-image"

	err := svc.Update(ctx, userID, &UpdateModelConfigRequest{
		Image: &ImageConfigDTO{
			Endpoint: "https://custom.example/v1",
			Model:    "custom-image-model",
		},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	cfg := svc.GetEffectiveImageConfig(ctx, userID)
	if cfg != nil {
		t.Fatalf("GetEffectiveImageConfig = %+v, want nil so caller uses system default", cfg)
	}
	if svc.HasCompleteImageOverride(ctx, userID) {
		t.Fatal("HasCompleteImageOverride = true, want false")
	}
}

func TestModelConfigCompleteImageOverrideApplies(t *testing.T) {
	svc, _ := setupTestModelConfigService(t)
	ctx := context.Background()
	userID := "user-complete-image"

	err := svc.Update(ctx, userID, &UpdateModelConfigRequest{
		Image: &ImageConfigDTO{
			Provider: "openai",
			Endpoint: "https://custom.example/v1",
			APIKey:   "custom-key",
			Model:    "gpt-image-2",
		},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	cfg := svc.GetEffectiveImageConfig(ctx, userID)
	if cfg == nil {
		t.Fatal("GetEffectiveImageConfig returned nil")
	}
	if cfg.Cover.Provider != "openai" || cfg.Cover.Key != "custom-key" || cfg.Cover.Model != "gpt-image-2" {
		t.Fatalf("effective config = %+v, want complete user override", cfg.Cover)
	}
	if !svc.HasCompleteImageOverride(ctx, userID) {
		t.Fatal("HasCompleteImageOverride = false, want true")
	}
}

func TestModelConfigRejectsImageKeepExistingKeyWithoutExistingKey(t *testing.T) {
	svc, _ := setupTestModelConfigService(t)
	ctx := context.Background()

	err := svc.Update(ctx, "user-no-existing-key", &UpdateModelConfigRequest{
		Image: &ImageConfigDTO{
			Provider: "openai",
			Endpoint: "https://custom.example/v1",
			APIKey:   "****",
			Model:    "gpt-image-2",
		},
	})
	if !errors.Is(err, ErrInvalidModelConfig) {
		t.Fatalf("Update error = %v, want ErrInvalidModelConfig", err)
	}
}

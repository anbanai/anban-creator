package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

func TestImageCapabilityRuntimeUsesConfiguredTimeout(t *testing.T) {
	route := srvconfig.ImageGenerationRouteConfig{Provider: "volcengine", Model: "image-v1", BaseURL: "https://images.example.com", APIKey: "secret", Timeout: 120000000000, Enabled: true}
	base := &srvconfig.Config{ModelRoutes: srvconfig.ModelRoutesConfig{ImageGeneration: srvconfig.ImageGenerationRoutesConfig{DefaultCapability: "standard", Capabilities: map[string]srvconfig.ImageGenerationRouteConfig{"standard": route}}}}
	got, ok := base.ImageAPIForCapability("standard")
	if !ok || got.Cover.TimeoutSec != 120 || got.Content.TimeoutSec != 120 {
		t.Fatalf("capability runtime timeouts = %#v, want 120/120", got)
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
		ModelRoutes: srvconfig.ModelRoutesConfig{ImageGeneration: srvconfig.ImageGenerationRoutesConfig{DefaultCapability: "standard", Capabilities: map[string]srvconfig.ImageGenerationRouteConfig{
			"standard": {Provider: "gemini", Model: "system-image-model", BaseURL: "https://system.example", APIKey: "system-key", Enabled: true, MinTier: "free", BillingSKU: "image.standard"},
		}}},
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

func TestModelConfigResponseContainsOnlyImageOverride(t *testing.T) {
	svc, _ := setupTestModelConfigService(t)
	ctx := context.Background()
	userID := "user-image-only"
	if err := svc.Update(ctx, userID, &UpdateModelConfigRequest{Image: &ImageConfigDTO{
		Provider: "openai", Endpoint: "https://custom.example/v1", APIKey: "custom-key", Model: "gpt-image-2",
	}}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Image == nil || got.Image.Provider != "openai" || !strings.HasPrefix(got.Image.APIKey, keepExistingKey) || got.Image.APIKey == "custom-key" {
		t.Fatalf("response = %#v", got)
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

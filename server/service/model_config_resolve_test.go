package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"gorm.io/gorm"

	appconfig "github.com/anbanai/anban-creator/app/config"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

// setupResolveTestService creates a ModelConfigService with image-type-specific
// system routes plus free/pro presets for config and generation resolution tests.
func setupResolveTestService(t *testing.T) (*ModelConfigService, repository.Repository, *gorm.DB) {
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
				Provider: "openai",
				Key:      "system-key",
				BaseURL:  "https://system.example",
				Model:    "gpt-image-2",
			},
			Content: &appconfig.ImageAPI{
				Provider: "volcengine",
				Key:      "system-key",
				BaseURL:  "https://system.example",
				Model:    "doubao-seedream",
			},
		},
		ModelRoutes: srvconfig.ModelRoutesConfig{
			ImageGeneration: srvconfig.ImageGenerationRoutesConfig{
				Designer: map[string]srvconfig.ImageGenerationRouteConfig{
					"seedream": {
						Provider:     "volcengine_ark",
						Model:        "doubao-seedream",
						QualityRank:  100,
						Capabilities: srvconfig.DesignerProviderCapabilities{SupportsReference: true, MaxReferenceImages: 10},
					},
					"gpt_image_2": {
						Provider:     "wangcai_openai",
						Model:        "gpt-image-2",
						QualityRank:  200,
						Capabilities: srvconfig.DesignerProviderCapabilities{SupportsReference: true, MaxReferenceImages: 16},
					},
				},
			},
		},
		ImagePresets: []srvconfig.ImageModelPreset{
			{
				Key:         "volcengine-standard",
				DisplayName: "Volcengine Standard",
				Provider:    "volcengine",
				Model:       "doubao-seedream",
				Endpoint:    "https://ark.volces.com",
				APIKey:      "volc-key",
				MinTier:     "free",
				QualityRank: 100,
				Capabilities: srvconfig.DesignerProviderCapabilities{
					SupportsReference:  true,
					MaxReferenceImages: 10,
				},
			},
			{
				Key:         "gemini-pro",
				DisplayName: "Gemini Pro",
				Provider:    "gemini",
				Model:       "gemini-3-pro-image-preview",
				Endpoint:    "https://generativelanguage.googleapis.com",
				APIKey:      "gemini-key",
				MinTier:     "pro",
				QualityRank: 150,
				Capabilities: srvconfig.DesignerProviderCapabilities{
					SupportsReference:  true,
					MaxReferenceImages: 4,
				},
			},
			{
				Key:         "openai-standard",
				DisplayName: "OpenAI Standard",
				Provider:    "openai",
				Model:       "gpt-image-2",
				Endpoint:    "https://openai.example/v1",
				APIKey:      "openai-key",
				MinTier:     "pro",
				QualityRank: 200,
				Capabilities: srvconfig.DesignerProviderCapabilities{
					SupportsReference:  true,
					MaxReferenceImages: 16,
				},
			},
		},
	}
	return NewModelConfigService(repo, cfg, &logger), repo, db
}

func TestResolveImageConfigForKey(t *testing.T) {
	svc, repo, _ := setupResolveTestService(t)
	ctx := context.Background()

	freeUser := "user-free"
	proUser := "user-pro"
	enterpriseUser := "user-enterprise"
	enterpriseUserWithOverride := "user-enterprise-override"
	nonexistentUser := "user-nonexistent"

	for _, u := range []struct {
		id   string
		tier model.Tier
	}{
		{freeUser, model.TierFree},
		{proUser, model.TierPro},
		{enterpriseUser, model.TierEnterprise},
		{enterpriseUserWithOverride, model.TierEnterprise},
	} {
		if err := repo.Users().Create(ctx, &model.User{
			ID:         u.id,
			Email:      u.id + "@example.com",
			Nickname:   u.id,
			Password:   "hashed",
			Tier:       u.tier,
			InviteCode: u.id + "-invite",
		}); err != nil {
			t.Fatalf("create %s: %v", u.id, err)
		}
	}

	// Give enterpriseUserWithOverride a complete image config override.
	if err := svc.Update(ctx, enterpriseUserWithOverride, &UpdateModelConfigRequest{
		Image: &ImageConfigDTO{
			Provider: "openai",
			Endpoint: "https://custom.example/v1",
			APIKey:   "user-own-key",
			Model:    "gpt-image-2",
		},
	}); err != nil {
		t.Fatalf("set override: %v", err)
	}

	tests := []struct {
		name       string
		userID     string
		key        string
		wantSource string
		nilCfg     bool
		provider   string
		model      string
		apiKey     string
	}{
		{
			name:       "empty key returns system default (no user override)",
			userID:     freeUser,
			key:        "",
			wantSource: "system_default",
			nilCfg:     true,
		},
		{
			name:       "empty key returns user_custom when override exists",
			userID:     enterpriseUserWithOverride,
			key:        "",
			wantSource: "user_custom",
			provider:   "openai",
			model:      "gpt-image-2",
			apiKey:     "user-own-key",
		},
		{
			name:       "custom key with override + Enterprise tier uses override",
			userID:     enterpriseUserWithOverride,
			key:        model.ImageModelKeyCustom,
			wantSource: "user_custom",
			provider:   "openai",
			model:      "gpt-image-2",
			apiKey:     "user-own-key",
		},
		{
			name:       "custom key without override falls back to system default",
			userID:     enterpriseUser,
			key:        model.ImageModelKeyCustom,
			wantSource: "system_default",
			nilCfg:     true,
		},
		{
			name:       "custom key for non-Enterprise tier falls back to system default",
			userID:     proUser,
			key:        model.ImageModelKeyCustom,
			wantSource: "system_default",
			nilCfg:     true,
		},
		{
			name:       "preset key satisfied by tier uses preset config",
			userID:     freeUser,
			key:        "volcengine-standard",
			wantSource: "preset:volcengine-standard",
			provider:   "volcengine",
			model:      "doubao-seedream",
			apiKey:     "volc-key",
		},
		{
			name:       "preset key at exact tier uses preset config",
			userID:     proUser,
			key:        "gemini-pro",
			wantSource: "preset:gemini-pro",
			provider:   "gemini",
			model:      "gemini-3-pro-image-preview",
			apiKey:     "gemini-key",
		},
		{
			name:       "preset key tier not satisfied (free wants gemini) falls back",
			userID:     freeUser,
			key:        "gemini-pro",
			wantSource: "system_default",
			nilCfg:     true,
		},
		{
			name:       "unknown preset key falls back",
			userID:     enterpriseUser,
			key:        "does-not-exist",
			wantSource: "system_default",
			nilCfg:     true,
		},
		{
			name:       "nonexistent user: preset free key still resolves (tier defaults Free)",
			userID:     nonexistentUser,
			key:        "volcengine-standard",
			wantSource: "preset:volcengine-standard",
			provider:   "volcengine",
			model:      "doubao-seedream",
			apiKey:     "volc-key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, source := svc.ResolveImageConfigForKey(ctx, tt.userID, tt.key)
			if source != tt.wantSource {
				t.Fatalf("source = %q, want %q", source, tt.wantSource)
			}
			if tt.nilCfg {
				if cfg != nil {
					t.Fatalf("expected nil cfg for system_default fallback, got %+v", cfg.Cover)
				}
				return
			}
			if cfg == nil {
				t.Fatalf("expected non-nil cfg for source %q", tt.wantSource)
			}
			if cfg.Cover.Provider != tt.provider {
				t.Errorf("Cover.Provider = %q, want %q", cfg.Cover.Provider, tt.provider)
			}
			if cfg.Cover.Model != tt.model {
				t.Errorf("Cover.Model = %q, want %q", cfg.Cover.Model, tt.model)
			}
			if cfg.Cover.Key != tt.apiKey {
				t.Errorf("Cover.Key = %q, want %q", cfg.Cover.Key, tt.apiKey)
			}
			if cfg.Content.Provider != tt.provider || cfg.Content.Model != tt.model {
				t.Errorf("Content not mirrored: %+v", cfg.Content)
			}
		})
	}
}

func TestResolveImageConfigForTaskKeyStrictFailures(t *testing.T) {
	svc, repo, _ := setupResolveTestService(t)
	ctx := context.Background()

	freeUser := "strict-free"
	enterpriseUser := "strict-enterprise"
	for _, u := range []struct {
		id   string
		tier model.Tier
	}{
		{freeUser, model.TierFree},
		{enterpriseUser, model.TierEnterprise},
	} {
		if err := repo.Users().Create(ctx, &model.User{
			ID:         u.id,
			Email:      u.id + "@example.com",
			Nickname:   u.id,
			Password:   "hashed",
			Tier:       u.tier,
			InviteCode: u.id + "-invite",
		}); err != nil {
			t.Fatalf("create %s: %v", u.id, err)
		}
	}

	tests := []struct {
		name    string
		userID  string
		key     string
		wantErr string
	}{
		{
			name:    "unknown preset fails instead of falling back",
			userID:  enterpriseUser,
			key:     "seedream-4.0",
			wantErr: "unknown image model key",
		},
		{
			name:    "tier downgrade fails instead of falling back",
			userID:  freeUser,
			key:     "gemini-pro",
			wantErr: "not allowed for user tier",
		},
		{
			name:    "custom without override fails instead of falling back",
			userID:  enterpriseUser,
			key:     model.ImageModelKeyCustom,
			wantErr: "custom image model is not configured",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, source, err := svc.ResolveImageConfigForTaskKey(ctx, tt.userID, tt.key)
			if err == nil {
				t.Fatalf("expected error, got cfg=%+v source=%q", cfg, source)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want to contain %q", err, tt.wantErr)
			}
		})
	}
}

func createResolveTestUser(t *testing.T, repo repository.Repository, id string, tier model.Tier) {
	t.Helper()
	if err := repo.Users().Create(context.Background(), &model.User{
		ID:         id,
		Email:      id + "@example.com",
		Nickname:   id,
		Password:   "hashed",
		Tier:       tier,
		InviteCode: id + "-invite",
	}); err != nil {
		t.Fatalf("create %s: %v", id, err)
	}
}

func TestResolveImageModelForGenerationRetainsPreferredWithoutReferences(t *testing.T) {
	svc, repo, _ := setupResolveTestService(t)
	createResolveTestUser(t, repo, "generation-free", model.TierFree)

	resolved, err := svc.ResolveImageModelForGeneration(
		context.Background(), "generation-free", "volcengine-standard", "content", 0,
	)
	if err != nil {
		t.Fatalf("ResolveImageModelForGeneration() error = %v", err)
	}
	if resolved.Key != "volcengine-standard" || resolved.Provider != "volcengine" || resolved.Model != "doubao-seedream" {
		t.Fatalf("resolved preferred model = %#v", resolved)
	}
	if resolved.SelectionReason != "preferred" {
		t.Fatalf("SelectionReason = %q, want preferred", resolved.SelectionReason)
	}
}

func TestResolveImageModelForGenerationUsesImageTypeCapabilities(t *testing.T) {
	svc, repo, _ := setupResolveTestService(t)
	createResolveTestUser(t, repo, "generation-pro", model.TierPro)
	ctx := context.Background()

	cover, err := svc.ResolveImageModelForGeneration(ctx, "generation-pro", "", "cover", 11)
	if err != nil {
		t.Fatalf("cover resolution error = %v", err)
	}
	if cover.Source != "system_default" || cover.Provider != "openai" || cover.Model != "gpt-image-2" {
		t.Fatalf("cover resolution = %#v, want preferred cover slot", cover)
	}

	content, err := svc.ResolveImageModelForGeneration(ctx, "generation-pro", "", "content", 11)
	if err != nil {
		t.Fatalf("content resolution error = %v", err)
	}
	if content.Source != "preset:openai-standard" || content.SelectionReason != "reference_compatible_fallback" {
		t.Fatalf("content resolution = %#v, want compatible fallback", content)
	}
}

func TestResolveImageModelForGenerationChoosesHighestQualityCompatiblePreset(t *testing.T) {
	svc, repo, _ := setupResolveTestService(t)
	createResolveTestUser(t, repo, "quality-pro", model.TierPro)

	resolved, err := svc.ResolveImageModelForGeneration(
		context.Background(), "quality-pro", "volcengine-standard", "content", 11,
	)
	if err != nil {
		t.Fatalf("ResolveImageModelForGeneration() error = %v", err)
	}
	if resolved.Key != "openai-standard" || resolved.SelectionReason != "reference_compatible_fallback" {
		t.Fatalf("resolved fallback = %#v", resolved)
	}
}

func TestResolveImageModelForGenerationFiltersByTierAndReturnsAccessibleLimit(t *testing.T) {
	svc, repo, _ := setupResolveTestService(t)
	createResolveTestUser(t, repo, "limit-free", model.TierFree)

	_, err := svc.ResolveImageModelForGeneration(
		context.Background(), "limit-free", "volcengine-standard", "content", 11,
	)
	var limitErr *ImageReferenceLimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("error = %v, want ImageReferenceLimitError", err)
	}
	if limitErr.Requested != 11 || limitErr.MaxReferenceImages != 10 {
		t.Fatalf("limit error = %#v, want requested=11 max=10", limitErr)
	}
}

func TestResolveImageModelForGenerationReturnsMaximumAccessibleLimit(t *testing.T) {
	svc, repo, _ := setupResolveTestService(t)
	createResolveTestUser(t, repo, "limit-pro", model.TierPro)

	_, err := svc.ResolveImageModelForGeneration(
		context.Background(), "limit-pro", "volcengine-standard", "content", 17,
	)
	var limitErr *ImageReferenceLimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("error = %v, want ImageReferenceLimitError", err)
	}
	if limitErr.MaxReferenceImages != 16 {
		t.Fatalf("MaxReferenceImages = %d, want 16", limitErr.MaxReferenceImages)
	}
}

func TestResolveImageModelForGenerationTreatsUnknownCustomCapabilitiesConservatively(t *testing.T) {
	svc, repo, _ := setupResolveTestService(t)
	userID := "custom-enterprise"
	createResolveTestUser(t, repo, userID, model.TierEnterprise)
	if err := svc.Update(context.Background(), userID, &UpdateModelConfigRequest{
		Image: &ImageConfigDTO{
			Provider: "custom-provider",
			Endpoint: "https://custom.example/v1",
			APIKey:   "custom-key",
			Model:    "unknown-image-model",
		},
	}); err != nil {
		t.Fatalf("set custom override: %v", err)
	}

	preferred, err := svc.ResolveImageModelForGeneration(
		context.Background(), userID, model.ImageModelKeyCustom, "content", 0,
	)
	if err != nil {
		t.Fatalf("zero-reference custom resolution error = %v", err)
	}
	if preferred.SupportsReference || preferred.MaxReferenceImages != 0 {
		t.Fatalf("unknown custom capabilities = %#v, want conservative zero support", preferred)
	}

	fallback, err := svc.ResolveImageModelForGeneration(
		context.Background(), userID, model.ImageModelKeyCustom, "content", 1,
	)
	if err != nil {
		t.Fatalf("reference custom resolution error = %v", err)
	}
	if fallback.Key != "openai-standard" || fallback.SelectionReason != "reference_compatible_fallback" {
		t.Fatalf("custom fallback = %#v", fallback)
	}
}

func TestResolveImageModelForGenerationBreaksEqualRankTiesByPresetKey(t *testing.T) {
	svc, repo, _ := setupResolveTestService(t)
	createResolveTestUser(t, repo, "tie-pro", model.TierPro)
	svc.cfg.ImagePresets = append(svc.cfg.ImagePresets,
		srvconfig.ImageModelPreset{
			Key: "z-compatible", Provider: "openai", Model: "z-model", Endpoint: "https://z.example", APIKey: "z", MinTier: "pro",
			QualityRank: 300, Capabilities: srvconfig.DesignerProviderCapabilities{SupportsReference: true, MaxReferenceImages: 16},
		},
		srvconfig.ImageModelPreset{
			Key: "a-compatible", Provider: "openai", Model: "a-model", Endpoint: "https://a.example", APIKey: "a", MinTier: "pro",
			QualityRank: 300, Capabilities: srvconfig.DesignerProviderCapabilities{SupportsReference: true, MaxReferenceImages: 16},
		},
	)

	resolved, err := svc.ResolveImageModelForGeneration(
		context.Background(), "tie-pro", "volcengine-standard", "content", 11,
	)
	if err != nil {
		t.Fatalf("ResolveImageModelForGeneration() error = %v", err)
	}
	if resolved.Key != "a-compatible" {
		t.Fatalf("resolved key = %q, want a-compatible", resolved.Key)
	}
}

func TestResolveImageModelForGenerationRejectsUnsupportedImageType(t *testing.T) {
	svc, repo, _ := setupResolveTestService(t)
	createResolveTestUser(t, repo, "invalid-type-free", model.TierFree)

	_, err := svc.ResolveImageModelForGeneration(
		context.Background(), "invalid-type-free", "volcengine-standard", "thumbnail", 0,
	)
	if err == nil || !strings.Contains(err.Error(), "unsupported image type") {
		t.Fatalf("error = %v, want unsupported image type", err)
	}
}

func TestResolveImageModelForGenerationReturnsNoCapableModelError(t *testing.T) {
	svc, repo, _ := setupResolveTestService(t)
	createResolveTestUser(t, repo, "no-capability-free", model.TierFree)
	svc.cfg.ModelRoutes.ImageGeneration.Designer = nil
	svc.cfg.ImagePresets = nil

	_, err := svc.ResolveImageModelForGeneration(
		context.Background(), "no-capability-free", "", "content", 1,
	)
	if err == nil || !strings.Contains(err.Error(), "no accessible image model supports reference images") {
		t.Fatalf("error = %v, want no-capable-model error", err)
	}
}

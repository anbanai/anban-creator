package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"gorm.io/gorm"

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
		ModelRoutes: srvconfig.ModelRoutesConfig{
			ImageGeneration: srvconfig.ImageGenerationRoutesConfig{
				DefaultCapability: "standard",
				Capabilities: map[string]srvconfig.ImageGenerationRouteConfig{
					"standard": {
						Provider: "volcengine", Model: "doubao-seedream", BaseURL: "https://ark.volces.com", APIKey: "volc-key",
						MinTier: "free", BillingSKU: "image.standard", Enabled: true, QualityRank: 100,
						Features: srvconfig.DesignerProviderCapabilities{SupportsReference: true, MaxReferenceImages: 10},
					},
					"gemini-pro": {
						Provider: "gemini", Model: "gemini-3-pro-image-preview", BaseURL: "https://generativelanguage.googleapis.com", APIKey: "gemini-key",
						MinTier: "pro", BillingSKU: "image.professional", Enabled: true, QualityRank: 150,
						Features: srvconfig.DesignerProviderCapabilities{SupportsReference: true, MaxReferenceImages: 4},
					},
					"professional": {
						Provider: "openai", Model: "gpt-image-2", BaseURL: "https://openai.example/v1", APIKey: "openai-key",
						MinTier: "pro", BillingSKU: "image.professional", Enabled: true, QualityRank: 200,
						Features: srvconfig.DesignerProviderCapabilities{SupportsReference: true, MaxReferenceImages: 16},
					},
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
			key:        "standard",
			wantSource: "capability:standard",
			provider:   "volcengine",
			model:      "doubao-seedream",
			apiKey:     "volc-key",
		},
		{
			name:       "preset key at exact tier uses preset config",
			userID:     proUser,
			key:        "gemini-pro",
			wantSource: "capability:gemini-pro",
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
			key:        "standard",
			wantSource: "capability:standard",
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
		context.Background(), "generation-free", "standard", "content", 0,
	)
	if err != nil {
		t.Fatalf("ResolveImageModelForGeneration() error = %v", err)
	}
	if resolved.Key != "standard" || resolved.Provider != "volcengine" || resolved.Model != "doubao-seedream" {
		t.Fatalf("resolved preferred model = %#v", resolved)
	}
	if resolved.SelectionReason != "preferred" {
		t.Fatalf("SelectionReason = %q, want preferred", resolved.SelectionReason)
	}
}

func TestResolveImageModelForGenerationUsesUnifiedCapabilityFeatures(t *testing.T) {
	svc, repo, _ := setupResolveTestService(t)
	createResolveTestUser(t, repo, "generation-pro", model.TierPro)
	ctx := context.Background()

	for _, imageType := range []string{"cover", "content"} {
		_, err := svc.ResolveImageModelForGeneration(ctx, "generation-pro", "", imageType, 11)
		var limitErr *ImageReferenceLimitError
		if !errors.As(err, &limitErr) || limitErr.MaxReferenceImages != 10 {
			t.Fatalf("%s error = %v, want selected standard limit 10", imageType, err)
		}
	}
}

func TestResolveImageModelForGenerationDoesNotSwitchCapabilityForReferences(t *testing.T) {
	svc, repo, _ := setupResolveTestService(t)
	createResolveTestUser(t, repo, "quality-pro", model.TierPro)

	_, err := svc.ResolveImageModelForGeneration(
		context.Background(), "quality-pro", "standard", "content", 11,
	)
	var limitErr *ImageReferenceLimitError
	if !errors.As(err, &limitErr) || limitErr.MaxReferenceImages != 10 {
		t.Fatalf("error = %v, want exact selected capability limit", err)
	}
}

func TestResolveImageModelForGenerationFiltersByTierAndReturnsAccessibleLimit(t *testing.T) {
	svc, repo, _ := setupResolveTestService(t)
	createResolveTestUser(t, repo, "limit-free", model.TierFree)

	_, err := svc.ResolveImageModelForGeneration(
		context.Background(), "limit-free", "standard", "content", 11,
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
		context.Background(), "limit-pro", "standard", "content", 17,
	)
	var limitErr *ImageReferenceLimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("error = %v, want ImageReferenceLimitError", err)
	}
	if limitErr.MaxReferenceImages != 10 {
		t.Fatalf("MaxReferenceImages = %d, want selected capability limit 10", limitErr.MaxReferenceImages)
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

	_, err = svc.ResolveImageModelForGeneration(
		context.Background(), userID, model.ImageModelKeyCustom, "content", 1,
	)
	if err == nil || !strings.Contains(err.Error(), "does not support reference images") {
		t.Fatalf("custom reference error = %v, want unsupported without fallback", err)
	}
}

func TestResolveImageModelForGenerationIgnoresOtherEqualRankCapabilities(t *testing.T) {
	svc, repo, _ := setupResolveTestService(t)
	createResolveTestUser(t, repo, "tie-pro", model.TierPro)
	svc.cfg.ModelRoutes.ImageGeneration.Capabilities["z-compatible"] = srvconfig.ImageGenerationRouteConfig{Provider: "openai", Model: "z-model", BaseURL: "https://z.example", APIKey: "z", MinTier: "pro", Enabled: true, QualityRank: 300, Features: srvconfig.DesignerProviderCapabilities{SupportsReference: true, MaxReferenceImages: 16}}
	svc.cfg.ModelRoutes.ImageGeneration.Capabilities["a-compatible"] = srvconfig.ImageGenerationRouteConfig{Provider: "openai", Model: "a-model", BaseURL: "https://a.example", APIKey: "a", MinTier: "pro", Enabled: true, QualityRank: 300, Features: srvconfig.DesignerProviderCapabilities{SupportsReference: true, MaxReferenceImages: 16}}

	_, err := svc.ResolveImageModelForGeneration(
		context.Background(), "tie-pro", "standard", "content", 11,
	)
	var limitErr *ImageReferenceLimitError
	if !errors.As(err, &limitErr) || limitErr.MaxReferenceImages != 10 {
		t.Fatalf("error = %v, want selected standard limit without rank fallback", err)
	}
}

func TestResolveImageModelForGenerationRejectsUnsupportedImageType(t *testing.T) {
	svc, repo, _ := setupResolveTestService(t)
	createResolveTestUser(t, repo, "invalid-type-free", model.TierFree)

	_, err := svc.ResolveImageModelForGeneration(
		context.Background(), "invalid-type-free", "standard", "thumbnail", 0,
	)
	if err == nil || !strings.Contains(err.Error(), "unsupported image type") {
		t.Fatalf("error = %v, want unsupported image type", err)
	}
}

func TestResolveImageModelForGenerationReturnsNoCapableModelError(t *testing.T) {
	svc, repo, _ := setupResolveTestService(t)
	createResolveTestUser(t, repo, "no-capability-free", model.TierFree)
	svc.cfg.ModelRoutes.ImageGeneration = srvconfig.ImageGenerationRoutesConfig{}

	_, err := svc.ResolveImageModelForGeneration(
		context.Background(), "no-capability-free", "", "content", 1,
	)
	if err == nil || !strings.Contains(err.Error(), "default image capability is unavailable") {
		t.Fatalf("error = %v, want unavailable default capability", err)
	}
}

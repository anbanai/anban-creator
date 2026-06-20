package service

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"gorm.io/gorm"

	appconfig "github.com/royalrick/anbanwriter/app/config"
	srvconfig "github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
)

// setupResolveTestService creates a ModelConfigService with two presets
// (volcengine-standard/free, gemini-pro/pro) for ResolveImageConfigForKey tests.
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
				Provider: "volcengine",
				Key:      "system-key",
				BaseURL:  "https://system.example",
				Model:    "system-image-model",
			},
			Content: &appconfig.ImageAPI{
				Provider: "volcengine",
				Key:      "system-key",
				BaseURL:  "https://system.example",
				Model:    "system-image-model",
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
			},
			{
				Key:         "gemini-pro",
				DisplayName: "Gemini Pro",
				Provider:    "gemini",
				Model:       "gemini-3-pro-image-preview",
				Endpoint:    "https://generativelanguage.googleapis.com",
				APIKey:      "gemini-key",
				MinTier:     "pro",
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

package handler

import (
	"testing"

	"github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/model"
)

func TestValidateImageModelKey(t *testing.T) {
	presets := []config.ImageModelPreset{
		{Key: "volcengine-standard", DisplayName: "Volcengine", Provider: "volcengine", Model: "doubao-seedream-5-0-260128", MinTier: "free"},
		{Key: "gemini-pro", DisplayName: "Gemini Pro", Provider: "gemini", Model: "gemini-3-pro-image-preview", MinTier: "pro"},
	}

	tests := []struct {
		name    string
		key     string
		tier    model.Tier
		wantErr bool
	}{
		{name: "empty key always allowed (free)", key: "", tier: model.TierFree, wantErr: false},
		{name: "empty key always allowed (enterprise)", key: "", tier: model.TierEnterprise, wantErr: false},
		{name: "free preset usable by free tier", key: "volcengine-standard", tier: model.TierFree, wantErr: false},
		{name: "free preset usable by pro tier", key: "volcengine-standard", tier: model.TierPro, wantErr: false},
		{name: "pro preset forbidden for free tier", key: "gemini-pro", tier: model.TierFree, wantErr: true},
		{name: "pro preset allowed for pro tier", key: "gemini-pro", tier: model.TierPro, wantErr: false},
		{name: "pro preset allowed for enterprise tier", key: "gemini-pro", tier: model.TierEnterprise, wantErr: false},
		{name: "custom forbidden for free tier", key: model.ImageModelKeyCustom, tier: model.TierFree, wantErr: true},
		{name: "custom forbidden for pro tier", key: model.ImageModelKeyCustom, tier: model.TierPro, wantErr: true},
		{name: "custom allowed for enterprise tier", key: model.ImageModelKeyCustom, tier: model.TierEnterprise, wantErr: false},
		{name: "unknown key rejected", key: "does-not-exist", tier: model.TierEnterprise, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateImageModelKey(tt.key, tt.tier, presets)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
		})
	}
}

func TestValidateImageModelKey_NormalizesInvalidMinTier(t *testing.T) {
	// A preset with garbage MinTier should be treated as free (default), not crash.
	presets := []config.ImageModelPreset{
		{Key: "weird", MinTier: "not-a-real-tier"},
	}
	if err := ValidateImageModelKey("weird", model.TierFree, presets); err != nil {
		t.Fatalf("preset with invalid min_tier should be usable by free tier, got: %v", err)
	}
}

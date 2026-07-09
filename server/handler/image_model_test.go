package handler

import (
	"context"
	"testing"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

func TestValidateImageModelKey(t *testing.T) {
	presets := []config.ImageModelPreset{
		{Key: "volcengine-standard", DisplayName: "Volcengine", Provider: "volcengine", Model: "doubao-seedream-5-0-pro-260628", MinTier: "free"},
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

// failClosedPresets has a single Pro-only model. Under fail-closed (tier resolves
// to Free) this key must be rejected — that is the contract these tests pin.
var failClosedPresets = []config.ImageModelPreset{
	{Key: "pro-only", DisplayName: "Pro Only", Provider: "volcengine", Model: "x", MinTier: "pro"},
}

func TestValidateImageModelKeyForUser_FailClosedOnNilRepo(t *testing.T) {
	// Degraded mode: repo unavailable (e.g. MySQL unreachable on startup). Tier
	// cannot be resolved, so it must fail-closed to Free — never widen access to
	// a Pro/Enterprise-only model because of an infra hiccup.
	if err := validateImageModelKeyForUser(context.Background(), nil, "user-1", "pro-only", failClosedPresets); err == nil {
		t.Fatalf("nil repo must fail-closed to Free; pro-only key should be rejected")
	}
	// Keys permitted at Free tier still pass under fail-closed.
	if err := validateImageModelKeyForUser(context.Background(), nil, "user-1", "", failClosedPresets); err != nil {
		t.Fatalf("empty key must always pass, got: %v", err)
	}
}

func TestValidateImageModelKeyForUser_FailClosedWhenUserMissing(t *testing.T) {
	// Non-nil repo but the user does not exist → FindByID errors → fail-closed to
	// Free (same TierFree default as the nil-repo branch).
	repo := repository.New(setupTaskHandlerTestDB(t))
	if err := validateImageModelKeyForUser(context.Background(), repo, "ghost-user", "pro-only", failClosedPresets); err == nil {
		t.Fatalf("missing-user lookup error must fail-closed to Free; pro-only key should be rejected")
	}
}

func TestValidateImageModelKeyForUser_ResolvesTierFromRepo(t *testing.T) {
	// Positive path: a real Pro user may select a Pro-only model, but not an
	// Enterprise-only one. Confirms tier is actually resolved from the repo.
	repo := repository.New(setupTaskHandlerTestDB(t))
	ctx := context.Background()
	uid := "user-pro"
	if err := repo.Users().Create(ctx, &model.User{ID: uid, Email: "pro@example.com", Password: "x", InviteCode: "ic", Tier: model.TierPro}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := validateImageModelKeyForUser(ctx, repo, uid, "pro-only", failClosedPresets); err != nil {
		t.Fatalf("pro user should be allowed a pro-only model, got: %v", err)
	}
	entPresets := []config.ImageModelPreset{{Key: "ent-only", MinTier: "enterprise"}}
	if err := validateImageModelKeyForUser(ctx, repo, uid, "ent-only", entPresets); err == nil {
		t.Fatalf("pro user must not be allowed an enterprise-only model")
	}
}

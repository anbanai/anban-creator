package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	serverconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

func TestImageCapabilityResolverRejectsUnavailableConfiguration(t *testing.T) {
	for _, resolver := range []*ImageCapabilityResolver{
		nil,
		NewImageCapabilityResolver(nil, nil),
	} {
		_, err := resolver.ResolvePublicImageCapability(t.Context(), "user", "standard")
		if err == nil || !strings.Contains(err.Error(), "image capability resolver is unavailable") {
			t.Fatalf("ResolvePublicImageCapability error = %v, want unavailable resolver", err)
		}
	}
}

func TestImageCapabilityResolverFreezesAndRejectsSemanticDrift(t *testing.T) {
	db := newMigrateTestDB(t)
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	user := &model.User{ID: "free", Email: "freeze@invalid.test", Password: "x", InviteCode: "freeze-code", Tier: model.TierFree}
	if err := repo.Users().Create(t.Context(), user); err != nil {
		t.Fatal(err)
	}
	route := serverconfig.ImageGenerationRouteConfig{
		Provider: "openai-primary", Model: "image-v1", BaseURL: "https://images.invalid/v1", APIKey: "secret-a",
		Timeout: 2 * time.Minute, MinTier: "free", BillingSKU: "image.standard", Enabled: true, ResponseFormat: "b64_json",
		GenerationFeatures: serverconfig.ImageGenerationFeatures{
			QualityLevels: []string{"standard"}, SizePresets: []string{"3:4"}, DefaultSize: "3:4",
			MaxBatch: 1, MaxReferenceImages: 4, SupportsReference: true, OutputFormats: []string{"png"}, Watermark: true,
		},
	}
	cfg := &serverconfig.Config{ModelRoutes: serverconfig.ModelRoutesConfig{ImageGeneration: serverconfig.ImageGenerationRoutesConfig{
		DefaultCapability: "standard", Capabilities: map[string]serverconfig.ImageGenerationRouteConfig{"standard": route},
	}}}
	resolver := NewImageCapabilityResolver(repo, cfg)

	frozen, err := resolver.FreezeImageCapability(t.Context(), user.ID, "standard")
	if err != nil {
		t.Fatalf("FreezeImageCapability: %v", err)
	}
	if frozen.Key != "standard" || frozen.BillingSKU != "image.standard" || frozen.Provider != "openai-primary" ||
		frozen.Model != "image-v1" || frozen.BaseURL != "https://images.invalid/v1" || frozen.Digest == "" {
		t.Fatalf("frozen capability = %#v", frozen)
	}
	if _, err := resolver.ResolveFrozenImageModelForGeneration(t.Context(), user.ID, frozen, "content", 1); err != nil {
		t.Fatalf("ResolveFrozenImageModelForGeneration: %v", err)
	}

	// Credentials may rotate without mutating task semantics.
	route.APIKey = "secret-b"
	cfg.ModelRoutes.ImageGeneration.Capabilities["standard"] = route
	if _, err := resolver.ResolveFrozenImageModelForGeneration(t.Context(), user.ID, frozen, "content", 1); err != nil {
		t.Fatalf("credential rotation rejected: %v", err)
	}

	// Provider/model/SKU/features/limits are immutable for an admitted task.
	route.BillingSKU = "image.professional"
	cfg.ModelRoutes.ImageGeneration.Capabilities["standard"] = route
	if _, err := resolver.ResolveFrozenImageModelForGeneration(t.Context(), user.ID, frozen, "content", 1); !errors.Is(err, ErrTaskImageCapabilityConflict) {
		t.Fatalf("semantic drift error = %v, want ErrTaskImageCapabilityConflict", err)
	}
}

func TestImageCapabilityResolverRejectsTamperedFrozenSnapshot(t *testing.T) {
	resolver := NewImageCapabilityResolver(nil, &serverconfig.Config{ModelRoutes: serverconfig.ModelRoutesConfig{ImageGeneration: serverconfig.ImageGenerationRoutesConfig{
		DefaultCapability: "standard",
		Capabilities: map[string]serverconfig.ImageGenerationRouteConfig{
			"standard": {Provider: "openai", Model: "image-v1", BaseURL: "https://images.invalid/v1", APIKey: "secret", Timeout: time.Minute, MinTier: "free", BillingSKU: "image.standard", Enabled: true},
		},
	}}})
	frozen, err := resolver.FreezeImageCapability(t.Context(), "", "standard")
	if err != nil {
		t.Fatal(err)
	}
	frozen.BillingSKU = "image.professional"
	if _, err := resolver.ResolveFrozenImageModelForGeneration(t.Context(), "", frozen, "content", 0); !errors.Is(err, ErrTaskImageCapabilityInvalid) {
		t.Fatalf("tampered snapshot error = %v, want ErrTaskImageCapabilityInvalid", err)
	}
}

func TestImageCapabilityResolverIsStrictAndDoesNotAutoUpgradeReferences(t *testing.T) {
	db := newMigrateTestDB(t)
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	for _, user := range []*model.User{
		{ID: "free", Email: "free@invalid.test", Password: "x", InviteCode: "free-code", Tier: model.TierFree},
		{ID: "enterprise", Email: "enterprise@invalid.test", Password: "x", InviteCode: "enterprise-code", Tier: model.TierEnterprise},
	} {
		if err := repo.Users().Create(t.Context(), user); err != nil {
			t.Fatal(err)
		}
	}
	capability := func(tier, sku string, supportsReference bool, maxReferences int) serverconfig.ImageGenerationRouteConfig {
		return serverconfig.ImageGenerationRouteConfig{
			Provider: "volcengine", Model: "internal-image", BaseURL: "https://internal.invalid", APIKey: "secret",
			Alias: "图像能力", Description: "图像能力说明", MinTier: tier, BillingSKU: sku,
			Timeout: time.Minute, Enabled: true, QualityRank: 100,
			GenerationFeatures: serverconfig.ImageGenerationFeatures{
				DefaultSize: "1:1", SizePresets: []string{"1:1"}, MaxBatch: 1, OutputFormats: []string{"png"},
				SupportsReference: supportsReference, MaxReferenceImages: maxReferences,
			},
		}
	}
	cfg := &serverconfig.Config{ModelRoutes: serverconfig.ModelRoutesConfig{ImageGeneration: serverconfig.ImageGenerationRoutesConfig{
		DefaultCapability: "standard",
		Capabilities: map[string]serverconfig.ImageGenerationRouteConfig{
			"standard":     capability("free", "image.standard", false, 0),
			"professional": capability("enterprise", "image.professional", true, 4),
		},
	}}}
	resolver := NewImageCapabilityResolver(repo, cfg)

	selected, err := resolver.ResolvePublicImageCapability(t.Context(), "free", "")
	if err != nil || selected.Key != "standard" {
		t.Fatalf("public default resolution = %#v, %v", selected, err)
	}
	if _, _, err := resolver.ResolveImageConfigForTaskKey(t.Context(), "free", ""); err == nil || !strings.Contains(err.Error(), "frozen image capability") {
		t.Fatalf("empty task config capability error = %v, want missing frozen capability rejection", err)
	}
	if _, err := resolver.FreezeImageCapability(t.Context(), "free", "professional"); err == nil {
		t.Fatal("free user resolved enterprise capability")
	}
	if _, err := resolver.FreezeImageCapability(t.Context(), "enterprise", "missing"); err == nil {
		t.Fatal("unknown capability silently fell back")
	}
	standard, err := resolver.FreezeImageCapability(t.Context(), "enterprise", "standard")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.ResolveFrozenImageModelForGeneration(t.Context(), "enterprise", standard, "content", 1); err == nil {
		t.Fatal("reference request silently upgraded from standard")
	}
	professional, err := resolver.FreezeImageCapability(t.Context(), "enterprise", "professional")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.ResolveFrozenImageModelForGeneration(t.Context(), "enterprise", professional, "content", 5); err == nil {
		t.Fatal("reference limit was not rejected")
	} else {
		var limit *ImageReferenceLimitError
		if !errors.As(err, &limit) || limit.Requested != 5 || limit.MaxReferenceImages != 4 {
			t.Fatalf("reference error = %#v, %v", limit, err)
		}
	}
}

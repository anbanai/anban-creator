package service

import (
	"errors"
	"testing"
	"time"

	serverconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

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
			DesignerFeatures: serverconfig.DesignerProviderCapabilities{
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

	standard, err := resolver.ResolveImageModelForGeneration(t.Context(), "free", "", "content", 0)
	if err != nil || standard.Key != "standard" || standard.BillingSKU != "image.standard" {
		t.Fatalf("default resolution = %#v, %v", standard, err)
	}
	if _, err := resolver.ResolveImageModelForGeneration(t.Context(), "free", "professional", "content", 0); err == nil {
		t.Fatal("free user resolved enterprise capability")
	}
	if _, err := resolver.ResolveImageModelForGeneration(t.Context(), "enterprise", "missing", "content", 0); err == nil {
		t.Fatal("unknown capability silently fell back")
	}
	if _, err := resolver.ResolveImageModelForGeneration(t.Context(), "enterprise", "standard", "content", 1); err == nil {
		t.Fatal("reference request silently upgraded from standard")
	}
	if _, err := resolver.ResolveImageModelForGeneration(t.Context(), "enterprise", "professional", "content", 5); err == nil {
		t.Fatal("reference limit was not rejected")
	} else {
		var limit *ImageReferenceLimitError
		if !errors.As(err, &limit) || limit.Requested != 5 || limit.MaxReferenceImages != 4 {
			t.Fatalf("reference error = %#v, %v", limit, err)
		}
	}
}

package service

import (
	"testing"

	"github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/model"
)

func TestResolveVideoGenerationPlanRequiresProjectProfile(t *testing.T) {
	_, err := ResolveVideoGenerationPlan(VideoGenerationRequest{
		Prompt: "生成一条产品种草视频",
	}, model.VideoDefaults{}, model.VideoModelPolicy{}, DefaultVideoModelCatalog(), 1000)
	if err == nil {
		t.Fatal("expected missing project video profile to fail")
	}
	if got := err.Error(); got != "project video profile is not configured" {
		t.Fatalf("error = %q", got)
	}
}

func TestResolveVideoGenerationPlanAppliesProjectDefaultsAndDynamicCredits(t *testing.T) {
	watermark := false
	plan, err := ResolveVideoGenerationPlan(VideoGenerationRequest{
		Prompt: "生成一条咖啡杯种草视频",
	}, model.VideoDefaults{
		Purpose:    VideoPurposePlanting,
		ModelKey:   "seedance-2.0-mini",
		Resolution: "720p",
		Ratio:      "16:9",
		Duration:   5,
		Watermark:  &watermark,
		Preflight:  true,
	}, model.VideoModelPolicy{
		AllowedModels: []string{"seedance-2.0-mini", "seedance-2.0-fast"},
		DefaultModel:  "seedance-2.0-mini",
		MaxResolution: "720p",
		MaxDuration:   15,
	}, DefaultVideoModelCatalog(), 1000)
	if err != nil {
		t.Fatalf("ResolveVideoGenerationPlan: %v", err)
	}
	if plan.Model != "doubao-seedance-2-0-mini-260615" {
		t.Fatalf("resolved model = %q", plan.Model)
	}
	if plan.EstimatedCredits != 2480 {
		t.Fatalf("credits = %d, want 2480", plan.EstimatedCredits)
	}
	if plan.PricingBreakdown == nil || plan.PricingBreakdown.CNY != 2.48 || plan.PricingBreakdown.CreditMultiplier != 1000 {
		t.Fatalf("pricing = %#v", plan.PricingBreakdown)
	}
	if !plan.Preflight {
		t.Fatal("preflight default should be preserved")
	}
	preflight := false
	overridden, err := ResolveVideoGenerationPlan(VideoGenerationRequest{
		Prompt:    "生成一条咖啡杯种草视频",
		Preflight: &preflight,
	}, model.VideoDefaults{
		Purpose:    VideoPurposePlanting,
		ModelKey:   "seedance-2.0-mini",
		Resolution: "720p",
		Ratio:      "16:9",
		Duration:   5,
		Watermark:  &watermark,
		Preflight:  true,
	}, model.VideoModelPolicy{
		AllowedModels: []string{"seedance-2.0-mini", "seedance-2.0-fast"},
		DefaultModel:  "seedance-2.0-mini",
		MaxResolution: "720p",
		MaxDuration:   15,
	}, DefaultVideoModelCatalog(), 1000)
	if err != nil {
		t.Fatalf("ResolveVideoGenerationPlan override: %v", err)
	}
	if overridden.Preflight {
		t.Fatal("preflight override should be preserved")
	}
}

func TestResolveVideoGenerationPlanRejectsUnsupportedModelResolution(t *testing.T) {
	_, err := ResolveVideoGenerationPlan(VideoGenerationRequest{
		Prompt:     "生成视频",
		Model:      "seedance-2.0-mini",
		Resolution: "1080p",
		Ratio:      "16:9",
		Duration:   5,
	}, model.VideoDefaults{
		Purpose:    VideoPurposePlanting,
		ModelKey:   "seedance-2.0-mini",
		Resolution: "720p",
		Ratio:      "16:9",
		Duration:   5,
	}, model.VideoModelPolicy{
		AllowedModels: []string{"seedance-2.0-mini"},
		DefaultModel:  "seedance-2.0-mini",
		MaxResolution: "1080p",
		MaxDuration:   15,
	}, DefaultVideoModelCatalog(), 1000)
	if err == nil {
		t.Fatal("expected unsupported resolution to fail")
	}
	if got := err.Error(); got != "model seedance-2.0-mini does not support resolution 1080p; suggested resolution: 720p" {
		t.Fatalf("error = %q", got)
	}
}

func TestResolveVideoGenerationPlanAutoDowngradesResolutionWhenPolicyAllows(t *testing.T) {
	plan, err := ResolveVideoGenerationPlan(VideoGenerationRequest{
		Prompt:     "生成视频",
		Model:      "seedance-2.0-mini",
		Resolution: "1080p",
		Ratio:      "16:9",
		Duration:   5,
	}, model.VideoDefaults{
		Purpose:    VideoPurposePlanting,
		ModelKey:   "seedance-2.0-mini",
		Resolution: "720p",
		Ratio:      "16:9",
		Duration:   5,
		Preflight:  true,
	}, model.VideoModelPolicy{
		AllowedModels:      []string{"seedance-2.0-mini"},
		DefaultModel:       "seedance-2.0-mini",
		AllowAutoDowngrade: true,
		MaxResolution:      "1080p",
		MaxDuration:        15,
	}, DefaultVideoModelCatalog(), 1000)
	if err != nil {
		t.Fatalf("ResolveVideoGenerationPlan: %v", err)
	}
	if plan.Resolution != "720p" || plan.EstimatedCredits != 2480 {
		t.Fatalf("auto downgraded plan = %#v, want 720p and 2480 credits", plan)
	}
}

func TestVideoAPIConfigUsesCatalogAndMultiplierNotBusinessDefaults(t *testing.T) {
	cfg := config.VideoAPIConfig{}
	if cfg.CreditMultiplierOrDefault() != 1000 {
		t.Fatalf("default multiplier = %d, want 1000", cfg.CreditMultiplierOrDefault())
	}
	if len(cfg.ModelCatalogOrDefault()) < 3 {
		t.Fatalf("model catalog should include seedance 2.0, fast, mini")
	}
}

func TestVideoModelCatalogFromConfigMergesConfiguredModelIDWithDefaultPricing(t *testing.T) {
	catalog := VideoModelCatalogFromConfig([]config.VideoModelCatalogEntry{{
		Key:     "seedance-2.0-mini",
		ModelID: "custom-mini-model",
	}})
	spec := catalog["seedance-2.0-mini"]
	if spec.ModelID != "custom-mini-model" {
		t.Fatalf("model id = %q", spec.ModelID)
	}
	if !spec.SupportsVideoInput || len(spec.NoInputPricePerSecond) == 0 || spec.NoInputPricePerSecond["720p"] == 0 {
		t.Fatalf("default capabilities/pricing were not preserved: %#v", spec)
	}
}

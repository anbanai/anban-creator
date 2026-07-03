package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
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

func TestVideoGenerationConfigErrorPreservesMessageAndSentinel(t *testing.T) {
	err := wrapVideoGenerationConfigError(errors.New("project video profile is not configured"))
	if !errors.Is(err, ErrVideoGenerationConfig) {
		t.Fatal("wrapped error should match ErrVideoGenerationConfig")
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

func TestVideoModelCatalogFromConfigOnlyExposesConfiguredModelsWhenProvided(t *testing.T) {
	catalog := VideoModelCatalogFromConfig([]config.VideoModelCatalogEntry{{
		Key:                  "configured-video",
		DisplayName:          "Configured Video",
		ModelID:              "provider-configured-video",
		SupportedResolutions: []string{"720p"},
		SupportedRatios:      []string{"9:16"},
		MinDuration:          1,
		MaxDuration:          15,
		NoInputPricePerSecond: map[string]float64{
			"720p": 1,
		},
	}})

	if _, ok := catalog["configured-video"]; !ok {
		t.Fatalf("configured model missing from catalog: %+v", catalog)
	}
	if _, ok := catalog["seedance-2.0-mini"]; ok {
		t.Fatalf("default model leaked into explicitly configured catalog: %+v", catalog)
	}
}

func TestResolveVideoGenerationPlanRejectsAllowedModelMissingFromCatalog(t *testing.T) {
	_, err := ResolveVideoGenerationPlan(VideoGenerationRequest{
		Prompt: "生成视频",
	}, model.VideoDefaults{
		Purpose:    VideoPurposePlanting,
		ModelKey:   "missing-model",
		Resolution: "720p",
		Ratio:      "9:16",
		Duration:   5,
	}, model.VideoModelPolicy{
		AllowedModels: []string{"missing-model"},
		DefaultModel:  "missing-model",
		MaxResolution: "720p",
		MaxDuration:   15,
	}, VideoModelCatalog{
		"configured-video": {
			Key:                  "configured-video",
			ModelID:              "provider-configured-video",
			SupportedResolutions: []string{"720p"},
			SupportedRatios:      []string{"9:16"},
			MinDuration:          1,
			MaxDuration:          15,
			NoInputPricePerSecond: map[string]float64{
				"720p": 1,
			},
		},
	}, 1000)
	if err == nil || !strings.Contains(err.Error(), "video model missing-model is not configured") {
		t.Fatalf("ResolveVideoGenerationPlan error = %v, want not configured", err)
	}
}

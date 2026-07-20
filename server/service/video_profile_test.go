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
	}, model.VideoDefaults{}, model.VideoModelPolicy{}, DefaultVideoModelCatalog())
	if err == nil {
		t.Fatal("expected missing project videocreator profile to fail")
	}
	if got := err.Error(); got != "project videocreator profile is not configured" {
		t.Fatalf("error = %q", got)
	}
}

func TestVideoGenerationConfigErrorPreservesMessageAndSentinel(t *testing.T) {
	err := wrapVideoGenerationConfigError(errors.New("project videocreator profile is not configured"))
	if !errors.Is(err, ErrVideoGenerationConfig) {
		t.Fatal("wrapped error should match ErrVideoGenerationConfig")
	}
	if got := err.Error(); got != "project videocreator profile is not configured" {
		t.Fatalf("error = %q", got)
	}
}

func TestResolveVideoGenerationPlanAppliesProjectDefaultsWithoutRetailPricing(t *testing.T) {
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
	}, DefaultVideoModelCatalog())
	if err != nil {
		t.Fatalf("ResolveVideoGenerationPlan: %v", err)
	}
	if plan.Model != "doubao-seedance-2-0-mini-260615" {
		t.Fatalf("resolved model = %q", plan.Model)
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
	}, DefaultVideoModelCatalog())
	if err != nil {
		t.Fatalf("ResolveVideoGenerationPlan override: %v", err)
	}
	if overridden.Preflight {
		t.Fatal("preflight override should be preserved")
	}
}

func TestResolveVideoGenerationPlanDefaultsCreativeTypePurposePair(t *testing.T) {
	plan, err := ResolveVideoGenerationPlan(VideoGenerationRequest{
		Prompt:       "生成一条办公室高效段子",
		CreativeType: VideoCreativeTypeHighEfficiencyJoke,
	}, model.VideoDefaults{
		ModelKey:   "seedance-2.0-mini",
		Resolution: "720p",
		Ratio:      "9:16",
		Duration:   5,
	}, model.VideoModelPolicy{
		AllowedModels: []string{"seedance-2.0-mini"},
		DefaultModel:  "seedance-2.0-mini",
		MaxResolution: "720p",
		MaxDuration:   15,
	}, DefaultVideoModelCatalog())
	if err != nil {
		t.Fatalf("ResolveVideoGenerationPlan: %v", err)
	}
	if plan.CreativeType != VideoCreativeTypeHighEfficiencyJoke {
		t.Fatalf("creative type = %q, want high-efficiency joke", plan.CreativeType)
	}
	if plan.Purpose != VideoPurposePromotion {
		t.Fatalf("purpose = %q, want promotion", plan.Purpose)
	}
}

func TestResolveVideoGenerationPlanAppliesPlaybookRouting(t *testing.T) {
	plan, err := ResolveVideoGenerationPlan(VideoGenerationRequest{
		Prompt:      "做一个品牌记忆点视频",
		ScenarioKey: "brand_promo",
	}, model.VideoDefaults{
		Purpose:      VideoPurposePlanting,
		CreativeType: VideoCreativeTypePersonalIP,
		ModelKey:     "seedance-2.0-mini",
		Resolution:   "720p",
		Ratio:        "1:1",
		Duration:     5,
	}, model.VideoModelPolicy{
		AllowedModels: []string{"seedance-2.0-mini"},
		DefaultModel:  "seedance-2.0-mini",
		MaxResolution: "720p",
		MaxDuration:   15,
	}, DefaultVideoModelCatalog())
	if err != nil {
		t.Fatalf("ResolveVideoGenerationPlan: %v", err)
	}
	if plan.CreativeType != VideoCreativeTypeBrandPromo || plan.Purpose != VideoPurposePromotion || plan.Ratio != "16:9" {
		t.Fatalf("playbook routing = creative:%s purpose:%s ratio:%s", plan.CreativeType, plan.Purpose, plan.Ratio)
	}
}

func TestResolveVideoGenerationPlanValidatesProductionControls(t *testing.T) {
	defaults := model.VideoDefaults{
		Purpose:    VideoPurposePlanting,
		ModelKey:   "seedance-2.0-mini",
		Resolution: "720p",
		Ratio:      "9:16",
		Duration:   5,
	}
	policy := model.VideoModelPolicy{
		AllowedModels: []string{"seedance-2.0-mini"},
		DefaultModel:  "seedance-2.0-mini",
		MaxResolution: "720p",
		MaxDuration:   15,
	}

	_, err := ResolveVideoGenerationPlan(VideoGenerationRequest{
		Prompt:         "生成一条产品视频",
		ProductionMode: "magic_mode",
	}, defaults, policy, DefaultVideoModelCatalog())
	if err == nil || !strings.Contains(err.Error(), "production_mode must be one of") {
		t.Fatalf("invalid production mode error = %v", err)
	}

	_, err = ResolveVideoGenerationPlan(VideoGenerationRequest{
		Prompt:       "生成一条产品视频",
		RetakeBudget: 21,
	}, defaults, policy, DefaultVideoModelCatalog())
	if err == nil || !strings.Contains(err.Error(), "retake_budget must be between 0 and 20") {
		t.Fatalf("invalid retake budget error = %v", err)
	}
}

func TestResolveVideoGenerationPlanSplitsTargetDurationByModelLimit(t *testing.T) {
	plan, err := ResolveVideoGenerationPlan(VideoGenerationRequest{
		Prompt:   "生成一条 1 分钟茶文化短视频",
		Duration: 60,
	}, model.VideoDefaults{
		Purpose:    VideoPurposePlanting,
		ModelKey:   "seedance-2.0-mini",
		Resolution: "720p",
		Ratio:      "9:16",
	}, model.VideoModelPolicy{
		AllowedModels: []string{"seedance-2.0-mini"},
		DefaultModel:  "seedance-2.0-mini",
		MaxResolution: "720p",
		MaxDuration:   120,
	}, DefaultVideoModelCatalog())
	if err != nil {
		t.Fatalf("ResolveVideoGenerationPlan: %v", err)
	}
	if plan.TargetDurationSeconds != 60 {
		t.Fatalf("target duration = %d, want 60", plan.TargetDurationSeconds)
	}
	if plan.TargetDurationSource != VideoDurationSourceUser {
		t.Fatalf("target duration source = %q, want user", plan.TargetDurationSource)
	}
	if len(plan.Segments) != 4 {
		t.Fatalf("segments = %d, want 4: %#v", len(plan.Segments), plan.Segments)
	}
	for i, seg := range plan.Segments {
		if seg.Index != i+1 {
			t.Fatalf("segment %d index = %d, want %d", i, seg.Index, i+1)
		}
		if seg.Duration != 15 {
			t.Fatalf("segment %d duration = %d, want 15", i, seg.Duration)
		}
		if seg.Duration > 15 {
			t.Fatalf("segment %d exceeds model limit: %d", i, seg.Duration)
		}
	}
}

func TestResolveVideoGenerationPlanUsesReferenceVideoDurationWhenUserDurationMissing(t *testing.T) {
	plan, err := ResolveVideoGenerationPlan(VideoGenerationRequest{
		Prompt: "按参考视频节奏生成同款",
		ReferenceSet: []VideoReferenceInput{{
			Type:                 VideoReferenceVideo,
			URL:                  "https://example.com/reference.mp4",
			InputDurationSeconds: 60.2,
		}},
	}, model.VideoDefaults{
		Purpose:    VideoPurposePlanting,
		ModelKey:   "seedance-2.0-mini",
		Resolution: "720p",
		Ratio:      "9:16",
		Duration:   15,
	}, model.VideoModelPolicy{
		AllowedModels: []string{"seedance-2.0-mini"},
		DefaultModel:  "seedance-2.0-mini",
		MaxResolution: "720p",
		MaxDuration:   120,
	}, DefaultVideoModelCatalog())
	if err != nil {
		t.Fatalf("ResolveVideoGenerationPlan: %v", err)
	}
	if plan.TargetDurationSeconds != 60 {
		t.Fatalf("target duration = %d, want rounded reference duration 60", plan.TargetDurationSeconds)
	}
	if plan.TargetDurationSource != VideoDurationSourceReferenceVideo {
		t.Fatalf("duration source = %q, want reference_video", plan.TargetDurationSource)
	}
	if len(plan.Segments) != 4 {
		t.Fatalf("segments = %d, want 4", len(plan.Segments))
	}
}

func TestResolveVideoGenerationPlanRequiresAIPlannedDurationReason(t *testing.T) {
	_, err := ResolveVideoGenerationPlan(VideoGenerationRequest{
		Prompt:                 "生成一条茶文化短视频",
		PlannedDurationSeconds: 45,
	}, model.VideoDefaults{
		Purpose:    VideoPurposePlanting,
		ModelKey:   "seedance-2.0-mini",
		Resolution: "720p",
		Ratio:      "9:16",
	}, model.VideoModelPolicy{
		AllowedModels: []string{"seedance-2.0-mini"},
		DefaultModel:  "seedance-2.0-mini",
		MaxResolution: "720p",
		MaxDuration:   120,
	}, DefaultVideoModelCatalog())
	if err == nil || !strings.Contains(err.Error(), "target_duration_reason is required") {
		t.Fatalf("ResolveVideoGenerationPlan error = %v, want reason requirement", err)
	}
}

func TestResolveVideoGenerationPlanRebalancesShortRemainder(t *testing.T) {
	catalog := DefaultVideoModelCatalog()
	spec := catalog["seedance-2.0-mini"]
	spec.MinDuration = 5
	spec.MaxDuration = 15
	catalog["seedance-2.0-mini"] = spec

	plan, err := ResolveVideoGenerationPlan(VideoGenerationRequest{
		Prompt:   "生成 46 秒视频",
		Duration: 46,
	}, model.VideoDefaults{
		Purpose:    VideoPurposePlanting,
		ModelKey:   "seedance-2.0-mini",
		Resolution: "720p",
		Ratio:      "9:16",
	}, model.VideoModelPolicy{
		AllowedModels: []string{"seedance-2.0-mini"},
		DefaultModel:  "seedance-2.0-mini",
		MaxResolution: "720p",
		MaxDuration:   120,
	}, catalog)
	if err != nil {
		t.Fatalf("ResolveVideoGenerationPlan: %v", err)
	}
	got := make([]int64, 0, len(plan.Segments))
	var total int64
	for _, seg := range plan.Segments {
		got = append(got, seg.Duration)
		total += seg.Duration
		if seg.Duration < 5 || seg.Duration > 15 {
			t.Fatalf("segment duration %d outside [5,15]; segments=%v", seg.Duration, got)
		}
	}
	if total != 46 {
		t.Fatalf("segment total = %d, want 46; segments=%v", total, got)
	}
	want := []int64{12, 12, 11, 11}
	if len(got) != len(want) {
		t.Fatalf("segments = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("segments = %v, want %v", got, want)
		}
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
	}, DefaultVideoModelCatalog())
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
	}, DefaultVideoModelCatalog())
	if err != nil {
		t.Fatalf("ResolveVideoGenerationPlan: %v", err)
	}
	if plan.Resolution != "720p" {
		t.Fatalf("auto downgraded plan = %#v, want 720p", plan)
	}
}

func TestVideoAPIConfigUsesCatalogNotBusinessDefaults(t *testing.T) {
	cfg := config.VideoAPIConfig{}
	if len(VideoModelCatalogFromConfig(cfg.ModelCatalog)) != 0 {
		t.Fatalf("empty video model config must not expose default models")
	}
}

func TestVideoModelCatalogFromConfigEmptyConfigExposesNoModels(t *testing.T) {
	catalog := VideoModelCatalogFromConfig(nil)
	if len(catalog) != 0 {
		t.Fatalf("empty config exposed models: %+v", catalog)
	}
}

func TestVideoModelCatalogFromConfigMergesConfiguredModelIDWithDefaultCapabilities(t *testing.T) {
	catalog := VideoModelCatalogFromConfig([]config.VideoModelCatalogEntry{{
		Key:     "seedance-2.0-mini",
		ModelID: "custom-mini-model",
	}})
	spec := catalog["seedance-2.0-mini"]
	if spec.ModelID != "custom-mini-model" {
		t.Fatalf("model id = %q", spec.ModelID)
	}
	if !spec.SupportsVideoInput || len(spec.SupportedResolutions) == 0 {
		t.Fatalf("default capabilities were not preserved: %#v", spec)
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
		},
	})
	if err == nil || !strings.Contains(err.Error(), "video model missing-model is not configured") {
		t.Fatalf("ResolveVideoGenerationPlan error = %v, want not configured", err)
	}
}

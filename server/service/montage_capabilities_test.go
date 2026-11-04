package service

import (
	"errors"
	"strings"
	"testing"

	serverconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func testMontageCapabilityService() *MontageCapabilityService {
	return NewMontageCapabilityService(serverconfig.MontageConfig{
		Enabled:            true,
		DefaultPipeline:    "cinematic",
		AllowedPipelines:   []string{"animated-explainer", "animation", "avatar-spokesperson", "character-animation", "cinematic", "clip-factory", "documentary-montage", "framework-smoke", "hybrid", "localization-dub", "podcast-repurpose", "screen-demo", "talking-head"},
		MaxDurationSeconds: 600,
		MaxAssets:          20,
	})
}

func TestValidateMaterializableMontageSourceAssetScopesMediaLibraryAsset(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:montage-source-asset?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.Asset{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := repository.New(db)
	asset := &model.Asset{ID: "media-asset", UserID: "owner", Purpose: DirectUploadPurposeMediaLibrary, StorageKey: "assets/users/owner/media-asset/clip.mp4", FileName: "clip.mp4", ContentType: "video/mp4", Size: 10, ETag: "etag"}
	if err := repo.Assets().Create(t.Context(), asset); err != nil {
		t.Fatalf("create asset: %v", err)
	}
	input := []model.MontageAsset{{Type: "video", AssetID: asset.ID, FileName: asset.FileName, MimeType: asset.ContentType, FileSize: asset.Size}}
	if err := ValidateMaterializableMontageSourceTaskFiles(t.Context(), repo, "oss", "owner", "project", input); err != nil {
		t.Fatalf("owner validation: %v", err)
	}
	if err := ValidateMaterializableMontageSourceTaskFiles(t.Context(), repo, "oss", "other-user", "project", input); err == nil || !containsText(err.Error(), "unavailable") {
		t.Fatalf("foreign validation = %v, want unavailable asset", err)
	}
}

func TestMontageCapabilityServiceCatalogExposesAllPinnedTemplates(t *testing.T) {
	catalog := testMontageCapabilityService().Catalog()

	if !catalog.Enabled || catalog.DefaultPipeline != "cinematic" || catalog.MaxDurationSeconds != 600 || catalog.MaxAssets != 20 {
		t.Fatalf("catalog metadata = %#v", catalog)
	}
	wantKeys := model.PinnedMontagePipelineKeys()
	if len(catalog.Items) != len(wantKeys) {
		t.Fatalf("catalog items = %d, want %d", len(catalog.Items), len(wantKeys))
	}
	for i, want := range wantKeys {
		if catalog.Items[i].Key != want {
			t.Fatalf("catalog item %d key = %q, want %q", i, catalog.Items[i].Key, want)
		}
	}
	if got := catalog.Items[12]; got.DisplayName != "口播精剪" || got.SourceRequirement != MontageSourceRequirementVideo || got.OutputMode != MontageOutputModeSingle {
		t.Fatalf("talking-head capability = %#v", got)
	}
	if got := catalog.Items[5]; got.DisplayName != "长视频拆条" || got.SourceRequirement != MontageSourceRequirementVideoOrAudio || got.OutputMode != MontageOutputModeMultiple {
		t.Fatalf("clip-factory capability = %#v", got)
	}
	if catalog.Items[7].Available || catalog.Items[7].AvailabilityReason == "" {
		t.Fatalf("framework-smoke must remain visible but unavailable: %#v", catalog.Items[7])
	}
}

func TestMontageCapabilityServiceRejectsPinnedPipelineDisabledByConfig(t *testing.T) {
	svc := NewMontageCapabilityService(serverconfig.MontageConfig{
		Enabled: true, DefaultPipeline: "cinematic", AllowedPipelines: []string{"cinematic"},
		MaxDurationSeconds: 600, MaxAssets: 20,
	})
	catalog := svc.Catalog()
	var disabled bool
	for _, item := range catalog.Items {
		if item.Key == "hybrid" {
			disabled = !item.Available
		}
	}
	if !disabled {
		t.Fatal("hybrid must be visible as unavailable when not enabled in config")
	}
	input := model.MontageInput{Brief: "test", PipelineKey: "hybrid"}
	if err := svc.NormalizeAndValidateInput(&input, model.MontageDefaults{}); err == nil || !errors.Is(err, ErrMontageInput) {
		t.Fatalf("NormalizeAndValidateInput error = %v, want ErrMontageInput", err)
	}
}

func TestMontageCapabilityServiceCatalogExposesPinnedTemplatesStylesAndOutputProfiles(t *testing.T) {
	catalog := testMontageCapabilityService().Catalog()
	for _, key := range []string{"animated-explainer", "animation", "avatar-spokesperson", "character-animation", "documentary-montage", "hybrid", "localization-dub", "podcast-repurpose"} {
		if _, ok := testMontageCapabilityService().pipeline(key); !ok {
			t.Errorf("pinned pipeline %q is not available", key)
		}
	}
	if len(catalog.Styles) != 5 || len(catalog.OutputProfiles) != 8 {
		t.Fatalf("catalog styles/profiles = %d/%d, want 5/8", len(catalog.Styles), len(catalog.OutputProfiles))
	}
	input := model.MontageInput{Brief: "test", PipelineKey: "hybrid", OutputProfileKey: "youtube_shorts", Preferences: model.MontagePreferences{PlaybookKey: "flat-motion-graphics"}}
	if err := testMontageCapabilityService().NormalizeAndValidateInput(&input, model.MontageDefaults{}); err != nil {
		t.Fatalf("NormalizeAndValidateInput: %v", err)
	}
	if input.OutputProfile == nil || input.OutputProfile.AspectRatio != "9:16" || input.OutputProfile.Width != 1080 || input.OutputProfile.Height != 1920 {
		t.Fatalf("resolved output profile = %#v", input.OutputProfile)
	}
}

func TestMontageCapabilityServiceRejectsUnknownPinnedTemplateAndOutputProfile(t *testing.T) {
	tests := []struct {
		name  string
		input model.MontageInput
		want  string
	}{
		{name: "unknown pipeline", input: model.MontageInput{Brief: "x", PipelineKey: "user-pipeline"}, want: "pipeline_key"},
		{name: "unknown style", input: model.MontageInput{Brief: "x", PipelineKey: "cinematic", Preferences: model.MontagePreferences{PlaybookKey: "custom-style"}}, want: "playbook_key"},
		{name: "unknown output profile", input: model.MontageInput{Brief: "x", PipelineKey: "cinematic", OutputProfileKey: "custom-size"}, want: "output_profile_key"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := testMontageCapabilityService().NormalizeAndValidateInput(&tc.input, model.MontageDefaults{})
			if err == nil || !errors.Is(err, ErrMontageInput) || !containsText(err.Error(), tc.want) {
				t.Fatalf("error = %v, want ErrMontageInput containing %q", err, tc.want)
			}
		})
	}
}

func TestMontageCapabilityServiceAcceptsWhitespacePaddedAllowedPipeline(t *testing.T) {
	svc := NewMontageCapabilityService(serverconfig.MontageConfig{
		Enabled: true, DefaultPipeline: " cinematic ", AllowedPipelines: []string{" cinematic "},
		MaxDurationSeconds: 600, MaxAssets: 20,
	})
	input := model.MontageInput{Brief: "带空格的配置", PipelineKey: "cinematic"}
	if err := svc.NormalizeAndValidateInput(&input, model.MontageDefaults{}); err != nil {
		t.Fatalf("NormalizeAndValidateInput error: %v", err)
	}
	if input.PipelineKey != "cinematic" {
		t.Fatalf("pipeline_key = %q, want cinematic", input.PipelineKey)
	}
}

func TestMontageCapabilityServiceCatalogHidesItemsWhenDisabled(t *testing.T) {
	catalog := NewMontageCapabilityService(serverconfig.MontageConfig{
		Enabled:          false,
		AllowedPipelines: []string{"cinematic"},
	}).Catalog()

	if catalog.Enabled || len(catalog.Items) != 0 {
		t.Fatalf("disabled catalog = %#v", catalog)
	}
}

func TestMontageCapabilityServiceNormalizesTaskInput(t *testing.T) {
	svc := testMontageCapabilityService()
	input := model.MontageInput{
		Brief:       "  发布新品  ",
		Preferences: model.MontagePreferences{},
	}
	defaults := model.MontageDefaults{
		DefaultPipeline: "talking-head",
		Preferences: model.MontagePreferences{
			DurationSeconds: 90,
			PlaybookKey:     "clean-professional",
			SubtitleMode:    "burned_in",
		},
		DeliveryTargets: []string{"wechat"},
	}

	if err := svc.NormalizeAndValidateInput(&input, defaults); err == nil || !errors.Is(err, ErrMontageInput) {
		t.Fatalf("missing required talking-head video error = %v", err)
	}
	input.SourceAssets = []model.MontageAsset{{Type: "video_url", URL: "https://example.com/talk.mp4"}}
	if err := svc.NormalizeAndValidateInput(&input, defaults); err != nil {
		t.Fatalf("NormalizeAndValidateInput error: %v", err)
	}
	if input.Brief != "发布新品" || input.PipelineKey != "talking-head" {
		t.Fatalf("normalized identity = %#v", input)
	}
	if input.Preferences.DurationSeconds != 90 || input.Preferences.PlaybookKey != "clean-professional" || input.Preferences.SubtitleMode != "burned_in" {
		t.Fatalf("normalized preferences = %#v", input.Preferences)
	}
	if len(input.DeliveryTargets) != 1 || input.DeliveryTargets[0] != "wechat" {
		t.Fatalf("normalized delivery targets = %#v", input.DeliveryTargets)
	}
}

func TestMontageCapabilityServiceUsesPipelineRecommendationWhenNoDurationDefault(t *testing.T) {
	input := model.MontageInput{
		Brief:       "拆成短视频",
		PipelineKey: "clip-factory",
		SourceAssets: []model.MontageAsset{{
			Type: "audio_url",
			URL:  "https://example.com/interview.m4a",
		}},
	}

	if err := testMontageCapabilityService().NormalizeAndValidateInput(&input, model.MontageDefaults{}); err != nil {
		t.Fatalf("NormalizeAndValidateInput error: %v", err)
	}
	if input.Preferences.DurationSeconds != 45 {
		t.Fatalf("duration = %d, want 45", input.Preferences.DurationSeconds)
	}
	if input.DeliveryTargets == nil {
		t.Fatal("explicitly empty delivery targets should remain a non-nil empty slice")
	}
}

func TestMontageCapabilityServiceValidatesInputPolicy(t *testing.T) {
	tests := []struct {
		name  string
		input model.MontageInput
		want  string
	}{
		{name: "unknown pipeline", input: model.MontageInput{Brief: "x", PipelineKey: "missing"}, want: "pipeline_key"},
		{name: "duration above limit", input: model.MontageInput{Brief: "x", PipelineKey: "cinematic", Preferences: model.MontagePreferences{DurationSeconds: 601}}, want: "duration_seconds"},
		{name: "too many assets", input: model.MontageInput{Brief: "x", PipelineKey: "cinematic", SourceAssets: make([]model.MontageAsset, 21)}, want: "source_assets"},
		{name: "talking head requires video", input: model.MontageInput{Brief: "x", PipelineKey: "talking-head", SourceAssets: []model.MontageAsset{{Type: "audio_url"}}}, want: "video source"},
		{name: "clip factory requires media", input: model.MontageInput{Brief: "x", PipelineKey: "clip-factory", SourceAssets: []model.MontageAsset{{Type: "image_url"}}}, want: "video or audio source"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := testMontageCapabilityService().NormalizeAndValidateInput(&tt.input, model.MontageDefaults{})
			if err == nil || !errors.Is(err, ErrMontageInput) || !containsText(err.Error(), tt.want) {
				t.Fatalf("error = %v, want ErrMontageInput containing %q", err, tt.want)
			}
		})
	}
}

func TestMontageCapabilityServiceAcceptsExistingMediaAssetTypes(t *testing.T) {
	tests := []struct {
		name     string
		pipeline string
		asset    model.MontageAsset
	}{
		{
			name:     "video asset",
			pipeline: "talking-head",
			asset:    model.MontageAsset{Type: "video", URL: "https://example.com/talk.mp4"},
		},
		{
			name:     "audio asset",
			pipeline: "clip-factory",
			asset:    model.MontageAsset{Type: "audio", URL: "https://example.com/interview.m4a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := model.MontageInput{
				Brief:        "reuse an existing API asset",
				PipelineKey:  tt.pipeline,
				SourceAssets: []model.MontageAsset{tt.asset},
			}
			if err := testMontageCapabilityService().NormalizeAndValidateInput(&input, model.MontageDefaults{}); err != nil {
				t.Fatalf("NormalizeAndValidateInput error: %v", err)
			}
		})
	}
}

func TestMontageCapabilityServiceRejectsEmptyRequiredMediaAssets(t *testing.T) {
	tests := []struct {
		name  string
		asset model.MontageAsset
	}{
		{name: "missing locator", asset: model.MontageAsset{Type: "video_url"}},
		{name: "blank locator", asset: model.MontageAsset{Type: "video", URL: "   "}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := model.MontageInput{
				Brief:        "talking head",
				PipelineKey:  "talking-head",
				SourceAssets: []model.MontageAsset{tt.asset},
			}
			err := testMontageCapabilityService().NormalizeAndValidateInput(&input, model.MontageDefaults{})
			if err == nil || !errors.Is(err, ErrMontageInput) || !containsText(err.Error(), "video source") {
				t.Fatalf("error = %v, want ErrMontageInput requiring a video source", err)
			}
		})
	}
}

func TestMontageCapabilityServiceValidatesProjectDefaultsWithoutAssets(t *testing.T) {
	svc := testMontageCapabilityService()

	if err := svc.ValidateProjectDefaults(model.MontageDefaults{DefaultPipeline: "talking-head", Preferences: model.MontagePreferences{DurationSeconds: 120}}); err != nil {
		t.Fatalf("valid project defaults error: %v", err)
	}
	for _, defaults := range []model.MontageDefaults{
		{DefaultPipeline: "missing"},
		{DefaultPipeline: "cinematic", Preferences: model.MontagePreferences{DurationSeconds: 601}},
	} {
		if err := svc.ValidateProjectDefaults(defaults); err == nil || !errors.Is(err, ErrProjectMontageDefaults) {
			t.Fatalf("defaults %#v error = %v, want ErrProjectMontageDefaults", defaults, err)
		}
	}
}

func TestMontageCapabilityServiceRejectsProjectDefaultsWhenDisabled(t *testing.T) {
	svc := NewMontageCapabilityService(serverconfig.MontageConfig{Enabled: false, MaxDurationSeconds: 600})

	if err := svc.ValidateProjectDefaults(model.MontageDefaults{}); err == nil || !errors.Is(err, ErrProjectMontageDefaults) {
		t.Fatalf("empty defaults error = %v, want ErrProjectMontageDefaults when Montage is disabled", err)
	}
}

func TestValidateMontageInlineBootstrapBudgetRejectsOversizedMetadata(t *testing.T) {
	input := &model.MontageInput{
		Brief:    "bounded metadata",
		Advanced: map[string]any{"payload": strings.Repeat("x", montageBootstrapInlineReserveBytes)},
	}
	if err := ValidateMontageInlineBootstrapBudget(input, nil, nil, nil); err == nil || !errors.Is(err, ErrMontageInput) {
		t.Fatalf("budget validation error = %v, want ErrMontageInput", err)
	}
}

func TestValidateMontageInlineBootstrapBudgetRejectsOversizedProjectInstructions(t *testing.T) {
	project := &model.Project{Instructions: strings.Repeat("x", montageBootstrapInlineReserveBytes)}
	if err := ValidateMontageInlineBootstrapBudget(&model.MontageInput{Brief: "bounded instructions"}, project, nil, nil); err == nil || !errors.Is(err, ErrMontageInput) {
		t.Fatalf("budget validation error = %v, want ErrMontageInput", err)
	}
}

func TestValidateMontageInlineBootstrapBudgetIncludesInputAttachments(t *testing.T) {
	attachments := []model.EntryAttachment{{Type: "text", FileName: "notes.txt", Text: strings.Repeat("x", montageBootstrapInlineReserveBytes)}}
	if err := ValidateMontageInlineBootstrapBudget(&model.MontageInput{Brief: "bounded attachments"}, nil, nil, nil, attachments); err == nil || !errors.Is(err, ErrMontageInput) {
		t.Fatalf("budget validation error = %v, want ErrMontageInput", err)
	}
}

func containsText(value, part string) bool {
	for i := 0; i+len(part) <= len(value); i++ {
		if value[i:i+len(part)] == part {
			return true
		}
	}
	return false
}

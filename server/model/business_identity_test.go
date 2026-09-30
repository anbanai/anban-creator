package model

import "testing"

func TestBusinessIdentitySetsRejectUndeclaredValues(t *testing.T) {
	for _, platform := range []string{PlatformArticle, PlatformSeednote, PlatformMoments, PlatformEcommerce, PlatformMontage, PlatformWhiteboardAnimation} {
		if !IsProjectPlatform(platform) {
			t.Errorf("IsProjectPlatform(%q) = false", platform)
		}
	}
	if IsProjectPlatform("future-pack-only") {
		t.Fatal("Pack-only value was accepted as a project platform")
	}

	for _, taskType := range []string{PlatformArticle, PlatformSeednote, PlatformMoments, PlatformEcommerce, PlatformMontage, PlatformWhiteboardAnimation, TaskTypeLiveSlicer, TaskTypeViralAnalysis} {
		if !IsTaskType(taskType) {
			t.Errorf("IsTaskType(%q) = false", taskType)
		}
	}
	if IsTaskType("future-pack-only") {
		t.Fatal("Pack-only value was accepted as a task type")
	}
}

func TestWhiteboardAnimationHasPublicPlatformConfig(t *testing.T) {
	config := GetPlatformConfig(PlatformWhiteboardAnimation)
	if config == nil || config.ID != PlatformWhiteboardAnimation || len(config.Fields) == 0 {
		t.Fatalf("whiteboard-animation platform config = %#v, want public form metadata", config)
	}
	if config.Label != "白板动画" {
		t.Fatalf("whiteboard-animation platform label = %q, want 白板动画", config.Label)
	}
}

func TestAdminOnlyProjectPlatforms(t *testing.T) {
	for _, platform := range []string{PlatformMoments, PlatformEcommerce, PlatformHypit} {
		if !IsAdminOnlyProjectPlatform(platform) {
			t.Errorf("IsAdminOnlyProjectPlatform(%q) = false", platform)
		}
	}
	for _, platform := range []string{PlatformArticle, PlatformSeednote, PlatformMontage} {
		if IsAdminOnlyProjectPlatform(platform) {
			t.Errorf("IsAdminOnlyProjectPlatform(%q) = true", platform)
		}
	}
}

func TestMontageHasPublicPlatformConfig(t *testing.T) {
	config := GetPlatformConfig(PlatformMontage)
	if config == nil || config.ID != PlatformMontage || len(config.Fields) == 0 {
		t.Fatalf("Montage platform config = %#v, want public form metadata", config)
	}
	if config.Label != "视频生成" {
		t.Fatalf("Montage platform label = %q, want 视频生成", config.Label)
	}
}

func TestSeednoteProjectConfigHasNoPublishingAuthor(t *testing.T) {
	config := GetPlatformConfig(PlatformSeednote)
	if config == nil {
		t.Fatal("seednote platform config is missing")
	}
	for _, field := range config.Fields {
		if field.Key == "author" {
			t.Fatal("seednote platform config must not expose a publishing author")
		}
	}
}

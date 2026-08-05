package model

import "testing"

func TestBusinessIdentitySetsRejectUndeclaredValues(t *testing.T) {
	for _, platform := range []string{PlatformArticle, PlatformSeednote, PlatformMoments, PlatformEcommerce, PlatformMontage} {
		if !IsProjectPlatform(platform) {
			t.Errorf("IsProjectPlatform(%q) = false", platform)
		}
	}
	if IsProjectPlatform("future-pack-only") {
		t.Fatal("Pack-only value was accepted as a project platform")
	}

	for _, taskType := range []string{PlatformArticle, PlatformSeednote, PlatformMoments, PlatformEcommerce, PlatformMontage, TaskTypeLiveSlicer, TaskTypeViralAnalysis} {
		if !IsTaskType(taskType) {
			t.Errorf("IsTaskType(%q) = false", taskType)
		}
	}
	if IsTaskType("future-pack-only") {
		t.Fatal("Pack-only value was accepted as a task type")
	}
}

func TestAdminOnlyProjectPlatforms(t *testing.T) {
	for _, platform := range []string{PlatformMoments, PlatformEcommerce, PlatformMontage} {
		if !IsAdminOnlyProjectPlatform(platform) {
			t.Errorf("IsAdminOnlyProjectPlatform(%q) = false", platform)
		}
	}
	for _, platform := range []string{PlatformArticle, PlatformSeednote} {
		if IsAdminOnlyProjectPlatform(platform) {
			t.Errorf("IsAdminOnlyProjectPlatform(%q) = true", platform)
		}
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

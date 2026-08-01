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

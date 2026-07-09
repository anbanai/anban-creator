package model

import "testing"

func TestOpenMontageDefaultsSurviveProjectSnapshotRoundTrip(t *testing.T) {
	project := &Project{
		Name:     "Launch Clips",
		Platform: PlatformOpenMontage,
	}
	project.SetOpenMontageDefaults(OpenMontageDefaults{
		DefaultPipeline: "social-short",
		Preferences: OpenMontagePreferences{
			AspectRatio:     "9:16",
			DurationSeconds: 45,
			SubtitleMode:    "burned-in",
		},
		AssetGuidance:   "Use the latest product shots",
		DeliveryTargets: []string{"wechat", "seednote"},
	})

	snapshot := SnapshotProject(project)
	restored := ProjectFromSnapshot(&Project{ID: "project-1"}, snapshot)

	got := restored.OpenMontageDefaults.Data()
	if got.DefaultPipeline != "social-short" {
		t.Fatalf("DefaultPipeline = %q, want social-short", got.DefaultPipeline)
	}
	if got.Preferences.AspectRatio != "9:16" || got.Preferences.DurationSeconds != 45 {
		t.Fatalf("Preferences = %#v, want aspect 9:16 and duration 45", got.Preferences)
	}
	if got.AssetGuidance != "Use the latest product shots" {
		t.Fatalf("AssetGuidance = %q", got.AssetGuidance)
	}
	if len(got.DeliveryTargets) != 2 || got.DeliveryTargets[0] != "wechat" || got.DeliveryTargets[1] != "seednote" {
		t.Fatalf("DeliveryTargets = %#v", got.DeliveryTargets)
	}
}

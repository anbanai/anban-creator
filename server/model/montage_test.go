package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMontageDefaultsSurviveProjectSnapshotRoundTrip(t *testing.T) {
	project := &Project{
		Name:     "Launch Clips",
		Platform: PlatformMontage,
	}
	project.SetMontageDefaults(MontageDefaults{
		DefaultPipeline: "social-short",
		Preferences: MontagePreferences{
			DurationSeconds: 45,
			SubtitleMode:    "burned-in",
		},
		AssetGuidance:   "Use the latest product shots",
		DeliveryTargets: []string{"wechat", "seednote"},
	})

	snapshot := SnapshotProject(project)
	restored := ProjectFromSnapshot(&Project{ID: "project-1"}, snapshot)

	got := restored.MontageDefaults.Data()
	if got.DefaultPipeline != "social-short" {
		t.Fatalf("DefaultPipeline = %q, want social-short", got.DefaultPipeline)
	}
	if got.Preferences.DurationSeconds != 45 {
		t.Fatalf("Preferences = %#v, want duration 45", got.Preferences)
	}
	if got.AssetGuidance != "Use the latest product shots" {
		t.Fatalf("AssetGuidance = %q", got.AssetGuidance)
	}
	if len(got.DeliveryTargets) != 2 || got.DeliveryTargets[0] != "wechat" || got.DeliveryTargets[1] != "seednote" {
		t.Fatalf("DeliveryTargets = %#v", got.DeliveryTargets)
	}
}

func TestMontagePreferencesDiscardLegacyAspectRatio(t *testing.T) {
	var input MontageInput
	if err := json.Unmarshal([]byte(`{"brief":"launch","preferences":{"aspect_ratio":"16:9","duration_seconds":30}}`), &input); err != nil {
		t.Fatalf("unmarshal legacy montage input: %v", err)
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal montage input: %v", err)
	}
	if strings.Contains(string(data), "aspect_ratio") {
		t.Fatalf("montage input retained a second ratio source: %s", data)
	}
}

func TestAgentConfigSurvivesProjectSnapshotRoundTripWithoutAliasing(t *testing.T) {
	project := &Project{Name: "Research", Platform: PlatformArticle}
	project.SetAgentConfig(map[string]any{
		"audience": "developers",
		"filters":  map[string]any{"language": "zh-CN"},
	})

	snapshot := SnapshotProject(project)
	project.AgentConfig.Data()["audience"] = "founders"
	project.AgentConfig.Data()["filters"].(map[string]any)["language"] = "en-US"
	restored := ProjectFromSnapshot(project, snapshot)

	if got := restored.AgentConfig.Data()["audience"]; got != "developers" {
		t.Fatalf("audience = %v, want frozen developers", got)
	}
	filters := restored.AgentConfig.Data()["filters"].(map[string]any)
	if got := filters["language"]; got != "zh-CN" {
		t.Fatalf("filters.language = %v, want frozen zh-CN", got)
	}
}

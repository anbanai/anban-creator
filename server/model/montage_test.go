package model

import (
	"encoding/json"
	"strings"
	"testing"

	"gorm.io/datatypes"
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

func TestProjectSnapshotDerivesPlatformFromCanonicalChannel(t *testing.T) {
	snapshot := ProjectSnapshot{
		ProjectName: "Frozen WeChat",
		AgentID:     AgentIDArticle,
		Channel:     ChannelArticle,
	}
	restored := ProjectFromSnapshot(&Project{Platform: PlatformSeednote}, snapshot)
	if restored.Platform != PlatformWechat {
		t.Fatalf("platform = %q, want %q", restored.Platform, PlatformWechat)
	}
}

func TestProjectSnapshotExcludesUnconfirmedProfileButKeepsConfirmedProfile(t *testing.T) {
	draft := NewProjectProfile()
	draft.Dimensions.Identity.Content["private_draft"] = "waiting for confirmation"
	project := &Project{ID: "project-1", Name: "Profile snapshot", Profile: datatypes.NewJSONType(draft)}

	snapshot := SnapshotProject(project)
	if snapshot.Profile.IsConfirmed() || snapshot.Profile.Dimensions.Identity.Content["private_draft"] != nil {
		t.Fatalf("unconfirmed profile entered task snapshot: %+v", snapshot.Profile)
	}

	confirmed := draft
	confirmed.Status = ProfileStatusConfirmed
	confirmed.Version = 1
	confirmed.Dimensions.Identity.Content["confirmed"] = "usable context"
	project.Profile = datatypes.NewJSONType(confirmed)
	snapshot = SnapshotProject(project)
	if !snapshot.Profile.IsConfirmed() || snapshot.Profile.Dimensions.Identity.Content["confirmed"] != "usable context" {
		t.Fatalf("confirmed profile was not frozen into task snapshot: %+v", snapshot.Profile)
	}
}

package model

import (
	"strings"
	"testing"
)

func TestVideoEditInputNormalizesDefaultsAndRequiresFinalizedAssets(t *testing.T) {
	input := VideoEditInput{Brief: "  cut the interview  ", SourceAssets: []VideoEditAsset{{Type: "video", AssetID: "asset-1"}}}
	input.Normalize(VideoEditDefaults{Preferences: VideoEditPreferences{QualityTier: "balanced", SubtitleMode: "required", Style: "fast", MaxDuration: 90}, DeliveryTargets: []string{"final_video"}})
	if input.Brief != "cut the interview" || input.Preferences.QualityTier != "balanced" || input.Preferences.SubtitleMode != "required" || input.Preferences.MaxDuration != 90 {
		t.Fatalf("normalized input = %#v", input)
	}
	if err := input.Validate(4, 180); err != nil {
		t.Fatalf("Validate(finalized upload): %v", err)
	}
	input.SourceAssets[0].URL = "https://example.com/video.mp4"
	if err := input.Validate(4, 180); err == nil || !strings.Contains(err.Error(), "finalized") {
		t.Fatalf("Validate(remote) = %v, want unfinalized URL rejection", err)
	}
}

func TestVideoEditInputDefaultsSubtitleOff(t *testing.T) {
	input := VideoEditInput{Brief: "make a short", SourceAssets: []VideoEditAsset{{Type: "audio", AssetID: "asset-1"}}}
	input.Normalize(VideoEditDefaults{})
	if input.Preferences.QualityTier != "effective" || input.Preferences.SubtitleMode != "off" {
		t.Fatalf("defaults = %#v", input.Preferences)
	}
}

func TestVideoEditInputRejectsRuntimeWorkspacePath(t *testing.T) {
	input := VideoEditInput{Brief: "make a short", SourceAssets: []VideoEditAsset{{Type: "video", AssetID: "asset-1", WorkspacePath: ".anban-creator/video-edit-assets/clip.mp4"}}}
	input.Normalize(VideoEditDefaults{})
	if err := input.Validate(20, 1800); err == nil || !strings.Contains(err.Error(), "workspace_path is Server-generated") {
		t.Fatalf("Validate() error = %v, want server-generated workspace path rejection", err)
	}
}

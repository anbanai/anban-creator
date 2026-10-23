package model

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/datatypes"
)

// VideoEditInput is the immutable, user-facing input for the video-edit Agent.
// Source assets are materialized by the Server into the task workspace; callers
// cannot provide arbitrary local paths or remote URLs.
type VideoEditInput struct {
	Brief           string               `json:"brief,omitempty"`
	SourceAssets    []VideoEditAsset     `json:"source_assets,omitempty"`
	Preferences     VideoEditPreferences `json:"preferences,omitempty"`
	DeliveryTargets []string             `json:"delivery_targets,omitempty"`
}

type VideoEditAsset struct {
	Type          string `json:"type"`
	AssetID       string `json:"asset_id,omitempty"`
	URL           string `json:"url,omitempty"`
	WorkspacePath string `json:"workspace_path,omitempty"`
	FileName      string `json:"file_name,omitempty"`
	MimeType      string `json:"mime_type,omitempty"`
	FileSize      int64  `json:"file_size,omitempty"`
}

type VideoEditPreferences struct {
	QualityTier  string `json:"quality_tier,omitempty" yaml:"quality_tier"`
	SubtitleMode string `json:"subtitle_mode,omitempty" yaml:"subtitle_mode"`
	Style        string `json:"style,omitempty" yaml:"style"`
	MaxDuration  int64  `json:"max_duration_seconds,omitempty" yaml:"max_duration_seconds"`
}

type VideoEditDefaults struct {
	Preferences     VideoEditPreferences `json:"preferences,omitempty" yaml:"preferences"`
	DeliveryTargets []string             `json:"delivery_targets,omitempty" yaml:"delivery_targets"`
}

// ResolveVideoEditDefaults applies project-level choices over Server policy.
func ResolveVideoEditDefaults(project, server VideoEditDefaults) VideoEditDefaults {
	resolved := server
	if strings.TrimSpace(project.Preferences.QualityTier) != "" {
		resolved.Preferences.QualityTier = project.Preferences.QualityTier
	}
	if strings.TrimSpace(project.Preferences.SubtitleMode) != "" {
		resolved.Preferences.SubtitleMode = project.Preferences.SubtitleMode
	}
	if strings.TrimSpace(project.Preferences.Style) != "" {
		resolved.Preferences.Style = project.Preferences.Style
	}
	if project.Preferences.MaxDuration != 0 {
		resolved.Preferences.MaxDuration = project.Preferences.MaxDuration
	}
	if project.DeliveryTargets != nil {
		resolved.DeliveryTargets = append([]string(nil), project.DeliveryTargets...)
	}
	return resolved
}

func (t *Task) SetVideoEditInput(input VideoEditInput) {
	t.VideoEditInput = datatypes.NewJSONType(input)
}

func (p *Project) SetVideoEditDefaults(defaults VideoEditDefaults) {
	p.VideoEditDefaults = datatypes.NewJSONType(defaults)
}

func (t *Task) VideoEditTimeout() time.Duration {
	if t != nil && t.VideoEditTimeoutMinutes > 0 {
		return time.Duration(t.VideoEditTimeoutMinutes) * time.Minute
	}
	return 90 * time.Minute
}

func (input *VideoEditInput) Normalize(defaults VideoEditDefaults) {
	if input == nil {
		return
	}
	input.Brief = strings.TrimSpace(input.Brief)
	if strings.TrimSpace(input.Preferences.QualityTier) == "" {
		input.Preferences.QualityTier = strings.TrimSpace(defaults.Preferences.QualityTier)
	}
	if input.Preferences.QualityTier == "" {
		input.Preferences.QualityTier = "effective"
	}
	if strings.TrimSpace(input.Preferences.SubtitleMode) == "" {
		input.Preferences.SubtitleMode = strings.TrimSpace(defaults.Preferences.SubtitleMode)
	}
	if input.Preferences.SubtitleMode == "" {
		input.Preferences.SubtitleMode = "off"
	}
	if strings.TrimSpace(input.Preferences.Style) == "" {
		input.Preferences.Style = strings.TrimSpace(defaults.Preferences.Style)
	}
	if input.Preferences.MaxDuration == 0 {
		input.Preferences.MaxDuration = defaults.Preferences.MaxDuration
	}
	if input.DeliveryTargets == nil {
		input.DeliveryTargets = append([]string(nil), defaults.DeliveryTargets...)
	}
	if input.DeliveryTargets == nil {
		input.DeliveryTargets = []string{"final_video", "edit_decision_list", "strategy", "delivery_manifest", "quality_evidence", "content_tags"}
	}
}

func (input VideoEditInput) Validate(maxAssets int, maxDuration int64) error {
	if strings.TrimSpace(input.Brief) == "" {
		return fmt.Errorf("video_edit_input.brief is required")
	}
	if len(input.SourceAssets) == 0 {
		return fmt.Errorf("video_edit_input.source_assets requires at least one uploaded asset")
	}
	if maxAssets > 0 && len(input.SourceAssets) > maxAssets {
		return fmt.Errorf("video_edit_input.source_assets cannot exceed %d items", maxAssets)
	}
	if input.Preferences.QualityTier != "effective" && input.Preferences.QualityTier != "balanced" && input.Preferences.QualityTier != "quality" {
		return fmt.Errorf("video_edit_input.preferences.quality_tier must be effective, balanced, or quality")
	}
	if input.Preferences.SubtitleMode != "off" && input.Preferences.SubtitleMode != "required" {
		return fmt.Errorf("video_edit_input.preferences.subtitle_mode must be off or required")
	}
	if maxDuration > 0 && (input.Preferences.MaxDuration < 0 || input.Preferences.MaxDuration > maxDuration) {
		return fmt.Errorf("video_edit_input.preferences.max_duration_seconds must be between 0 and %d", maxDuration)
	}
	for i, asset := range input.SourceAssets {
		typ := strings.ToLower(strings.TrimSpace(asset.Type))
		if typ != "video" && typ != "audio" && typ != "image" {
			return fmt.Errorf("video_edit_input.source_assets[%d].type must be video, audio, or image", i)
		}
		if strings.TrimSpace(asset.URL) != "" {
			return fmt.Errorf("video_edit_input.source_assets[%d].url must be finalized to a Server-owned asset", i)
		}
		if strings.TrimSpace(asset.WorkspacePath) != "" {
			return fmt.Errorf("video_edit_input.source_assets[%d].workspace_path is Server-generated", i)
		}
		if strings.TrimSpace(asset.AssetID) == "" {
			return fmt.Errorf("video_edit_input.source_assets[%d].asset_id must reference an uploaded Anban asset", i)
		}
	}
	return nil
}

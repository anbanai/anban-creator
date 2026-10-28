package model

import (
	"fmt"
	"gorm.io/datatypes"
	"strings"
)

func (t *Task) SetTalkingHeadEditorInput(input TalkingHeadEditorInput) {
	if t != nil {
		t.TalkingHeadEditorInput = datatypes.NewJSONType(input)
	}
}

type TalkingHeadEditorAsset struct {
	Role     string `json:"role"`
	AssetID  string `json:"asset_id,omitempty"`
	URL      string `json:"url,omitempty"`
	FileName string `json:"file_name,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	FileSize int64  `json:"file_size,omitempty"`
}

type TalkingHeadEditorPreferences struct {
	QualityTier           string `json:"quality_tier,omitempty"`
	SubtitleMode          string `json:"subtitle_mode,omitempty"`
	TargetDurationSeconds int64  `json:"target_duration_seconds,omitempty"`
	AspectRatio           string `json:"aspect_ratio,omitempty"`
	BrollMode             string `json:"broll_mode,omitempty"`
	SoundMode             string `json:"sound_mode,omitempty"`
	Style                 string `json:"style,omitempty"`
}

type TalkingHeadEditorInput struct {
	Brief            string                       `json:"brief"`
	PrimaryVideo     TalkingHeadEditorAsset       `json:"primary_video"`
	SupportingAssets []TalkingHeadEditorAsset     `json:"supporting_assets,omitempty"`
	ReferenceCut     *TalkingHeadEditorAsset      `json:"reference_cut,omitempty"`
	Preferences      TalkingHeadEditorPreferences `json:"preferences,omitempty"`
	DeliveryTargets  []string                     `json:"delivery_targets,omitempty"`
}

type TalkingHeadEditorDefaults struct {
	Preferences     TalkingHeadEditorPreferences `json:"preferences,omitempty"`
	DeliveryTargets []string                     `json:"delivery_targets,omitempty"`
}

func ResolveTalkingHeadEditorDefaults(project, server TalkingHeadEditorDefaults) TalkingHeadEditorDefaults {
	resolved := server
	if project.Preferences.QualityTier != "" {
		resolved.Preferences.QualityTier = project.Preferences.QualityTier
	}
	if project.Preferences.SubtitleMode != "" {
		resolved.Preferences.SubtitleMode = project.Preferences.SubtitleMode
	}
	if project.Preferences.TargetDurationSeconds != 0 {
		resolved.Preferences.TargetDurationSeconds = project.Preferences.TargetDurationSeconds
	}
	if project.Preferences.AspectRatio != "" {
		resolved.Preferences.AspectRatio = project.Preferences.AspectRatio
	}
	if project.Preferences.BrollMode != "" {
		resolved.Preferences.BrollMode = project.Preferences.BrollMode
	}
	if project.Preferences.SoundMode != "" {
		resolved.Preferences.SoundMode = project.Preferences.SoundMode
	}
	if project.Preferences.Style != "" {
		resolved.Preferences.Style = project.Preferences.Style
	}
	if len(project.DeliveryTargets) > 0 {
		resolved.DeliveryTargets = append([]string(nil), project.DeliveryTargets...)
	}
	return resolved
}

func (input *TalkingHeadEditorInput) Normalize(defaults TalkingHeadEditorDefaults) {
	if input == nil {
		return
	}
	if strings.TrimSpace(input.Preferences.QualityTier) == "" {
		input.Preferences.QualityTier = defaults.Preferences.QualityTier
	}
	if strings.TrimSpace(input.Preferences.SubtitleMode) == "" {
		input.Preferences.SubtitleMode = defaults.Preferences.SubtitleMode
	}
	if input.Preferences.TargetDurationSeconds == 0 {
		input.Preferences.TargetDurationSeconds = defaults.Preferences.TargetDurationSeconds
	}
	if strings.TrimSpace(input.Preferences.AspectRatio) == "" {
		input.Preferences.AspectRatio = defaults.Preferences.AspectRatio
	}
	if strings.TrimSpace(input.Preferences.BrollMode) == "" {
		input.Preferences.BrollMode = defaults.Preferences.BrollMode
	}
	if strings.TrimSpace(input.Preferences.SoundMode) == "" {
		input.Preferences.SoundMode = defaults.Preferences.SoundMode
	}
	if strings.TrimSpace(input.Preferences.Style) == "" {
		input.Preferences.Style = defaults.Preferences.Style
	}
	if len(input.DeliveryTargets) == 0 {
		input.DeliveryTargets = append([]string(nil), defaults.DeliveryTargets...)
	}
}

func (input TalkingHeadEditorInput) Validate(maxAssets int, maxDuration int64) error {
	if strings.TrimSpace(input.Brief) == "" {
		return fmt.Errorf("talking_head_editor_input.brief is required")
	}
	if strings.TrimSpace(input.PrimaryVideo.AssetID) == "" {
		return fmt.Errorf("talking_head_editor_input.primary_video.asset_id is required")
	}
	if input.PrimaryVideo.Role != "" && input.PrimaryVideo.Role != "reference" {
		return fmt.Errorf("talking_head_editor_input.primary_video.role must be reference")
	}
	if input.PrimaryVideo.MimeType != "" && !strings.HasPrefix(strings.ToLower(input.PrimaryVideo.MimeType), "video/") {
		return fmt.Errorf("talking_head_editor_input.primary_video must be a video")
	}
	if maxAssets > 0 && len(input.SupportingAssets) > maxAssets {
		return fmt.Errorf("talking_head_editor_input.supporting_assets cannot exceed %d items", maxAssets)
	}
	if input.Preferences.QualityTier != "" && input.Preferences.QualityTier != "effective" && input.Preferences.QualityTier != "balanced" && input.Preferences.QualityTier != "quality" {
		return fmt.Errorf("talking_head_editor_input.preferences.quality_tier is invalid")
	}
	if input.Preferences.SubtitleMode != "" && input.Preferences.SubtitleMode != "off" && input.Preferences.SubtitleMode != "required" {
		return fmt.Errorf("talking_head_editor_input.preferences.subtitle_mode is invalid")
	}
	if maxDuration > 0 && input.Preferences.TargetDurationSeconds > maxDuration {
		return fmt.Errorf("talking_head_editor_input.preferences.target_duration_seconds must be <= %d", maxDuration)
	}
	for i, asset := range input.SupportingAssets {
		if asset.Role != "broll" && asset.Role != "sfx" && asset.Role != "bgm" && asset.Role != "reference" {
			return fmt.Errorf("talking_head_editor_input.supporting_assets[%d].role is invalid", i)
		}
		if strings.TrimSpace(asset.AssetID) == "" {
			return fmt.Errorf("talking_head_editor_input.supporting_assets[%d].asset_id is required", i)
		}
		if asset.MimeType != "" {
			mimeType := strings.ToLower(asset.MimeType)
			if asset.Role == "sfx" || asset.Role == "bgm" {
				if !strings.HasPrefix(mimeType, "audio/") {
					return fmt.Errorf("talking_head_editor_input.supporting_assets[%d] must be audio", i)
				}
			} else if !strings.HasPrefix(mimeType, "video/") && !strings.HasPrefix(mimeType, "image/") {
				return fmt.Errorf("talking_head_editor_input.supporting_assets[%d] must be video or image", i)
			}
		}
	}
	return nil
}

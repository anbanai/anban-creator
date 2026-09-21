package model

import (
	"encoding/json"
	"gorm.io/datatypes"
	"time"
)

type HypitAsset struct {
	Type       string `json:"type"`
	URL        string `json:"url,omitempty"`
	TaskFileID string `json:"task_file_id,omitempty"`
	FileName   string `json:"file_name,omitempty"`
	MimeType   string `json:"mime_type,omitempty"`
	FileSize   int64  `json:"file_size,omitempty"`
}
type HypitPreferences struct {
	DurationSeconds *int64 `json:"duration_seconds,omitempty"`
	AspectRatio     string `json:"aspect_ratio,omitempty"`
	Language        string `json:"language,omitempty"`
}
type HypitInput struct {
	Brief        string           `json:"brief"`
	Reference    *HypitAsset      `json:"reference,omitempty"`
	SourceAssets []HypitAsset     `json:"source_assets,omitempty"`
	Preferences  HypitPreferences `json:"preferences,omitempty"`
}
type HypitDefaults struct {
	Preferences   HypitPreferences `json:"preferences"`
	AssetGuidance string           `json:"asset_guidance,omitempty"`
}

func (t *Task) SetHypitInput(v HypitInput)          { t.HypitInput = datatypes.NewJSONType(v) }
func (p *Plan) SetHypitInput(v HypitInput)          { p.HypitInput = datatypes.NewJSONType(v) }
func (p *Project) SetHypitDefaults(v HypitDefaults) { p.HypitDefaults = datatypes.NewJSONType(v) }
func IsHypitPlatform(v string) bool                 { return v == PlatformHypit }

// HypitTimeout uses immutable admission limits, never current deployment defaults.
func (t *Task) HypitTimeout() time.Duration {
	var snapshot struct {
		Limits struct {
			TimeoutMinutes int `json:"timeout_minutes"`
		} `json:"limits"`
	}
	if t != nil && json.Unmarshal(t.HypitRuntimeSnapshot, &snapshot) == nil && snapshot.Limits.TimeoutMinutes > 0 && snapshot.Limits.TimeoutMinutes <= 90 {
		return time.Duration(snapshot.Limits.TimeoutMinutes) * time.Minute
	}
	return 90 * time.Minute
}

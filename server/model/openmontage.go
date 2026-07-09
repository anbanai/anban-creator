package model

import "gorm.io/datatypes"

type OpenMontageInput struct {
	Brief           string                 `json:"brief,omitempty"`
	PipelineKey     string                 `json:"pipeline_key,omitempty"`
	SourceAssets    []OpenMontageAsset     `json:"source_assets,omitempty"`
	Preferences     OpenMontagePreferences `json:"preferences,omitempty"`
	DeliveryTargets []string               `json:"delivery_targets,omitempty"`
	Advanced        map[string]any         `json:"advanced,omitempty"`
}

type OpenMontageAsset struct {
	Type       string `json:"type"`
	URL        string `json:"url,omitempty"`
	TaskFileID string `json:"task_file_id,omitempty"`
	Text       string `json:"text,omitempty"`
	FileName   string `json:"file_name,omitempty"`
	MimeType   string `json:"mime_type,omitempty"`
	FileSize   int64  `json:"file_size,omitempty"`
}

type OpenMontagePreferences struct {
	AspectRatio     string `json:"aspect_ratio,omitempty"`
	DurationSeconds int64  `json:"duration_seconds,omitempty"`
	Style           string `json:"style,omitempty"`
	MusicPrompt     string `json:"music_prompt,omitempty"`
	SubtitleMode    string `json:"subtitle_mode,omitempty"`
	VoiceoverMode   string `json:"voiceover_mode,omitempty"`
}

type OpenMontageDefaults struct {
	DefaultPipeline string                 `json:"default_pipeline,omitempty"`
	Preferences     OpenMontagePreferences `json:"preferences,omitempty"`
	AssetGuidance   string                 `json:"asset_guidance,omitempty"`
	DeliveryTargets []string               `json:"delivery_targets,omitempty"`
}

func (t *Task) SetOpenMontageInput(input OpenMontageInput) {
	t.OpenMontageInput = datatypes.NewJSONType(input)
}

func (p *Plan) SetOpenMontageInput(input OpenMontageInput) {
	p.OpenMontageInput = datatypes.NewJSONType(input)
}

func (p *Project) SetOpenMontageDefaults(defaults OpenMontageDefaults) {
	p.OpenMontageDefaults = datatypes.NewJSONType(defaults)
}

package service

import (
	"fmt"
	"slices"
	"strings"

	serverconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
)

type MontageSourceRequirement string

const (
	MontageSourceRequirementOptional     MontageSourceRequirement = "optional"
	MontageSourceRequirementVideo        MontageSourceRequirement = "video"
	MontageSourceRequirementVideoOrAudio MontageSourceRequirement = "video_or_audio"
)

type MontageOutputMode string

const (
	MontageOutputModeSingle   MontageOutputMode = "single"
	MontageOutputModeMultiple MontageOutputMode = "multiple"
)

type MontagePipelineCapability struct {
	Key                        string                   `json:"key"`
	DisplayName                string                   `json:"display_name"`
	Description                string                   `json:"description"`
	BestFor                    []string                 `json:"best_for"`
	SourceHint                 string                   `json:"source_hint"`
	OutputHint                 string                   `json:"output_hint"`
	SourceRequirement          MontageSourceRequirement `json:"source_requirement"`
	OutputMode                 MontageOutputMode        `json:"output_mode"`
	RecommendedDurationSeconds int64                    `json:"recommended_duration_seconds"`
}

type MontageCapabilityCatalog struct {
	Enabled            bool                        `json:"enabled"`
	DefaultPipeline    string                      `json:"default_pipeline"`
	MaxDurationSeconds int64                       `json:"max_duration_seconds"`
	MaxAssets          int                         `json:"max_assets"`
	Items              []MontagePipelineCapability `json:"items"`
}

type MontageCapabilityService struct {
	config serverconfig.MontageConfig
}

func NewMontageCapabilityService(cfg serverconfig.MontageConfig) *MontageCapabilityService {
	return &MontageCapabilityService{config: cfg}
}

var montagePipelineCapabilities = map[string]MontagePipelineCapability{
	"cinematic": {
		Key: "cinematic", DisplayName: "电影感制作", Description: "品牌片、预告片与情绪叙事",
		BestFor:    []string{"品牌发布", "概念预告", "氛围短片"},
		SourceHint: "可使用视频、图片，也可仅根据创意说明生成", OutputHint: "一条完整成片",
		SourceRequirement: MontageSourceRequirementOptional, OutputMode: MontageOutputModeSingle, RecommendedDurationSeconds: 30,
	},
	"talking-head": {
		Key: "talking-head", DisplayName: "口播精剪", Description: "人物讲解、访谈与课程内容",
		BestFor:    []string{"人物口播", "课程讲解", "采访精剪"},
		SourceHint: "需要一段包含人物讲话的原始视频", OutputHint: "一条带字幕的精剪视频",
		SourceRequirement: MontageSourceRequirementVideo, OutputMode: MontageOutputModeSingle, RecommendedDurationSeconds: 60,
	},
	"screen-demo": {
		Key: "screen-demo", DisplayName: "屏幕演示", Description: "产品教程、软件操作与终端流程",
		BestFor:    []string{"产品演示", "使用教程", "终端操作"},
		SourceHint: "可上传屏幕录制，或在说明中给出可复现的操作步骤", OutputHint: "一条清晰的演示视频",
		SourceRequirement: MontageSourceRequirementOptional, OutputMode: MontageOutputModeSingle, RecommendedDurationSeconds: 60,
	},
	"clip-factory": {
		Key: "clip-factory", DisplayName: "长视频拆条", Description: "从直播、播客或访谈中提炼短视频",
		BestFor:    []string{"直播切片", "播客拆条", "访谈精选"},
		SourceHint: "需要一段长视频或音频作为拆条来源", OutputHint: "多条可独立发布的短视频",
		SourceRequirement: MontageSourceRequirementVideoOrAudio, OutputMode: MontageOutputModeMultiple, RecommendedDurationSeconds: 45,
	},
}

func customMontagePipelineCapability(key string) MontagePipelineCapability {
	return MontagePipelineCapability{
		Key: key, DisplayName: key, Description: "自定义视频工作流",
		BestFor: []string{"团队自定义制作流程"}, SourceHint: "根据工作流要求添加图片、视频或音频素材",
		OutputHint: "按自定义工作流生成视频", SourceRequirement: MontageSourceRequirementOptional,
		OutputMode: MontageOutputModeSingle, RecommendedDurationSeconds: 30,
	}
}

func (s *MontageCapabilityService) Catalog() MontageCapabilityCatalog {
	catalog := MontageCapabilityCatalog{
		Enabled: s.config.Enabled, DefaultPipeline: strings.TrimSpace(s.config.DefaultPipeline),
		MaxDurationSeconds: s.config.MaxDurationSeconds, MaxAssets: s.config.MaxAssets,
		Items: []MontagePipelineCapability{},
	}
	if !s.config.Enabled {
		return catalog
	}
	for _, rawKey := range s.config.AllowedPipelines {
		key := strings.TrimSpace(rawKey)
		if key == "" {
			continue
		}
		item, ok := montagePipelineCapabilities[key]
		if !ok {
			item = customMontagePipelineCapability(key)
		}
		item.BestFor = slices.Clone(item.BestFor)
		catalog.Items = append(catalog.Items, item)
	}
	return catalog
}

func (s *MontageCapabilityService) NormalizeAndValidateInput(input *model.MontageInput, defaults model.MontageDefaults) error {
	if input == nil {
		return fmt.Errorf("%w: montage_input is required", ErrMontageInput)
	}
	input.Brief = strings.TrimSpace(input.Brief)
	input.PipelineKey = strings.TrimSpace(input.PipelineKey)
	if input.PipelineKey == "" {
		input.PipelineKey = strings.TrimSpace(defaults.DefaultPipeline)
	}
	if input.PipelineKey == "" {
		input.PipelineKey = strings.TrimSpace(s.config.DefaultPipeline)
	}
	if input.Preferences.DurationSeconds == 0 {
		input.Preferences.DurationSeconds = defaults.Preferences.DurationSeconds
	}
	if input.Preferences.Style == "" {
		input.Preferences.Style = defaults.Preferences.Style
	}
	if input.Preferences.MusicPrompt == "" {
		input.Preferences.MusicPrompt = defaults.Preferences.MusicPrompt
	}
	if input.Preferences.SubtitleMode == "" {
		input.Preferences.SubtitleMode = defaults.Preferences.SubtitleMode
	}
	if input.Preferences.VoiceoverMode == "" {
		input.Preferences.VoiceoverMode = defaults.Preferences.VoiceoverMode
	}
	if input.DeliveryTargets == nil {
		input.DeliveryTargets = slices.Clone(defaults.DeliveryTargets)
		if input.DeliveryTargets == nil {
			input.DeliveryTargets = []string{}
		}
	}

	capability, allowed := s.pipeline(input.PipelineKey)
	if input.Preferences.DurationSeconds == 0 {
		input.Preferences.DurationSeconds = capability.RecommendedDurationSeconds
	}
	if !s.config.Enabled {
		return fmt.Errorf("%w: montage is disabled", ErrMontageInput)
	}
	if !allowed {
		return fmt.Errorf("%w: pipeline_key %q is not allowed", ErrMontageInput, input.PipelineKey)
	}
	if input.Preferences.DurationSeconds < 1 || input.Preferences.DurationSeconds > s.config.MaxDurationSeconds {
		return fmt.Errorf("%w: duration_seconds must be between 1 and %d", ErrMontageInput, s.config.MaxDurationSeconds)
	}
	if len(input.SourceAssets) > s.config.MaxAssets {
		return fmt.Errorf("%w: source_assets cannot exceed %d items", ErrMontageInput, s.config.MaxAssets)
	}
	if err := validateMontageRequiredSources(capability.SourceRequirement, input.SourceAssets); err != nil {
		return fmt.Errorf("%w: %v", ErrMontageInput, err)
	}
	return nil
}

func (s *MontageCapabilityService) ValidateProjectDefaults(defaults model.MontageDefaults) error {
	key := strings.TrimSpace(defaults.DefaultPipeline)
	if key != "" {
		if !s.config.Enabled {
			return fmt.Errorf("%w: montage is disabled", ErrProjectMontageDefaults)
		}
		if _, allowed := s.pipeline(key); !allowed {
			return fmt.Errorf("%w: default_pipeline %q is not allowed", ErrProjectMontageDefaults, key)
		}
	}
	duration := defaults.Preferences.DurationSeconds
	if duration < 0 || duration > s.config.MaxDurationSeconds {
		return fmt.Errorf("%w: montage duration_seconds must be between 0 and %d", ErrProjectMontageDefaults, s.config.MaxDurationSeconds)
	}
	return nil
}

func (s *MontageCapabilityService) pipeline(key string) (MontagePipelineCapability, bool) {
	key = strings.TrimSpace(key)
	if !slices.Contains(s.config.AllowedPipelines, key) {
		return customMontagePipelineCapability(key), false
	}
	item, ok := montagePipelineCapabilities[key]
	if !ok {
		item = customMontagePipelineCapability(key)
	}
	return item, true
}

func validateMontageRequiredSources(requirement MontageSourceRequirement, assets []model.MontageAsset) error {
	hasVideo, hasAudio := false, false
	for _, asset := range assets {
		if strings.TrimSpace(asset.URL) == "" && strings.TrimSpace(asset.TaskFileID) == "" {
			continue
		}
		switch strings.TrimSpace(asset.Type) {
		case "video", "video_url":
			hasVideo = true
		case "audio", "audio_url":
			hasAudio = true
		}
	}
	switch requirement {
	case MontageSourceRequirementVideo:
		if !hasVideo {
			return fmt.Errorf("selected pipeline requires a video source")
		}
	case MontageSourceRequirementVideoOrAudio:
		if !hasVideo && !hasAudio {
			return fmt.Errorf("selected pipeline requires a video or audio source")
		}
	}
	return nil
}

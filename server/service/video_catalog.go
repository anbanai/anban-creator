package service

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
)

type VideoModelCatalog map[string]VideoModelSpec

type VideoModelSpec struct {
	Key                  string   `json:"key" yaml:"key"`
	DisplayName          string   `json:"display_name" yaml:"display_name"`
	ModelID              string   `json:"model_id" yaml:"model_id"`
	SupportedResolutions []string `json:"supported_resolutions" yaml:"supported_resolutions"`
	SupportedRatios      []string `json:"supported_ratios" yaml:"supported_ratios"`
	MinDuration          int64    `json:"min_duration" yaml:"min_duration"`
	MaxDuration          int64    `json:"max_duration" yaml:"max_duration"`
	SupportsVideoInput   bool     `json:"supports_video_input" yaml:"supports_video_input"`
	Supports4K           bool     `json:"supports_4k" yaml:"supports_4k"`
}

func DefaultVideoModelCatalog() VideoModelCatalog {
	commonRatios := []string{"16:9", "9:16", "1:1", "4:3", "3:4"}
	return VideoModelCatalog{
		"seedance-2.0": {
			Key:                  "seedance-2.0",
			DisplayName:          "Doubao Seedance 2.0",
			ModelID:              "doubao-seedance-2-0-260128",
			SupportedResolutions: []string{"480p", "720p", "1080p", "4k"},
			SupportedRatios:      commonRatios,
			MinDuration:          1,
			MaxDuration:          15,
			SupportsVideoInput:   true,
			Supports4K:           true,
		},
		"seedance-2.0-fast": {
			Key:                  "seedance-2.0-fast",
			DisplayName:          "Doubao Seedance 2.0 Fast",
			ModelID:              "doubao-seedance-2-0-fast-260128",
			SupportedResolutions: []string{"480p", "720p"},
			SupportedRatios:      commonRatios,
			MinDuration:          1,
			MaxDuration:          15,
			SupportsVideoInput:   true,
		},
		"seedance-2.0-mini": {
			Key:                  "seedance-2.0-mini",
			DisplayName:          "Doubao Seedance 2.0 Mini",
			ModelID:              "doubao-seedance-2-0-mini-260615",
			SupportedResolutions: []string{"480p", "720p"},
			SupportedRatios:      commonRatios,
			MinDuration:          1,
			MaxDuration:          15,
			SupportsVideoInput:   true,
		},
	}
}

func VideoModelCatalogFromConfig(entries []config.VideoModelCatalogEntry) VideoModelCatalog {
	defaults := DefaultVideoModelCatalog()
	if len(entries) == 0 {
		return VideoModelCatalog{}
	}
	catalog := VideoModelCatalog{}
	for _, entry := range entries {
		if strings.TrimSpace(entry.Key) == "" {
			continue
		}
		spec, existed := defaults[entry.Key]
		spec.Key = entry.Key
		if entry.DisplayName != "" {
			spec.DisplayName = entry.DisplayName
		}
		if entry.ModelID != "" {
			spec.ModelID = entry.ModelID
		}
		if len(entry.SupportedResolutions) > 0 {
			spec.SupportedResolutions = entry.SupportedResolutions
		}
		if len(entry.SupportedRatios) > 0 {
			spec.SupportedRatios = entry.SupportedRatios
		}
		if entry.MinDuration > 0 {
			spec.MinDuration = entry.MinDuration
		}
		if entry.MaxDuration > 0 {
			spec.MaxDuration = entry.MaxDuration
		}
		if !existed || entry.SupportsVideoInput {
			spec.SupportsVideoInput = entry.SupportsVideoInput
		}
		if !existed || entry.Supports4K {
			spec.Supports4K = entry.Supports4K
		}
		catalog[entry.Key] = spec
	}
	return catalog
}

func ResolveVideoGenerationPlan(req VideoGenerationRequest, defaults model.VideoDefaults, policy model.VideoModelPolicy, catalog VideoModelCatalog) (VideoGenerationPlan, error) {
	if defaults == (model.VideoDefaults{}) || policy.DefaultModel == "" || len(policy.AllowedModels) == 0 {
		return VideoGenerationPlan{}, fmt.Errorf("project videocreator profile is not configured")
	}
	if catalog == nil {
		catalog = VideoModelCatalog{}
	}
	resolved := req
	if playbook, ok := FindVideoPlaybook(resolved.ScenarioKey); ok {
		if resolved.CreativeType == "" {
			resolved.CreativeType = playbook.CreativeType
		}
		if resolved.Purpose == "" {
			resolved.Purpose = playbook.Purpose
		}
		if resolved.Ratio == "" {
			resolved.Ratio = playbook.DefaultRatio
		}
	}
	if resolved.CreativeType == "" {
		resolved.CreativeType = defaults.CreativeType
	}
	if resolved.CreativeType == "" {
		resolved.CreativeType = VideoCreativeTypePersonalIP
	}
	if resolved.Purpose == "" {
		if req.CreativeType != "" {
			resolved.Purpose = DefaultVideoPurposeForCreativeType(resolved.CreativeType)
		} else {
			resolved.Purpose = defaults.Purpose
		}
	}
	if resolved.Purpose == "" {
		resolved.Purpose = DefaultVideoPurposeForCreativeType(resolved.CreativeType)
	}
	modelKey := strings.TrimSpace(resolved.Model)
	if modelKey == "" {
		modelKey = defaults.ModelKey
	}
	if modelKey == "" {
		modelKey = policy.DefaultModel
	}
	if !stringIn(modelKey, policy.AllowedModels) {
		return VideoGenerationPlan{}, fmt.Errorf("model %s is not allowed by project video policy", modelKey)
	}
	spec, ok := catalog[modelKey]
	if !ok {
		return VideoGenerationPlan{}, fmt.Errorf("video model %s is not configured or unavailable", modelKey)
	}
	resolved.Model = spec.ModelID
	if resolved.Resolution == "" {
		resolved.Resolution = defaults.Resolution
	}
	if resolved.Ratio == "" {
		resolved.Ratio = defaults.Ratio
	}
	targetDuration, targetSource, targetReason, err := resolveTargetVideoDuration(req, defaults)
	if err != nil {
		return VideoGenerationPlan{}, err
	}
	resolved.Duration = targetDuration
	if resolved.Watermark == nil {
		resolved.Watermark = defaults.Watermark
	}
	preflight := defaults.Preflight
	if resolved.Preflight != nil {
		preflight = *resolved.Preflight
	}
	if err := validateVideoGenerationRequest(resolved); err != nil {
		return VideoGenerationPlan{}, err
	}
	if policy.MaxDuration > 0 && resolved.Duration > policy.MaxDuration {
		return VideoGenerationPlan{}, fmt.Errorf("duration %d exceeds project max duration %d", resolved.Duration, policy.MaxDuration)
	}
	if policy.MaxResolution != "" && resolutionRank(resolved.Resolution) > resolutionRank(policy.MaxResolution) {
		return VideoGenerationPlan{}, fmt.Errorf("resolution %s exceeds project max resolution %s", resolved.Resolution, policy.MaxResolution)
	}
	if !stringInFold(resolved.Resolution, spec.SupportedResolutions) {
		suggested := highestResolutionAtOrBelow(spec.SupportedResolutions, resolved.Resolution)
		if suggested != "" {
			if policy.AllowAutoDowngrade {
				resolved.Resolution = suggested
			} else {
				return VideoGenerationPlan{}, fmt.Errorf("model %s does not support resolution %s; suggested resolution: %s", modelKey, resolved.Resolution, suggested)
			}
		} else {
			return VideoGenerationPlan{}, fmt.Errorf("model %s does not support resolution %s", modelKey, resolved.Resolution)
		}
	}
	if !stringInFold(resolved.Ratio, spec.SupportedRatios) {
		return VideoGenerationPlan{}, fmt.Errorf("model %s does not support ratio %s", modelKey, resolved.Ratio)
	}
	hasInputVideo, _ := videoInputStats(resolved.ReferenceSet)
	if hasInputVideo && !spec.SupportsVideoInput {
		return VideoGenerationPlan{}, fmt.Errorf("model %s does not support video input", modelKey)
	}
	segmentDurations, err := splitVideoDuration(targetDuration, spec.MinDuration, spec.MaxDuration)
	if err != nil {
		return VideoGenerationPlan{}, err
	}
	segments := make([]VideoGenerationSegmentPlan, 0, len(segmentDurations))
	var cursor int64
	for i, duration := range segmentDurations {
		index := i + 1
		segmentPrompt := resolved.Prompt
		if len(segmentDurations) > 1 {
			segmentPrompt = fmt.Sprintf("%s\n\nSegment %d/%d: generate the continuous portion from %ds to %ds of the final video. Keep character, setting, lighting, and style consistent with adjacent segments.", resolved.Prompt, index, len(segmentDurations), cursor, cursor+duration)
		}
		segments = append(segments, VideoGenerationSegmentPlan{
			Index:       index,
			StartSecond: cursor,
			EndSecond:   cursor + duration,
			Duration:    duration,
			Prompt:      segmentPrompt,
			ModelKey:    modelKey,
			Model:       resolved.Model,
			Resolution:  resolved.Resolution,
			Ratio:       resolved.Ratio,
		})
		cursor += duration
	}
	plan := VideoGenerationPlan{
		ScenarioKey:               resolved.ScenarioKey,
		ProductionMode:            resolved.ProductionMode,
		Purpose:                   resolved.Purpose,
		CreativeType:              resolved.CreativeType,
		SubjectProfile:            resolved.SubjectProfile,
		Audience:                  resolved.Audience,
		SingleMessage:             resolved.SingleMessage,
		Prompt:                    resolved.Prompt,
		ModelKey:                  modelKey,
		Model:                     resolved.Model,
		Resolution:                resolved.Resolution,
		Ratio:                     resolved.Ratio,
		Duration:                  targetDuration,
		TargetDurationSeconds:     targetDuration,
		TargetDurationSource:      targetSource,
		TargetDurationReason:      targetReason,
		SegmentMaxDurationSeconds: spec.MaxDuration,
		SegmentMinDurationSeconds: spec.MinDuration,
		Segments:                  segments,
		Seed:                      resolved.Seed,
		CameraFixed:               resolved.CameraFixed,
		Watermark:                 resolved.Watermark,
		Preflight:                 preflight,
		ServiceTier:               resolved.ServiceTier,
		References:                resolved.ReferenceSet,
		RetakeBudget:              resolved.RetakeBudget,
		DeliveryTargets:           resolved.DeliveryTargets,
	}
	return plan, nil
}

func resolveTargetVideoDuration(req VideoGenerationRequest, defaults model.VideoDefaults) (int64, string, string, error) {
	if req.Duration > 0 {
		return req.Duration, VideoDurationSourceUser, "user requested explicit target duration", nil
	}
	if _, inputSeconds := videoInputStats(req.ReferenceSet); inputSeconds > 0 {
		return int64(math.Round(inputSeconds)), VideoDurationSourceReferenceVideo, "matched measured reference video duration", nil
	}
	if req.PlannedDurationSeconds > 0 {
		reason := strings.TrimSpace(req.TargetDurationReason)
		if reason == "" {
			return 0, "", "", fmt.Errorf("target_duration_reason is required when target duration source is ai_planned")
		}
		return req.PlannedDurationSeconds, VideoDurationSourceAIPlanned, reason, nil
	}
	if defaults.Duration > 0 {
		return defaults.Duration, VideoDurationSourceProjectDefault, "project default target duration", nil
	}
	return 0, "", "", fmt.Errorf("target duration is required")
}

func splitVideoDuration(target, minDuration, maxDuration int64) ([]int64, error) {
	if minDuration <= 0 {
		minDuration = 1
	}
	if maxDuration <= 0 {
		return nil, fmt.Errorf("model max duration must be configured")
	}
	if minDuration > maxDuration {
		return nil, fmt.Errorf("model min duration %d exceeds max duration %d", minDuration, maxDuration)
	}
	if target < minDuration {
		return nil, fmt.Errorf("target duration %d is shorter than model min duration %d", target, minDuration)
	}
	segmentCount := int(math.Ceil(float64(target) / float64(maxDuration)))
	for {
		if segmentCount <= 0 {
			return nil, fmt.Errorf("target duration is required")
		}
		base := target / int64(segmentCount)
		remainder := target % int64(segmentCount)
		if base >= minDuration && base <= maxDuration {
			segments := make([]int64, segmentCount)
			for i := 0; i < segmentCount; i++ {
				segments[i] = base
				if int64(i) < remainder {
					segments[i]++
				}
				if segments[i] < minDuration || segments[i] > maxDuration {
					return nil, fmt.Errorf("cannot split target duration %d into legal model segments", target)
				}
			}
			return segments, nil
		}
		segmentCount++
		if int64(segmentCount)*minDuration > target {
			return nil, fmt.Errorf("cannot split target duration %d into legal model segments", target)
		}
	}
}

func videoInputStats(refs []VideoReferenceInput) (bool, float64) {
	maxSeconds := 0.0
	found := false
	for _, ref := range refs {
		if ref.Type != VideoReferenceVideo {
			continue
		}
		found = true
		if ref.InputDurationSeconds > maxSeconds {
			maxSeconds = ref.InputDurationSeconds
		}
	}
	return found, maxSeconds
}

func stringIn(v string, items []string) bool {
	for _, item := range items {
		if v == item {
			return true
		}
	}
	return false
}

func stringInFold(v string, items []string) bool {
	for _, item := range items {
		if strings.EqualFold(v, item) {
			return true
		}
	}
	return false
}

func resolutionRank(v string) int {
	switch strings.ToLower(v) {
	case "480p":
		return 1
	case "720p":
		return 2
	case "1080p":
		return 3
	case "2k":
		return 4
	case "4k":
		return 5
	default:
		return 0
	}
}

func highestResolutionAtOrBelow(supported []string, requested string) string {
	reqRank := resolutionRank(requested)
	ordered := append([]string(nil), supported...)
	sort.Slice(ordered, func(i, j int) bool { return resolutionRank(ordered[i]) > resolutionRank(ordered[j]) })
	for _, item := range ordered {
		if resolutionRank(item) <= reqRank {
			return strings.ToLower(item)
		}
	}
	return ""
}
